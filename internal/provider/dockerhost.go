// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	cliconfig "github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/context/store"
	dockerclient "github.com/docker/docker/client"
)

const (
	envDockerContext   = "DOCKER_CONTEXT"
	defaultContextName = "default"
	dockerEndpointName = "docker"
)

// dockerHostCandidate is a docker daemon to probe for its embedded buildkit.
// fromEnv candidates are configured with dockerclient.FromEnv so DOCKER_HOST's
// companion TLS variables apply; err marks a candidate that could not be
// resolved and is reported without dialing.
type dockerHostCandidate struct {
	host    string
	source  string
	fromEnv bool
	err     error
}

func (c dockerHostCandidate) label() string {
	if c.host == "" {
		return c.source
	}
	return c.source + " " + c.host
}

// dockerEndpointMeta is the subset of a docker CLI context's docker endpoint
// that discovery needs.
type dockerEndpointMeta struct {
	Host          string `json:",omitempty"`
	SkipTLSVerify bool
}

// dockerHostCandidates lists docker daemons to probe, mirroring how the docker
// CLI picks one:
//
//  1. DOCKER_HOST, alone when set.
//  2. DOCKER_CONTEXT, alone when set.
//  3. the CLI config's currentContext.
//  4. the default socket.
//  5. per-user sockets of OrbStack, Docker Desktop, Colima and Rancher
//     Desktop that exist, so a stale default context or a missing
//     /var/run/docker.sock symlink does not hide a running engine.
//
// duplicate hosts are dropped.
func dockerHostCandidates() []dockerHostCandidate {
	if host := os.Getenv(dockerclient.EnvOverrideHost); host != "" {
		return []dockerHostCandidate{{host: host, source: "DOCKER_HOST", fromEnv: true}}
	}
	if name := os.Getenv(envDockerContext); name != "" {
		return []dockerHostCandidate{contextCandidate(name, "DOCKER_CONTEXT")}
	}

	var candidates []dockerHostCandidate
	if cfg, err := cliconfig.Load(cliconfig.Dir()); err == nil && cfg.CurrentContext != "" && cfg.CurrentContext != defaultContextName {
		candidates = append(candidates, contextCandidate(cfg.CurrentContext, "docker context"))
	}
	candidates = append(candidates, dockerHostCandidate{host: dockerclient.DefaultDockerHost, source: "default socket"})
	candidates = append(candidates, userSocketCandidates()...)

	seen := map[string]bool{}
	deduped := candidates[:0]
	for _, c := range candidates {
		if c.host != "" && seen[c.host] {
			continue
		}
		seen[c.host] = true
		deduped = append(deduped, c)
	}
	return deduped
}

// contextCandidate resolves a docker CLI context to its docker endpoint host.
// contexts with TLS client material are reported rather than dialed, since
// discovery only uses the docker socket as a plain transport.
func contextCandidate(name, source string) dockerHostCandidate {
	source = fmt.Sprintf("%s %q", source, name)
	if name == defaultContextName {
		return dockerHostCandidate{host: dockerclient.DefaultDockerHost, source: source}
	}

	s := store.New(cliconfig.ContextStoreDir(), store.NewConfig(
		func() any { return &map[string]any{} },
		store.EndpointTypeGetter(dockerEndpointName, func() any { return &dockerEndpointMeta{} }),
	))
	meta, err := s.GetMetadata(name)
	if err != nil {
		return dockerHostCandidate{source: source, err: fmt.Errorf("reading context: %w", err)}
	}
	ep, ok := meta.Endpoints[dockerEndpointName].(dockerEndpointMeta)
	if !ok || ep.Host == "" {
		return dockerHostCandidate{source: source, err: errors.New("context has no docker endpoint host")}
	}
	if files, err := s.ListTLSFiles(name); err == nil && len(files[dockerEndpointName]) > 0 {
		return dockerHostCandidate{host: ep.Host, source: source, err: errors.New("context uses TLS client certificates; set DOCKER_HOST and DOCKER_CERT_PATH or BUILDKIT_HOST instead")}
	}
	return dockerHostCandidate{host: ep.Host, source: source}
}

// userSocketCandidates returns the per-user docker sockets of common desktop
// engines that exist on this host.
func userSocketCandidates() []dockerHostCandidate {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	known := []struct{ source, path string }{
		{"OrbStack", ".orbstack/run/docker.sock"},
		{"Docker Desktop", ".docker/run/docker.sock"},
		{"Docker Desktop", ".docker/desktop/docker.sock"},
		{"Colima", ".colima/default/docker.sock"},
		{"Colima", ".config/colima/default/docker.sock"},
		{"Rancher Desktop", ".rd/docker.sock"},
	}
	var candidates []dockerHostCandidate
	for _, k := range known {
		p := filepath.Join(home, k.path)
		if _, err := os.Stat(p); err == nil {
			candidates = append(candidates, dockerHostCandidate{host: "unix://" + p, source: k.source})
		}
	}
	return candidates
}
