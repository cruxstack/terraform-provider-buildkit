// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/cruxstack/terraform-provider-buildkit/internal/provider/buildengine"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*imagesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*imagesDataSource)(nil)
)

type imagesDataSource struct {
	provider *providerData
}

type imagesDataSourceModel struct {
	Registry       types.String `tfsdk:"registry"`
	Repository     types.String `tfsdk:"repository"`
	TagPattern     types.String `tfsdk:"tag_pattern"`
	Labels         types.Map    `tfsdk:"labels"`
	Platforms      types.Set    `tfsdk:"platforms"`
	MostRecentOnly types.Bool   `tfsdk:"most_recent_only"`
	Insecure       types.Bool   `tfsdk:"insecure"`
	Images         types.List   `tfsdk:"images"`
	ID             types.String `tfsdk:"id"`
}

func imageResultAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"reference":  types.StringType,
		"digest":     types.StringType,
		"digest_url": types.StringType,
		"media_type": types.StringType,
		"platforms":  types.ListType{ElemType: types.StringType},
		"labels":     types.MapType{ElemType: types.StringType},
		"created":    types.StringType,
	}
}

func NewImagesDataSource() datasource.DataSource {
	return &imagesDataSource{}
}

func (d *imagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_images"
}

func (d *imagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Queries a registry repository for images, filtered by a tag regex, required labels, and required platforms.",
		Attributes: map[string]schema.Attribute{
			"registry":   schema.StringAttribute{Required: true, MarkdownDescription: "Registry host to query."},
			"repository": schema.StringAttribute{Required: true, MarkdownDescription: "Repository name to query."},
			"tag_pattern": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Regular expression to filter tags. Empty matches all tags.",
			},
			"labels": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Required label key/values; only images carrying all of them match.",
			},
			"platforms": schema.SetAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Required platforms; only images supporting all of them match.",
			},
			"most_recent_only": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "When `true`, return only the most recently created matching image.",
			},
			"insecure": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Allow plain-HTTP / untrusted TLS when contacting the registry.",
			},
			"images": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.ObjectType{AttrTypes: imageResultAttrTypes()},
				MarkdownDescription: "Matching images.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Synthetic id derived from the query.",
			},
		},
	}
}

func (d *imagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *imagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg imagesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	labels := stringMap(ctx, cfg.Labels, &resp.Diagnostics)
	platforms := stringSet(ctx, cfg.Platforms, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	results, err := d.provider.auth.QueryImages(buildengine.ImageQuery{
		Registry:   cfg.Registry.ValueString(),
		Repository: cfg.Repository.ValueString(),
		TagPattern: cfg.TagPattern.ValueString(),
		Labels:     labels,
		Platforms:  platforms,
		Insecure:   cfg.Insecure.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("querying registry", err.Error())
		return
	}

	if cfg.MostRecentOnly.ValueBool() && len(results) > 1 {
		// results are sorted by reference; prefer created desc when available.
		best := results[0]
		for _, r := range results[1:] {
			if r.Created > best.Created {
				best = r
			}
		}
		results = []*buildengine.ImageInfo{best}
	}

	objs := make([]attr.Value, 0, len(results))
	for _, r := range results {
		pl, diags := types.ListValueFrom(ctx, types.StringType, r.Platforms)
		resp.Diagnostics.Append(diags...)
		lbl, diags := types.MapValueFrom(ctx, types.StringType, r.Labels)
		resp.Diagnostics.Append(diags...)
		obj, diags := types.ObjectValue(imageResultAttrTypes(), map[string]attr.Value{
			"reference":  types.StringValue(r.Reference),
			"digest":     types.StringValue(r.Digest),
			"digest_url": types.StringValue(r.DigestURL),
			"media_type": types.StringValue(r.MediaType),
			"platforms":  pl,
			"labels":     lbl,
			"created":    types.StringValue(r.Created),
		})
		resp.Diagnostics.Append(diags...)
		objs = append(objs, obj)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	lv, diags := types.ListValue(types.ObjectType{AttrTypes: imageResultAttrTypes()}, objs)
	resp.Diagnostics.Append(diags...)
	cfg.Images = lv
	cfg.ID = types.StringValue(cfg.Registry.ValueString() + "/" + cfg.Repository.ValueString())
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
