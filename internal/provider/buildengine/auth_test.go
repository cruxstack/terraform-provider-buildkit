// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildengine

import (
	"fmt"
	"sync"
	"testing"

	clitypes "github.com/docker/cli/cli/config/types"
)

// fakeConfigFile implements dockerConfigFile for tests.
type fakeConfigFile struct {
	mu    sync.Mutex
	calls map[string]int
	auths map[string]clitypes.AuthConfig
}

func (f *fakeConfigFile) GetAuthConfig(host string) (clitypes.AuthConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[host]++
	if ac, ok := f.auths[host]; ok {
		return ac, nil
	}
	return clitypes.AuthConfig{}, nil
}

// newLoadedLazy returns a configFileLazy whose docker config is pre-seeded with
// the given fake, bypassing the on-disk load.
func newLoadedLazy(cf dockerConfigFile) *configFileLazy {
	c := &configFileLazy{cf: cf}
	c.loadOnce.Do(func() {}) // mark loaded so load() does not touch disk
	return c
}

func TestConfigFileLazyResolvesMultipleHosts(t *testing.T) {
	fake := &fakeConfigFile{auths: map[string]clitypes.AuthConfig{
		"ghcr.io":   {Username: "gh", Password: "p1"},
		"docker.io": {Username: "dh", Password: "p2"},
	}}
	c := newLoadedLazy(fake)

	// the bug: only the first host queried used to resolve; every later host
	// returned (zero,false). assert both resolve.
	if ac, ok := c.lookup("ghcr.io"); !ok || ac.Username != "gh" {
		t.Fatalf("ghcr.io lookup: ok=%v ac=%+v", ok, ac)
	}
	if ac, ok := c.lookup("docker.io"); !ok || ac.Username != "dh" {
		t.Fatalf("docker.io lookup: ok=%v ac=%+v", ok, ac)
	}
}

func TestConfigFileLazyMemoizesPerHost(t *testing.T) {
	fake := &fakeConfigFile{auths: map[string]clitypes.AuthConfig{
		"ghcr.io": {Username: "gh", Password: "p1"},
	}}
	c := newLoadedLazy(fake)

	for i := 0; i < 3; i++ {
		if _, ok := c.lookup("ghcr.io"); !ok {
			t.Fatal("expected ghcr.io to resolve")
		}
	}
	// a host with no creds should also be memoized (negative cache).
	for i := 0; i < 3; i++ {
		if _, ok := c.lookup("example.com"); ok {
			t.Fatal("did not expect example.com to resolve")
		}
	}

	if fake.calls["ghcr.io"] != 1 {
		t.Errorf("ghcr.io GetAuthConfig called %d times, want 1", fake.calls["ghcr.io"])
	}
	if fake.calls["example.com"] != 1 {
		t.Errorf("example.com GetAuthConfig called %d times, want 1", fake.calls["example.com"])
	}
}

func TestConfigFileLazyConcurrent(t *testing.T) {
	auths := map[string]clitypes.AuthConfig{}
	for i := 0; i < 10; i++ {
		auths[fmt.Sprintf("reg%d.io", i)] = clitypes.AuthConfig{Username: fmt.Sprintf("u%d", i), Password: "p"}
	}
	c := newLoadedLazy(&fakeConfigFile{auths: auths})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		for g := 0; g < 5; g++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				host := fmt.Sprintf("reg%d.io", i)
				if ac, ok := c.lookup(host); !ok || ac.Username != fmt.Sprintf("u%d", i) {
					t.Errorf("host %s: ok=%v ac=%+v", host, ok, ac)
				}
			}(i)
		}
	}
	wg.Wait()
}
