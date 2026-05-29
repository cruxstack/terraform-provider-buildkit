// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

//go:build linux

package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccEmbeddedBuildkitdArtifact is the literal verification of the provider's
// "no Docker / no BuildKit preinstalled, no remote endpoint" goal: it forces the
// embedded buildkitd (which provisions buildkitd + the bundled runc, downloading
// a pinned release if needed) and builds the echo-app fixture, which contains
// RUN steps (apk add, zip). Success proves the provider can execute a real
// container build with nothing preinstalled.
//
// It is gated behind both TF_ACC and TF_ACC_EMBEDDED so it only runs in CI jobs
// explicitly provisioned for it (Linux, with user-namespace/subuid support for
// rootless, or running as root). On amd64/arm64 only, since those are the pinned
// architectures.
func TestAccEmbeddedBuildkitdArtifact(t *testing.T) {
	if os.Getenv("TF_ACC") == "" || os.Getenv("TF_ACC_EMBEDDED") == "" {
		t.Skip("set TF_ACC=1 and TF_ACC_EMBEDDED=1 to run the embedded-buildkitd acceptance test (Linux only)")
	}
	if runtime.GOOS != "linux" {
		t.Skipf("embedded buildkitd is Linux-only; host is %s", runtime.GOOS)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("no pinned buildkit release for linux/%s", runtime.GOARCH)
	}

	dstDir := t.TempDir()
	dstPath := filepath.Join(dstDir, "package.zip")
	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "local", "fixtures", "echo-app"))
	if err != nil {
		t.Fatal(err)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccEmbeddedArtifactConfig(fixture, dstPath),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("buildkit_artifact.test", "artifact_path", dstPath),
					resource.TestCheckResourceAttrSet("buildkit_artifact.test", "artifact_sha256"),
					checkFileExists(dstPath),
				),
			},
		},
	})
}

func testAccEmbeddedArtifactConfig(context, dst string) string {
	return fmt.Sprintf(`
provider "buildkit" {
  # force embedded buildkitd: do not auto-discover or use a remote endpoint.
  embedded_buildkitd    = true
  buildkit_autodiscover = false
}

resource "buildkit_artifact" "test" {
  build_context     = %q
  target            = "package"
  artifact_src_path = "/tmp/package.zip"
  artifact_src_type = "zip"
  artifact_dst_path = %q
}
`, context, dst)
}
