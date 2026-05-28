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

var (
	_ datasource.DataSource              = (*registryImageDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*registryImageDataSource)(nil)
)

type registryImageDataSource struct {
	provider *providerData
}

type registryImageDataSourceModel struct {
	Reference types.String `tfsdk:"reference"`
	Insecure  types.Bool   `tfsdk:"insecure"`
	Digest    types.String `tfsdk:"digest"`
	DigestURL types.String `tfsdk:"digest_url"`
	MediaType types.String `tfsdk:"media_type"`
	Platforms types.List   `tfsdk:"platforms"`
	Labels    types.Map    `tfsdk:"labels"`
	Created   types.String `tfsdk:"created"`
	ID        types.String `tfsdk:"id"`
}

func NewRegistryImageDataSource() datasource.DataSource {
	return &registryImageDataSource{}
}

func (d *registryImageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_image"
}

func (d *registryImageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Resolves an existing image reference in a registry to its digest, platforms, labels, and creation time. Uses the provider's registry credentials.",
		Attributes: map[string]schema.Attribute{
			"reference": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Image reference, e.g. `ghcr.io/org/app:latest` or `ghcr.io/org/app@sha256:...`.",
			},
			"insecure": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Allow plain-HTTP / untrusted TLS when contacting the registry.",
			},
			"digest": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`sha256:`-prefixed manifest digest.",
			},
			"digest_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Fully-qualified digest reference (`registry/repo@sha256:...`).",
			},
			"media_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Manifest media type.",
			},
			"platforms": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Platforms present in the image (or index).",
			},
			"labels": schema.MapAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Image config labels (single-platform images only).",
			},
			"created": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC3339 creation timestamp, if available.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Equals `digest_url`.",
			},
		},
	}
}

func (d *registryImageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	d.provider = data
}

func (d *registryImageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg registryImageDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	info, err := d.provider.auth.ResolveImageInsecure(cfg.Reference.ValueString(), cfg.Insecure.ValueBool())
	if err != nil {
		if err == buildengine.ErrNotFound {
			resp.Diagnostics.AddError("image not found", fmt.Sprintf("reference %q does not exist", cfg.Reference.ValueString()))
			return
		}
		resp.Diagnostics.AddError("resolving image", err.Error())
		return
	}

	cfg.Digest = types.StringValue(info.Digest)
	cfg.DigestURL = types.StringValue(info.DigestURL)
	cfg.MediaType = types.StringValue(info.MediaType)
	cfg.Created = types.StringValue(info.Created)
	cfg.ID = types.StringValue(info.DigestURL)

	pl, diags := types.ListValueFrom(ctx, types.StringType, info.Platforms)
	resp.Diagnostics.Append(diags...)
	cfg.Platforms = pl

	lbl, diags := types.MapValueFrom(ctx, types.StringType, info.Labels)
	resp.Diagnostics.Append(diags...)
	cfg.Labels = lbl

	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
