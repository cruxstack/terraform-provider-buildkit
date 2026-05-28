// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/cruxstack/terraform-provider-buildkit/internal/provider/buildengine"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = (*contextDataSource)(nil)

type contextDataSource struct{}

type contextDataSourceModel struct {
	Path         types.String `tfsdk:"path"`
	Dockerignore types.String `tfsdk:"dockerignore"`
	Digest       types.String `tfsdk:"digest"`
	ID           types.String `tfsdk:"id"`
}

func NewContextDataSource() datasource.DataSource {
	return &contextDataSource{}
}

func (d *contextDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_context"
}

func (d *contextDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Computes a deterministic, dockerignore-aware SHA256 of a build context directory. Useful as a stable idempotency key (e.g. wired into a `buildkit_image` `triggers` map) so plans only change when the context content changes.",
		Attributes: map[string]schema.Attribute{
			"path": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Path to the build context directory.",
			},
			"dockerignore": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional path to a `.dockerignore` file to use instead of `<path>/.dockerignore`.",
			},
			"digest": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`sha256:`-prefixed digest of the included context content.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Equals `digest`.",
			},
		},
	}
}

func (d *contextDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg contextDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	digest, err := buildengine.HashContext(cfg.Path.ValueString(), cfg.Dockerignore.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("hashing build context", fmt.Sprintf("%s", err))
		return
	}
	cfg.Digest = types.StringValue(digest)
	cfg.ID = types.StringValue(digest)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
