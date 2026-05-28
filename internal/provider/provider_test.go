// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories wires the provider for acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"buildkit": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck validates that the environment is ready for acceptance tests.
// acceptance tests require a reachable BuildKit endpoint (via auto-discovery,
// BUILDKIT_HOST, or buildkit_address).
func testAccPreCheck(t *testing.T) {
	t.Helper()
	// no required env vars yet; auto-discovery covers the common local case.
}
