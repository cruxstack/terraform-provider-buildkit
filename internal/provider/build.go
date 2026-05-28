// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sync/errgroup"
)

// buildInput describes everything needed to run a single Dockerfile build and
// export the resulting stage filesystem to a host directory.
type buildInput struct {
	// Client is a connected buildkit client produced by endpoint discovery.
	Client *client.Client
	// Context is the absolute path to the build context directory.
	Context string
	// Dockerfile is the filename of the Dockerfile within the context dir.
	Dockerfile string
	// Target is the optional multi-stage build target.
	Target string
	// BuildArgs are passed as --build-arg equivalents.
	BuildArgs map[string]string
	// ExportDir is the host directory the target stage filesystem is written
	// to via the BuildKit local exporter.
	ExportDir string
}

// runBuild connects to buildkitd, solves the Dockerfile frontend for the
// requested target, and exports the resulting filesystem to input.ExportDir on
// the host. this is the key capability the reference provider lacks: instead of
// pushing an image to a registry, we use the "local" exporter so a file/dir can
// be pulled straight out onto the host with no docker cp / local-exec.
func runBuild(ctx context.Context, input buildInput) error {
	c := input.Client

	if err := os.MkdirAll(input.ExportDir, 0o755); err != nil {
		return fmt.Errorf("creating export dir: %w", err)
	}

	// the dockerfile frontend needs the context and the dockerfile dir synced
	// from the host into the build.
	contextFS, err := fsutil.NewFS(input.Context)
	if err != nil {
		return fmt.Errorf("preparing context fs: %w", err)
	}
	dockerfileDir := filepath.Dir(filepath.Join(input.Context, input.Dockerfile))
	dockerfileFS, err := fsutil.NewFS(dockerfileDir)
	if err != nil {
		return fmt.Errorf("preparing dockerfile fs: %w", err)
	}

	frontendAttrs := map[string]string{
		"filename": filepath.Base(input.Dockerfile),
	}
	if input.Target != "" {
		frontendAttrs["target"] = input.Target
	}
	for k, v := range input.BuildArgs {
		frontendAttrs["build-arg:"+k] = v
	}

	solveOpt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalMounts: map[string]fsutil.FS{
			"context":    contextFS,
			"dockerfile": dockerfileFS,
		},
		Exports: []client.ExportEntry{
			{
				Type:      client.ExporterLocal,
				OutputDir: input.ExportDir,
			},
		},
		Session: []session.Attachable{},
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		_, err := c.Solve(ctx, nil, solveOpt, nil)
		if err != nil {
			return fmt.Errorf("buildkit solve failed: %w", err)
		}
		return nil
	})

	return eg.Wait()
}
