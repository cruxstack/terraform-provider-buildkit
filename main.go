// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"flag"
	"log"

	"github.com/cruxstack/terraform-provider-buildkit/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// generate provider documentation into docs/ from the schema descriptions and
// the examples/ directory.
//go:generate go tool tfplugindocs generate --provider-name buildkit

// these are overridden via -ldflags at build time for release builds. for the
// local prototype they are informational only.
var (
	version = "dev"
)

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		// this address is what consumers reference in required_providers and in
		// the CLI dev_overrides block while prototyping locally.
		Address: "registry.terraform.io/cruxstack/buildkit",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
