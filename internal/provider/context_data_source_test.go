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

// TestAccContextDataSource hashes a context directory and asserts a sha256
// digest is produced. Requires TF_ACC=1 but does not require a BuildKit
// endpoint: the provider connects lazily and this data source only hashes a
// local directory.
func TestAccContextDataSource(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests (requires a BuildKit endpoint)")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "buildkit" {}

data "buildkit_context" "test" {
  path = %q
}
`, dir),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("data.buildkit_context.test", "digest", regexpSHA256()),
				),
			},
		},
	})
}
