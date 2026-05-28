// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	dockerclient "github.com/docker/docker/client"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	bkclient "github.com/moby/buildkit/client"

	// register connection helpers so addresses like docker-container:// work
	// when passed explicitly or via BUILDKIT_HOST.
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
)

// resolvedEndpoint is the outcome of discovery: a connected buildkit client and
// a human-readable description of how it was reached (for logging).
type resolvedEndpoint struct {
	client  *bkclient.Client
	source  string
	cleanup func()
}

// resolveOptions controls endpoint resolution.
type resolveOptions struct {
	address      string
	autodiscover bool
	embedded     bool
}

// resolveBuildkit finds and connects to a buildkit endpoint.
//
// resolution order:
//  1. explicit address (config) - always wins, never falls through.
//  2. BUILDKIT_HOST env var.
//
// when autodiscover is enabled and neither of the above is set:
//  3. docker engine embedded buildkit via the daemon /grpc endpoint
//     (OrbStack / Docker Desktop / Colima default "docker" driver). uses the
//     docker socket as transport only.
//  4. conventional local buildkitd sockets (rootless / system).
//
// each candidate is validated with a ListWorkers ping before acceptance.
func resolveBuildkit(ctx context.Context, opts resolveOptions) (*resolvedEndpoint, error) {
	var tried []string

	// 1. explicit address.
	if opts.address != "" {
		c, err := dialDirect(ctx, opts.address)
		if err != nil {
			return nil, fmt.Errorf("connecting to configured buildkit_address %q: %w", opts.address, err)
		}
		return &resolvedEndpoint{client: c, source: "buildkit_address=" + opts.address}, nil
	}

	// 2. BUILDKIT_HOST.
	if env := os.Getenv("BUILDKIT_HOST"); env != "" {
		c, err := dialDirect(ctx, env)
		if err != nil {
			return nil, fmt.Errorf("connecting to BUILDKIT_HOST %q: %w", env, err)
		}
		return &resolvedEndpoint{client: c, source: "BUILDKIT_HOST=" + env}, nil
	}
	tried = append(tried, "BUILDKIT_HOST (unset)")

	if opts.autodiscover {
		// 3. docker engine embedded buildkit via /grpc.
		if c, src, err := dialDockerGRPC(ctx); err == nil {
			return &resolvedEndpoint{client: c, source: src}, nil
		} else {
			tried = append(tried, "docker engine /grpc ("+err.Error()+")")
		}

		// 4. conventional local buildkitd sockets.
		for _, sock := range localSocketCandidates() {
			if _, err := os.Stat(strings.TrimPrefix(sock, "unix://")); err != nil {
				tried = append(tried, sock+" (not found)")
				continue
			}
			c, err := dialDirect(ctx, sock)
			if err != nil {
				tried = append(tried, sock+" ("+err.Error()+")")
				continue
			}
			return &resolvedEndpoint{client: c, source: sock}, nil
		}
	} else {
		tried = append(tried, "auto-discovery (disabled)")
	}

	// 5. embedded rootless buildkitd (Linux only, opt-in). on non-Linux builds
	// startEmbeddedBuildkitd always returns an error, which staticcheck flags as
	// an always-true comparison (SA4023) for that GOOS; it is reachable on Linux.
	if opts.embedded {
		ep, err := startEmbeddedBuildkitd(ctx)
		if err != nil { //nolint:staticcheck // SA4023: only always-true on non-Linux builds
			return nil, fmt.Errorf("embedded_buildkitd requested but failed to start: %w", err)
		}
		return ep, nil
	}

	return nil, fmt.Errorf(
		"could not find a buildkit endpoint. tried: %s. "+
			"fix by setting buildkit_address, exporting BUILDKIT_HOST, enabling embedded_buildkitd (Linux), "+
			"starting Colima/Lima/OrbStack, or running `docker buildx create`",
		strings.Join(tried, "; "),
	)
}

// dialDirect connects using buildkit's own client (handles tcp://, unix://, and
// registered connhelper schemes like docker-container://) and validates it.
func dialDirect(ctx context.Context, address string) (*bkclient.Client, error) {
	warnInsecureTCP(ctx, address)
	c, err := bkclient.New(ctx, address)
	if err != nil {
		return nil, err
	}
	if err := validate(ctx, c); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// warnInsecureTCP logs a warning when connecting to a non-loopback tcp:// host.
// The buildkit client speaks plaintext h2c over tcp:// without TLS, so any
// non-loopback endpoint exposes credentials and build inputs on the wire. Use a
// unix socket, a connection helper (docker-container://), or an SSH/TLS tunnel
// for remote daemons.
func warnInsecureTCP(ctx context.Context, address string) {
	if !strings.HasPrefix(address, "tcp://") {
		return
	}
	host := strings.TrimPrefix(address, "tcp://")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if isLoopbackHost(host) {
		return
	}
	tflog.Warn(ctx, "connecting to a non-loopback buildkit endpoint over plaintext tcp://; "+
		"credentials and build context are sent unencrypted. Prefer a unix socket, a "+
		"docker-container:// connection helper, or an SSH/TLS tunnel for remote daemons.",
		map[string]any{"address": address})
}

func isLoopbackHost(host string) bool {
	if host == "localhost" || host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// dialDockerGRPC reaches the buildkit instance compiled into the docker engine
// (used by OrbStack / Docker Desktop / Colima with the default "docker" driver)
// via the undocumented but buildx-relied-upon /grpc endpoint on the docker
// socket. the docker socket is used purely as a transport.
func dialDockerGRPC(ctx context.Context) (*bkclient.Client, string, error) {
	dcli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, "", fmt.Errorf("docker client: %w", err)
	}

	// confirm the daemon is reachable before attempting the hijack.
	if _, err := dcli.Ping(ctx); err != nil {
		_ = dcli.Close()
		return nil, "", fmt.Errorf("docker daemon unreachable: %w", err)
	}

	c, err := bkclient.New(ctx, "",
		bkclient.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dcli.DialHijack(ctx, "/grpc", "h2c", nil)
		}),
	)
	if err != nil {
		_ = dcli.Close()
		return nil, "", fmt.Errorf("buildkit /grpc dial: %w", err)
	}
	if err := validate(ctx, c); err != nil {
		_ = c.Close()
		_ = dcli.Close()
		return nil, "", fmt.Errorf("buildkit /grpc validate: %w", err)
	}

	host := dcli.DaemonHost()
	return c, "docker engine /grpc (transport: " + host + ")", nil
}

// validate pings the buildkit control API to confirm a usable endpoint.
func validate(ctx context.Context, c *bkclient.Client) error {
	ws, err := c.ListWorkers(ctx)
	if err != nil {
		return err
	}
	if len(ws) == 0 {
		return fmt.Errorf("buildkit endpoint reports no workers")
	}
	return nil
}

func localSocketCandidates() []string {
	candidates := []string{}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		candidates = append(candidates, "unix://"+filepath.Join(xdg, "buildkit", "buildkitd.sock"))
	}
	candidates = append(candidates, "unix:///run/buildkit/buildkitd.sock")
	return candidates
}
