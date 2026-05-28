// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccImageResource builds the bundled echo-app fixture as an image. When
// BUILDKIT_TEST_REGISTRY (e.g. "127.0.0.1:5000") is set it pushes and asserts a
// digest; otherwise it builds without pushing and asserts the apply succeeds.
// Requires TF_ACC=1 and a reachable BuildKit endpoint.
func TestAccImageResource(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests (requires a BuildKit endpoint)")
	}

	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "local", "fixtures", "echo-app"))
	if err != nil {
		t.Fatal(err)
	}

	registry := os.Getenv("BUILDKIT_TEST_REGISTRY")

	var config string
	var checks []resource.TestCheckFunc
	if registry == "" {
		config = fmt.Sprintf(`
provider "buildkit" {}

resource "buildkit_image" "test" {
  context    = %q
  dockerfile = "Dockerfile"
  target     = "build"
  platforms  = ["linux/amd64"]
}
`, fixture)
		checks = []resource.TestCheckFunc{
			resource.TestCheckResourceAttrSet("buildkit_image.test", "context_digest"),
		}
	} else {
		config = fmt.Sprintf(`
provider "buildkit" {}

resource "buildkit_image" "test" {
  context    = %q
  dockerfile = "Dockerfile"
  target     = "build"
  platforms  = ["linux/amd64"]

  publish {
    registry   = %q
    repository = "buildkit-tf/echo-app"
    tags       = ["acc"]
    insecure   = true
  }
}
`, fixture, registry)
		checks = []resource.TestCheckFunc{
			resource.TestMatchResourceAttr("buildkit_image.test", "image_digest", regexpSHA256()),
			resource.TestCheckResourceAttr("buildkit_image.test", "published.0.tag", "acc"),
			resource.TestCheckResourceAttrSet("buildkit_image.test", "published.0.digest_url"),
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
		},
	})
}
