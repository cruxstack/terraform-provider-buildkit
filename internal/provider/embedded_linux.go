// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

//go:build linux

package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	bkclient "github.com/moby/buildkit/client"
)

// startEmbeddedBuildkitd locates a `buildkitd` binary and supervises it as a
// rootless child process for the lifetime of the provider, exposing it on a
// private unix socket. This mirrors the approach of buildctl-daemonless.sh
// rather than embedding the daemon in-process (which would require bundling
// runc/rootlesskit and managing subuid/subgid, /proc, and snapshotter setup).
//
// Requirements on the host:
//   - a `buildkitd` binary on PATH (the moby/buildkit release binaries)
//   - for unprivileged use, `rootlesskit` on PATH and configured
//     /etc/subuid + /etc/subgid entries (see rootlesscontaine.rs)
//
// If `buildkitd` is not found, a clear, actionable error is returned.
func startEmbeddedBuildkitd(ctx context.Context) (*resolvedEndpoint, error) {
	buildkitd, err := exec.LookPath("buildkitd")
	if err != nil {
		return nil, fmt.Errorf(
			"buildkitd binary not found on PATH. Install it from https://github.com/moby/buildkit/releases " +
				"(and `rootlesskit` for unprivileged use), or set buildkit_address / BUILDKIT_HOST to an existing endpoint",
		)
	}

	runDir, err := os.MkdirTemp(embeddedRuntimeRoot(), "buildkit-tf-")
	if err != nil {
		return nil, fmt.Errorf("creating embedded buildkit runtime dir: %w", err)
	}
	sock := filepath.Join(runDir, "buildkitd.sock")
	addr := "unix://" + sock
	root := filepath.Join(runDir, "data")

	rootless := os.Geteuid() != 0
	var cmd *exec.Cmd
	args := []string{
		"--addr", addr,
		"--root", root,
	}
	if rootless {
		// no-process-sandbox avoids needing systempaths=unconfined; acceptable
		// because the daemon already runs unprivileged.
		args = append(args, "--oci-worker-no-process-sandbox")
		if rk, err := exec.LookPath("rootlesskit"); err == nil {
			full := append([]string{buildkitd}, args...)
			cmd = exec.Command(rk, full...) //nolint:gosec // args are provider-controlled
		} else {
			return nil, fmt.Errorf(
				"running as a non-root user but `rootlesskit` was not found on PATH; " +
					"install rootlesskit and configure /etc/subuid + /etc/subgid, or run as root, " +
					"or point at an existing endpoint via buildkit_address / BUILDKIT_HOST",
			)
		}
	} else {
		cmd = exec.Command(buildkitd, args...) //nolint:gosec // args are provider-controlled
	}

	cmd.Env = append(os.Environ(),
		"XDG_RUNTIME_DIR="+runDir,
		"HOME="+homeOrTemp(),
	)
	// surface daemon logs on the provider's stderr for debugging.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	// run the daemon (and, for rootless, its rootlesskit-reexeced children) in
	// its own process group so cleanup can signal the whole group and avoid
	// leaving orphaned buildkitd/runc processes if the provider exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(runDir)
		return nil, fmt.Errorf("starting buildkitd: %w", err)
	}

	pgid := cmd.Process.Pid
	cleanup := func() {
		// signal the entire process group (negative pid) so rootlesskit's
		// grandchildren are reaped too, then fall back to killing the direct
		// child if the group signal did not apply.
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = os.RemoveAll(runDir)
	}

	// wait for the daemon to become reachable.
	client, err := waitForEmbedded(ctx, addr, 30*time.Second)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("waiting for embedded buildkitd: %w", err)
	}

	return &resolvedEndpoint{
		client:  client,
		source:  "embedded buildkitd (" + pidString(cmd) + ", " + addr + ")",
		cleanup: cleanup,
	}, nil
}

func waitForEmbedded(ctx context.Context, addr string, timeout time.Duration) (*bkclient.Client, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := bkclient.New(ctx, addr)
		if err == nil {
			if _, err := c.ListWorkers(ctx); err == nil {
				return c, nil
			} else {
				lastErr = err
				_ = c.Close()
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timed out")
	}
	return nil, lastErr
}

func embeddedRuntimeRoot() string {
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		return x
	}
	return os.TempDir()
}

func homeOrTemp() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return os.TempDir()
}

func pidString(cmd *exec.Cmd) string {
	if cmd == nil || cmd.Process == nil {
		return "pid=?"
	}
	return "pid=" + strconv.Itoa(cmd.Process.Pid)
}
