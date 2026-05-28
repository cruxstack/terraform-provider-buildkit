// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package buildengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/session/sshforward/sshprovider"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sync/errgroup"
)

// Attestations requests SBOM and/or provenance attestation manifests on the
// build output. Provenance mode is "min" or "max"; empty disables it.
type Attestations struct {
	SBOM           bool
	ProvenanceMode string
}

// SSHConfig mirrors a single sshprovider.AgentConfig.
type SSHConfig struct {
	ID    string
	Paths []string
}

// CacheEntry is one --import-cache / --export-cache entry.
type CacheEntry struct {
	Type  string
	Attrs map[string]string
}

// Request fully describes a single BuildKit solve.
type Request struct {
	Context    string
	Dockerfile string
	Target     string
	Platforms  []string
	BuildArgs  map[string]string
	Labels     map[string]string
	Secrets    map[string][]byte
	SSH        []SSHConfig
	CacheFrom  []CacheEntry
	CacheTo    []CacheEntry
	Exports    []client.ExportEntry
	Attest     *Attestations

	// Auth resolves registry credentials for pulling base images and pushing.
	Auth AuthConfig
}

// Response carries the useful outputs of a solve.
type Response struct {
	ExporterResponse map[string]string
}

// ImageDigest returns the produced image manifest digest, if any.
func (r *Response) ImageDigest() string {
	if r == nil || r.ExporterResponse == nil {
		return ""
	}
	return r.ExporterResponse["containerimage.digest"]
}

// Run executes a single Dockerfile solve against the provided client, streaming
// progress to the Terraform logs and returning the exporter response.
func Run(ctx context.Context, c *client.Client, req Request) (*Response, error) {
	if c == nil {
		return nil, fmt.Errorf("no buildkit client")
	}

	contextDir, err := filepath.Abs(req.Context)
	if err != nil {
		return nil, fmt.Errorf("resolving build context: %w", err)
	}
	dockerfile := req.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	contextFS, err := fsutil.NewFS(contextDir)
	if err != nil {
		return nil, fmt.Errorf("preparing context fs: %w", err)
	}
	dockerfileDir := filepath.Dir(filepath.Join(contextDir, dockerfile))
	dockerfileFS, err := fsutil.NewFS(dockerfileDir)
	if err != nil {
		return nil, fmt.Errorf("preparing dockerfile fs: %w", err)
	}

	frontendAttrs := map[string]string{
		"filename": filepath.Base(dockerfile),
	}
	if req.Target != "" {
		frontendAttrs["target"] = req.Target
	}
	if len(req.Platforms) > 0 {
		frontendAttrs["platform"] = strings.Join(req.Platforms, ",")
	}
	for k, v := range req.BuildArgs {
		frontendAttrs["build-arg:"+k] = v
	}
	for k, v := range req.Labels {
		frontendAttrs["label:"+k] = v
	}
	if req.Attest != nil {
		if req.Attest.SBOM {
			frontendAttrs["attest:sbom"] = ""
		}
		if req.Attest.ProvenanceMode != "" {
			frontendAttrs["attest:provenance"] = "mode=" + req.Attest.ProvenanceMode
		}
	}

	sessionAttachables := []session.Attachable{
		NewAuthProvider(req.Auth),
	}
	if len(req.Secrets) > 0 {
		sessionAttachables = append(sessionAttachables, secretsprovider.FromMap(req.Secrets))
	}
	if len(req.SSH) > 0 {
		confs := make([]sshprovider.AgentConfig, 0, len(req.SSH))
		for _, s := range req.SSH {
			confs = append(confs, sshprovider.AgentConfig{ID: s.ID, Paths: s.Paths})
		}
		sp, err := sshprovider.NewSSHAgentProvider(confs)
		if err != nil {
			return nil, fmt.Errorf("configuring ssh agent forwarding: %w", err)
		}
		sessionAttachables = append(sessionAttachables, sp)
	}

	solveOpt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalMounts: map[string]fsutil.FS{
			"context":    contextFS,
			"dockerfile": dockerfileFS,
		},
		Exports:      req.Exports,
		CacheImports: toCacheEntries(req.CacheFrom),
		CacheExports: toCacheEntries(req.CacheTo),
		Session:      sessionAttachables,
	}

	var resp *Response
	statusCh := make(chan *client.SolveStatus)

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		r, err := c.Solve(egctx, nil, solveOpt, statusCh)
		if err != nil {
			return fmt.Errorf("buildkit solve failed: %w", err)
		}
		if r != nil {
			resp = &Response{ExporterResponse: r.ExporterResponse}
		}
		return nil
	})
	eg.Go(func() error {
		streamStatus(egctx, statusCh)
		return nil
	})

	if err := eg.Wait(); err != nil {
		return nil, err
	}
	if resp == nil {
		resp = &Response{ExporterResponse: map[string]string{}}
	}
	return resp, nil
}

// streamStatus forwards BuildKit solve status to the Terraform logs until the
// status channel is closed.
func streamStatus(ctx context.Context, ch chan *client.SolveStatus) {
	for status := range ch {
		if status == nil {
			continue
		}
		for _, v := range status.Vertexes {
			fields := map[string]any{"name": v.Name}
			if v.Error != "" {
				fields["error"] = v.Error
				tflog.Warn(ctx, "buildkit vertex error", fields)
				continue
			}
			if v.Completed != nil {
				tflog.Debug(ctx, "buildkit vertex completed", fields)
			}
		}
		for _, l := range status.Logs {
			tflog.Trace(ctx, "buildkit log", map[string]any{
				"data": strings.TrimRight(string(l.Data), "\n"),
			})
		}
	}
}

func toCacheEntries(in []CacheEntry) []client.CacheOptionsEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]client.CacheOptionsEntry, 0, len(in))
	for _, e := range in {
		out = append(out, client.CacheOptionsEntry{Type: e.Type, Attrs: e.Attrs})
	}
	return out
}

// LocalExport returns an ExportEntry that writes the target stage's filesystem
// to a host directory (used by buildkit_artifact).
func LocalExport(dir string) client.ExportEntry {
	return client.ExportEntry{Type: client.ExporterLocal, OutputDir: dir}
}

// ImageExport returns an ExportEntry that produces (and optionally pushes) an
// image under the given comma-joined names. When insecure is true, pushes are
// allowed over plain HTTP / with untrusted TLS (registry.insecure=true).
func ImageExport(names []string, push, insecure bool) client.ExportEntry {
	sort.Strings(names)
	attrs := map[string]string{"name": strings.Join(names, ",")}
	if push {
		attrs["push"] = "true"
	}
	if insecure {
		attrs["registry.insecure"] = "true"
	}
	return client.ExportEntry{Type: client.ExporterImage, Attrs: attrs}
}

// EnsureDir creates a directory if missing (helper for export targets).
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
