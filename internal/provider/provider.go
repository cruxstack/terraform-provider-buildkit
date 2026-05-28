// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	bkclient "github.com/moby/buildkit/client"
)

// ensure the implementation satisfies the provider interface.
var _ provider.Provider = (*artifactPackagerProvider)(nil)

// artifactPackagerProvider is the provider implementation.
type artifactPackagerProvider struct {
	version string
}

// providerModel maps provider schema data to a Go type.
type providerModel struct {
	BuildkitAddress      types.String `tfsdk:"buildkit_address"`
	BuildkitAutodiscover types.Bool   `tfsdk:"buildkit_autodiscover"`
}

// providerData is passed to resources after Configure. it carries the resolved,
// connected buildkit client so each resource does not re-run discovery.
type providerData struct {
	client *bkclient.Client
	source string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &artifactPackagerProvider{version: version}
	}
}

func (p *artifactPackagerProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "buildkit"
	resp.Version = p.version
}

func (p *artifactPackagerProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds artifacts from a Dockerfile using BuildKit and extracts them to the host filesystem, without requiring the Docker daemon/socket as a build mechanism. Connects to a local or remote buildkitd gRPC endpoint, with optional auto-discovery.",
		Attributes: map[string]schema.Attribute{
			"buildkit_address": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "gRPC address of a running buildkitd (e.g. `unix:///run/buildkit/buildkitd.sock`, `tcp://127.0.0.1:1234`, or `docker-container://buildkitd`). When set, auto-discovery is skipped. When unset, the `BUILDKIT_HOST` environment variable is used, then auto-discovery (if enabled).",
			},
			"buildkit_autodiscover": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "When `true` (default) and no explicit address or `BUILDKIT_HOST` is set, the provider attempts to discover a BuildKit endpoint: " +
					"the Docker-engine embedded BuildKit via the daemon `/grpc` endpoint (OrbStack / Docker Desktop / Colima), then conventional local buildkitd sockets. " +
					"Set to `false` to require an explicit address / `BUILDKIT_HOST` (recommended for hermetic CI).",
			},
		},
	}
}

func (p *artifactPackagerProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// autodiscover defaults to true when not set.
	autodiscover := true
	if !config.BuildkitAutodiscover.IsNull() && !config.BuildkitAutodiscover.IsUnknown() {
		autodiscover = config.BuildkitAutodiscover.ValueBool()
	}

	endpoint, err := resolveBuildkit(ctx, config.BuildkitAddress.ValueString(), autodiscover)
	if err != nil {
		resp.Diagnostics.AddError("Could not connect to BuildKit", err.Error())
		return
	}

	tflog.Info(ctx, "resolved buildkit endpoint", map[string]any{"source": endpoint.source})

	data := &providerData{client: endpoint.client, source: endpoint.source}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *artifactPackagerProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewArtifactResource,
	}
}

func (p *artifactPackagerProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
