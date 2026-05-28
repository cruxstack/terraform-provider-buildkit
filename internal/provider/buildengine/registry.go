// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildengine

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	cliconfig "github.com/docker/cli/cli/config"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// ImageInfo is the resolved metadata for a registry reference.
type ImageInfo struct {
	Reference string
	Digest    string // sha256:...
	DigestURL string // registry/repo@sha256:...
	MediaType string
	Platforms []string
	Labels    map[string]string
	Created   string
}

// authenticatorFor returns a go-containerregistry Authenticator for the given
// registry host using the same precedence as builds: explicit creds first,
// then docker config (when enabled), else anonymous.
func (a AuthConfig) authenticatorFor(host string) authn.Authenticator {
	key := normalizeHost(host)
	for k, ra := range a.Explicit {
		if normalizeHost(k) == key {
			return authn.FromConfig(authn.AuthConfig{
				Username:      ra.Username,
				Password:      ra.Password,
				Auth:          ra.Auth,
				IdentityToken: ra.IdentityToken,
			})
		}
	}
	if a.UseDockerConfig {
		if cf, err := cliconfig.Load(cliconfig.Dir()); err == nil && cf != nil {
			if ac, err := cf.GetAuthConfig(host); err == nil {
				return authn.FromConfig(authn.AuthConfig{
					Username:      ac.Username,
					Password:      ac.Password,
					Auth:          ac.Auth,
					IdentityToken: ac.IdentityToken,
					RegistryToken: ac.RegistryToken,
				})
			}
		}
	}
	return authn.Anonymous
}

// ErrNotFound is returned when a registry reference does not exist (HTTP 404).
var ErrNotFound = errors.New("registry reference not found")

// ResolveImage looks up a registry reference and returns its metadata. If the
// reference does not exist it returns ErrNotFound.
func (a AuthConfig) ResolveImage(reference string) (*ImageInfo, error) {
	return a.ResolveImageInsecure(reference, false)
}

// ResolveImageInsecure is like ResolveImage but allows plain-HTTP / untrusted
// TLS registries when insecure is true.
func (a AuthConfig) ResolveImageInsecure(reference string, insecure bool) (*ImageInfo, error) {
	var nopts []name.Option
	if insecure {
		nopts = append(nopts, name.Insecure)
	}
	ref, err := name.ParseReference(reference, nopts...)
	if err != nil {
		return nil, err
	}
	auth := a.authenticatorFor(ref.Context().RegistryStr())

	desc, err := remote.Get(ref, remote.WithAuth(auth), remote.WithTransport(remote.DefaultTransport))
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	info := &ImageInfo{
		Reference: ref.Name(),
		Digest:    desc.Digest.String(),
		DigestURL: ref.Context().Digest(desc.Digest.String()).String(),
		MediaType: string(desc.MediaType),
		Labels:    map[string]string{},
	}

	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err == nil {
			if mft, err := idx.IndexManifest(); err == nil {
				for _, m := range mft.Manifests {
					if m.Platform != nil {
						info.Platforms = append(info.Platforms, m.Platform.OS+"/"+m.Platform.Architecture)
					}
				}
			}
		}
	} else if desc.MediaType.IsImage() {
		img, err := desc.Image()
		if err == nil {
			if cfg, err := img.ConfigFile(); err == nil {
				if cfg.OS != "" {
					info.Platforms = append(info.Platforms, cfg.OS+"/"+cfg.Architecture)
				}
				for k, v := range cfg.Config.Labels {
					info.Labels[k] = v
				}
				if !cfg.Created.IsZero() {
					info.Created = cfg.Created.UTC().Round(time.Second).Format(time.RFC3339)
				}
			}
		}
	}
	return info, nil
}

// Digest returns just the digest (sha256:...) of a reference, or ErrNotFound.
func (a AuthConfig) Digest(reference string) (string, error) {
	return a.DigestInsecure(reference, false)
}

// DigestInsecure is like Digest but allows plain-HTTP / untrusted-TLS
// registries when insecure is true.
func (a AuthConfig) DigestInsecure(reference string, insecure bool) (string, error) {
	opts := []name.Option{}
	if insecure {
		opts = append(opts, name.Insecure)
	}
	ref, err := name.ParseReference(reference, opts...)
	if err != nil {
		return "", err
	}
	auth := a.authenticatorFor(ref.Context().RegistryStr())
	d, err := remote.Head(ref, remote.WithAuth(auth))
	if err != nil {
		if isNotFound(err) {
			return "", ErrNotFound
		}
		return "", err
	}
	return d.Digest.String(), nil
}

// ListTags returns the tags of a repository.
func (a AuthConfig) ListTags(repository string) ([]string, error) {
	return a.ListTagsInsecure(repository, false)
}

// ListTagsInsecure is like ListTags but allows insecure registries.
func (a AuthConfig) ListTagsInsecure(repository string, insecure bool) ([]string, error) {
	var nopts []name.Option
	if insecure {
		nopts = append(nopts, name.Insecure)
	}
	repo, err := name.NewRepository(repository, nopts...)
	if err != nil {
		return nil, err
	}
	auth := a.authenticatorFor(repo.RegistryStr())
	return remote.List(repo, remote.WithAuth(auth))
}

func isNotFound(err error) bool {
	var te *transport.Error
	if errors.As(err, &te) {
		return te.StatusCode == http.StatusNotFound
	}
	return false
}

// ImageQuery filters a repository's tags.
type ImageQuery struct {
	Registry   string
	Repository string
	TagPattern string            // regex; empty matches all
	Labels     map[string]string // required label key/values
	Platforms  []string          // required platforms (all must be present)
	Insecure   bool              // allow plain-HTTP / untrusted TLS
}

// QueryImages lists tags in a repository, resolves each to image metadata, and
// filters by tag pattern, required labels, and required platforms.
func (a AuthConfig) QueryImages(q ImageQuery) ([]*ImageInfo, error) {
	repoRef := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(q.Registry, "https://"), "http://"), "/") + "/" + q.Repository
	tags, err := a.ListTagsInsecure(repoRef, q.Insecure)
	if err != nil {
		return nil, err
	}

	var re *regexp.Regexp
	if q.TagPattern != "" {
		re, err = regexp.Compile(q.TagPattern)
		if err != nil {
			return nil, err
		}
	}

	var results []*ImageInfo
	for _, t := range tags {
		if re != nil && !re.MatchString(t) {
			continue
		}
		info, err := a.ResolveImageInsecure(repoRef+":"+t, q.Insecure)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, err
		}
		if !matchesLabels(info.Labels, q.Labels) {
			continue
		}
		if !matchesPlatforms(info.Platforms, q.Platforms) {
			continue
		}
		results = append(results, info)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Reference < results[j].Reference })
	return results, nil
}

func matchesLabels(have, required map[string]string) bool {
	for k, v := range required {
		if have[k] != v {
			return false
		}
	}
	return true
}

func matchesPlatforms(have, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := map[string]struct{}{}
	for _, p := range have {
		set[p] = struct{}{}
	}
	for _, r := range required {
		if _, ok := set[r]; !ok {
			return false
		}
	}
	return true
}
