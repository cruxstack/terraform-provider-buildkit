// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"path/filepath"
	"testing"

	cliconfig "github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/context/store"
	dockerclient "github.com/docker/docker/client"
)

type dockerEnv struct {
	home      string
	configDir string
}

func newDockerEnv(t *testing.T) dockerEnv {
	t.Helper()
	env := dockerEnv{home: t.TempDir(), configDir: t.TempDir()}
	t.Setenv("HOME", env.home)
	t.Setenv(dockerclient.EnvOverrideHost, "")
	t.Setenv(envDockerContext, "")
	prev := cliconfig.Dir()
	cliconfig.SetDir(env.configDir)
	t.Cleanup(func() { cliconfig.SetDir(prev) })
	return env
}

func (e dockerEnv) socket(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join(e.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return "unix://" + p
}

func (e dockerEnv) context(t *testing.T, name, host string, tls bool) {
	t.Helper()
	s := store.New(cliconfig.ContextStoreDir(), store.NewConfig(
		func() any { return &map[string]any{} },
		store.EndpointTypeGetter(dockerEndpointName, func() any { return &dockerEndpointMeta{} }),
	))
	if err := s.CreateOrUpdate(store.Metadata{
		Name:      name,
		Metadata:  map[string]any{},
		Endpoints: map[string]any{dockerEndpointName: dockerEndpointMeta{Host: host}},
	}); err != nil {
		t.Fatal(err)
	}
	if tls {
		if err := s.ResetEndpointTLSMaterial(name, dockerEndpointName, &store.EndpointTLSData{
			Files: map[string][]byte{"ca.pem": []byte("ca")},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func (e dockerEnv) currentContext(t *testing.T, name string) {
	t.Helper()
	cfg := []byte(`{"currentContext":"` + name + `"}`)
	if err := os.WriteFile(filepath.Join(e.configDir, cliconfig.ConfigFileName), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
}

func hosts(cands []dockerHostCandidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.host)
	}
	return out
}

func assertHosts(t *testing.T, got []dockerHostCandidate, want ...string) {
	t.Helper()
	g := hosts(got)
	if len(g) != len(want) {
		t.Fatalf("hosts = %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("hosts = %v, want %v", g, want)
		}
	}
}

func TestDockerHostCandidatesDockerHostIsExclusive(t *testing.T) {
	env := newDockerEnv(t)
	env.socket(t, ".orbstack/run/docker.sock")
	t.Setenv(dockerclient.EnvOverrideHost, "tcp://127.0.0.1:2375")

	got := dockerHostCandidates()
	assertHosts(t, got, "tcp://127.0.0.1:2375")
	if !got[0].fromEnv {
		t.Error("DOCKER_HOST candidate should use dockerclient.FromEnv")
	}
}

func TestDockerHostCandidatesDockerContextIsExclusive(t *testing.T) {
	env := newDockerEnv(t)
	env.socket(t, ".orbstack/run/docker.sock")
	env.context(t, "remote", "tcp://10.0.0.5:2375", false)
	t.Setenv(envDockerContext, "remote")

	assertHosts(t, dockerHostCandidates(), "tcp://10.0.0.5:2375")
}

func TestDockerHostCandidatesUnknownDockerContext(t *testing.T) {
	newDockerEnv(t)
	t.Setenv(envDockerContext, "missing")

	got := dockerHostCandidates()
	if len(got) != 1 || got[0].err == nil {
		t.Fatalf("candidates = %+v, want one unresolved candidate", got)
	}
}

func TestDockerHostCandidatesStaleDefaultFallsBackToUserSockets(t *testing.T) {
	env := newDockerEnv(t)
	orb := env.socket(t, ".orbstack/run/docker.sock")
	colima := env.socket(t, ".colima/default/docker.sock")

	assertHosts(t, dockerHostCandidates(), dockerclient.DefaultDockerHost, orb, colima)
}

func TestDockerHostCandidatesCurrentContextFirst(t *testing.T) {
	env := newDockerEnv(t)
	orb := env.socket(t, ".orbstack/run/docker.sock")
	dd := env.socket(t, ".docker/run/docker.sock")
	env.context(t, "orbstack", orb, false)
	env.currentContext(t, "orbstack")

	got := dockerHostCandidates()
	assertHosts(t, got, orb, dockerclient.DefaultDockerHost, dd)
	if got[0].source != `docker context "orbstack"` {
		t.Errorf("source = %q", got[0].source)
	}
}

func TestDockerHostCandidatesDefaultCurrentContext(t *testing.T) {
	env := newDockerEnv(t)
	env.currentContext(t, defaultContextName)

	assertHosts(t, dockerHostCandidates(), dockerclient.DefaultDockerHost)
}

func TestDockerHostCandidatesTLSContextIsReported(t *testing.T) {
	env := newDockerEnv(t)
	env.context(t, "secure", "tcp://10.0.0.5:2376", true)
	env.currentContext(t, "secure")

	got := dockerHostCandidates()
	if got[0].host != "tcp://10.0.0.5:2376" || got[0].err == nil {
		t.Fatalf("first candidate = %+v, want TLS context reported with an error", got[0])
	}
	assertHosts(t, got[1:], dockerclient.DefaultDockerHost)
}
