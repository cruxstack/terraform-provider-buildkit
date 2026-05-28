// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

// Package buildengine contains the shared BuildKit solve logic used by all
// resources and data sources in the provider.
package buildengine

import (
	"context"
	"strings"
	"time"

	cliconfig "github.com/docker/cli/cli/config"
	clitypes "github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
)

// RegistryAuth is a single explicit registry credential supplied via the
// provider configuration. Address is matched against the registry host that
// BuildKit requests credentials for.
type RegistryAuth struct {
	Address       string
	Username      string
	Password      string
	Auth          string
	IdentityToken string
}

// AuthConfig controls how registry credentials are resolved during a build.
type AuthConfig struct {
	// Explicit credentials supplied via the provider's registry_auth blocks,
	// keyed by normalized host.
	Explicit map[string]RegistryAuth
	// UseDockerConfig enables fallback to ~/.docker/config.json and its
	// configured credential helpers.
	UseDockerConfig bool
}

// normalizeHost maps the various forms BuildKit and docker use for the Docker
// Hub registry onto a single canonical key, and strips scheme/path noise from
// other hosts so explicit blocks match regardless of how they were written.
func normalizeHost(host string) string {
	h := host
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimSuffix(h, "/")
	switch h {
	case "index.docker.io/v1", "index.docker.io", "registry-1.docker.io", "docker.io":
		return "docker.io"
	}
	return h
}

// NewAuthProvider builds a BuildKit session attachable that resolves registry
// credentials. Explicit provider credentials take precedence; when none match
// and UseDockerConfig is true, the host's docker config + credential helpers
// are consulted.
func NewAuthProvider(cfg AuthConfig) session.Attachable {
	explicit := make(map[string]RegistryAuth, len(cfg.Explicit))
	for k, v := range cfg.Explicit {
		explicit[normalizeHost(k)] = v
	}

	var dockerCfg *configFileLazy
	if cfg.UseDockerConfig {
		dockerCfg = &configFileLazy{}
	}

	provider := func(_ context.Context, host string, _ []string, _ authprovider.ExpireCachedAuthCheck) (clitypes.AuthConfig, error) {
		key := normalizeHost(host)
		if ra, ok := explicit[key]; ok {
			return clitypes.AuthConfig{
				Username:      ra.Username,
				Password:      ra.Password,
				Auth:          ra.Auth,
				IdentityToken: ra.IdentityToken,
				ServerAddress: host,
			}, nil
		}
		if dockerCfg != nil {
			if ac, ok := dockerCfg.lookup(host); ok {
				return ac, nil
			}
		}
		// anonymous
		return clitypes.AuthConfig{ServerAddress: host}, nil
	}

	return authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		AuthConfigProvider: provider,
		ExpireCachedAuth: func(created time.Time, _ string) bool {
			return time.Since(created) > 4*time.Minute+50*time.Second
		},
	})
}

// configFileLazy loads ~/.docker/config.json on first use so we don't touch the
// filesystem / credential helpers unless a registry host actually requires it.
type configFileLazy struct {
	loaded bool
	auths  map[string]clitypes.AuthConfig
}

func (c *configFileLazy) lookup(host string) (clitypes.AuthConfig, bool) {
	if !c.loaded {
		c.loaded = true
		cf, err := cliconfig.Load(cliconfig.Dir())
		if err == nil && cf != nil {
			ac, err := cf.GetAuthConfig(host)
			if err == nil && hasCreds(ac) {
				if c.auths == nil {
					c.auths = map[string]clitypes.AuthConfig{}
				}
				c.auths[normalizeHost(host)] = ac
				return ac, true
			}
		}
		return clitypes.AuthConfig{}, false
	}
	ac, ok := c.auths[normalizeHost(host)]
	return ac, ok
}

func hasCreds(ac clitypes.AuthConfig) bool {
	return ac.Username != "" || ac.Password != "" || ac.Auth != "" || ac.IdentityToken != "" || ac.RegistryToken != ""
}
