// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccArtifactResource builds the bundled echo-app fixture and asserts the
// artifact is produced on the host. Requires TF_ACC=1 and a reachable BuildKit
// endpoint (auto-discovery, BUILDKIT_HOST, or buildkit_address).
func TestAccArtifactResource(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests (requires a BuildKit endpoint)")
	}

	dstDir := t.TempDir()
	dstPath := filepath.Join(dstDir, "package.zip")
	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "local", "fixtures", "echo-app"))
	if err != nil {
		t.Fatal(err)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccArtifactConfig(fixture, dstPath),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("buildkit_artifact.test", "artifact_path", dstPath),
					resource.TestCheckResourceAttrSet("buildkit_artifact.test", "artifact_sha256"),
					checkFileExists(dstPath),
				),
			},
		},
	})
}

// checkFileExists asserts the produced artifact actually exists on disk.
func checkFileExists(path string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("expected artifact at %s: %w", path, err)
		}
		return nil
	}
}

func testAccArtifactConfig(context, dst string) string {
	return fmt.Sprintf(`
provider "buildkit" {}

resource "buildkit_artifact" "test" {
  build_context     = %q
  target            = "package"
  artifact_src_path = "/tmp/package.zip"
  artifact_src_type = "zip"
  artifact_dst_path = %q
}
`, context, dst)
}
