// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

//go:build !linux

package provider

import (
	"context"
	"fmt"
	"runtime"
)

// startEmbeddedBuildkitd is unsupported on non-Linux hosts. buildkitd requires
// a Linux container runtime, so there is nothing to embed here.
func startEmbeddedBuildkitd(_ context.Context, _ embeddedOptions) (*resolvedEndpoint, error) {
	return nil, fmt.Errorf(
		"embedded_buildkitd is only supported on Linux; this host is %s. "+
			"Set buildkit_address or BUILDKIT_HOST, or use auto-discovery against OrbStack/Docker Desktop/Colima",
		runtime.GOOS,
	)
}
