// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/cruxstack/terraform-provider-buildkit/internal/provider/buildengine"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*imageResource)(nil)
	_ resource.ResourceWithConfigure   = (*imageResource)(nil)
	_ resource.ResourceWithImportState = (*imageResource)(nil)
)

type imageResource struct {
	provider *providerData
}

// publishModel maps one publish block.
type publishModel struct {
	Registry   types.String `tfsdk:"registry"`
	Repository types.String `tfsdk:"repository"`
	Tags       types.List   `tfsdk:"tags"`
	Push       types.Bool   `tfsdk:"push"`
	Insecure   types.Bool   `tfsdk:"insecure"`
}

// publishedModel maps one computed published entry.
type publishedModel struct {
	Registry   types.String `tfsdk:"registry"`
	Repository types.String `tfsdk:"repository"`
	Tag        types.String `tfsdk:"tag"`
	TagURL     types.String `tfsdk:"tag_url"`
	DigestURL  types.String `tfsdk:"digest_url"`
	Insecure   types.Bool   `tfsdk:"insecure"`
}

func publishedAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"registry":   types.StringType,
		"repository": types.StringType,
		"tag":        types.StringType,
		"tag_url":    types.StringType,
		"digest_url": types.StringType,
		"insecure":   types.BoolType,
	}
}

type cacheEntryModel struct {
	Type  types.String `tfsdk:"type"`
	Attrs types.Map    `tfsdk:"attrs"`
}

type sshModel struct {
	ID    types.String `tfsdk:"id"`
	Paths types.List   `tfsdk:"paths"`
}

type attestationsModel struct {
	SBOM       types.Bool   `tfsdk:"sbom"`
	Provenance types.String `tfsdk:"provenance"`
}

type imageResourceModel struct {
	ID                    types.String       `tfsdk:"id"`
	Context               types.String       `tfsdk:"context"`
	Dockerfile            types.String       `tfsdk:"dockerfile"`
	Target                types.String       `tfsdk:"target"`
	Platforms             types.Set          `tfsdk:"platforms"`
	Labels                types.Map          `tfsdk:"labels"`
	Args                  types.Map          `tfsdk:"args"`
	Secrets               types.Map          `tfsdk:"secrets"`
	SecretsBase64         types.Map          `tfsdk:"secrets_base64"`
	ForwardSSHAgentSocket types.Bool         `tfsdk:"forward_ssh_agent_socket"`
	SSH                   []sshModel         `tfsdk:"ssh"`
	Triggers              types.Map          `tfsdk:"triggers"`
	Publish               []publishModel     `tfsdk:"publish"`
	CacheFrom             []cacheEntryModel  `tfsdk:"cache_from"`
	CacheTo               []cacheEntryModel  `tfsdk:"cache_to"`
	Attestations          *attestationsModel `tfsdk:"attestations"`
	ImageDigest           types.String       `tfsdk:"image_digest"`
	ContextDigest         types.String       `tfsdk:"context_digest"`
	Published             types.List         `tfsdk:"published"`
}

func NewImageResource() resource.Resource {
	return &imageResource{}
}

func (r *imageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_image"
}

func (r *imageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds a container image from a Dockerfile with BuildKit and optionally pushes it to one or more registries in a single build. Supports multi-platform builds, build args, labels, build secrets, SSH agent forwarding, cache import/export, and SBOM/provenance attestations.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Synthetic resource id (equals `image_digest`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"context": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Path to the build context directory.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dockerfile": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Path to the Dockerfile, relative to `context`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Multi-stage build target to build.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"platforms": schema.SetAttribute{
				Required:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Target platforms, e.g. `[\"linux/amd64\", \"linux/arm64\"]`.",
				PlanModifiers:       []planmodifier.Set{},
			},
			"labels": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Image labels (equivalent to `LABEL` instructions).",
			},
			"args": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Build args (values for `ARG` instructions).",
			},
			"secrets": schema.MapAttribute{
				Optional:            true,
				Sensitive:           true,
				ElementType:         types.StringType,
				MarkdownDescription: "Build secrets in `id => value` form, exposed via `RUN --mount=type=secret,id=...`.",
			},
			"secrets_base64": schema.MapAttribute{
				Optional:            true,
				Sensitive:           true,
				ElementType:         types.StringType,
				MarkdownDescription: "Build secrets in `id => base64(value)` form, decoded before use.",
			},
			"forward_ssh_agent_socket": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Forward the host `SSH_AUTH_SOCK` as the `default` ssh mount (`RUN --mount=type=ssh`).",
			},
			"triggers": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Arbitrary map; any change forces the image to be rebuilt.",
				PlanModifiers:       []planmodifier.Map{mapRequiresReplaceIfConfigured()},
			},
			"image_digest": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The sha256 digest of the built image manifest.",
			},
			"context_digest": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Dockerignore-aware sha256 of the build context at build time.",
			},
			"published": schema.ListAttribute{
				Computed:            true,
				MarkdownDescription: "Resolved publish coordinates, including digest URLs.",
				ElementType:         types.ObjectType{AttrTypes: publishedAttrTypes()},
			},
		},
		Blocks: map[string]schema.Block{
			"ssh": schema.ListNestedBlock{
				MarkdownDescription: "Explicit ssh agent/socket forwards (`RUN --mount=type=ssh,id=...`).",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id":    schema.StringAttribute{Required: true, MarkdownDescription: "SSH mount id."},
						"paths": schema.ListAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Socket or key paths."},
					},
				},
			},
			"publish": schema.ListNestedBlock{
				MarkdownDescription: "A registry/repository and the tags to publish. Multiple blocks publish to multiple targets in a single build.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"registry":   schema.StringAttribute{Required: true, MarkdownDescription: "Registry host, e.g. `docker.io` or `ghcr.io`."},
						"repository": schema.StringAttribute{Required: true, MarkdownDescription: "Repository name, e.g. `org/app`."},
						"tags":       schema.ListAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Tags to publish."},
						"push":       schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true), MarkdownDescription: "Push to the registry. Defaults to `true`."},
						"insecure":   schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Allow pushing over plain HTTP / untrusted TLS (sets `registry.insecure=true`). Also used for digest lookups during refresh."},
					},
				},
			},
			"cache_from": schema.ListNestedBlock{
				MarkdownDescription: "Cache import sources (`--import-cache`).",
				NestedObject:        cacheBlockObject(),
			},
			"cache_to": schema.ListNestedBlock{
				MarkdownDescription: "Cache export targets (`--export-cache`).",
				NestedObject:        cacheBlockObject(),
			},
			"attestations": schema.SingleNestedBlock{
				MarkdownDescription: "Attach SBOM and/or provenance attestations to the build output.",
				Attributes: map[string]schema.Attribute{
					"sbom":       schema.BoolAttribute{Optional: true, MarkdownDescription: "Generate an SBOM attestation."},
					"provenance": schema.StringAttribute{Optional: true, MarkdownDescription: "Provenance mode: `min` or `max`."},
				},
			},
		},
	}
}

func cacheBlockObject() schema.NestedBlockObject {
	return schema.NestedBlockObject{
		Attributes: map[string]schema.Attribute{
			"type":  schema.StringAttribute{Required: true, MarkdownDescription: "Cache type: `registry`, `local`, `gha`, or `inline`."},
			"attrs": schema.MapAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: "Type-specific attributes (e.g. `ref`, `mode`, `dest`, `src`)."},
		},
	}
}

func (r *imageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.provider = data
}

func (r *imageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan imageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.build(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state imageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-resolve each pushed tag's digest. If any tracked tag is gone, drop the
	// resource so it gets rebuilt. If a digest changed, record drift.
	if !state.Published.IsNull() {
		var published []publishedModel
		resp.Diagnostics.Append(state.Published.ElementsAs(ctx, &published, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		anyTracked := false
		for i := range published {
			tagURL := published[i].TagURL.ValueString()
			if tagURL == "" {
				continue
			}
			anyTracked = true
			digest, err := r.provider.auth.DigestInsecure(tagURL, published[i].Insecure.ValueBool())
			if err != nil {
				if err == buildengine.ErrNotFound {
					resp.State.RemoveResource(ctx)
					return
				}
				resp.Diagnostics.AddError("reading published image", err.Error())
				return
			}
			published[i].DigestURL = types.StringValue(
				published[i].Registry.ValueString() + "/" + published[i].Repository.ValueString() + "@" + digest,
			)
		}
		if anyTracked {
			lv, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: publishedAttrTypes()}, published)
			resp.Diagnostics.Append(d...)
			if resp.Diagnostics.HasError() {
				return
			}
			state.Published = lv
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *imageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan imageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.build(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *imageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// We do not delete remote images; removing the resource only drops it from
	// state. This matches the conventional behavior of image-publishing
	// providers and avoids destructive registry operations.
}

func (r *imageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("image_digest"), req, resp)
}

// build runs the image build/push and fills computed fields.
func (r *imageResource) build(ctx context.Context, m *imageResourceModel, diags *diag.Diagnostics) {
	dockerfile := m.Dockerfile.ValueString()

	platforms := stringSet(ctx, m.Platforms, diags)
	if diags.HasError() {
		return
	}
	labels := stringMap(ctx, m.Labels, diags)
	args := stringMap(ctx, m.Args, diags)
	if diags.HasError() {
		return
	}

	secrets, err := mergeSecrets(ctx, m.Secrets, m.SecretsBase64, diags)
	if diags.HasError() {
		return
	}
	_ = err

	ssh := r.collectSSH(ctx, m, diags)
	if diags.HasError() {
		return
	}

	// expand publish blocks into image names.
	var names []string
	for _, p := range m.Publish {
		var tags []string
		diags.Append(p.Tags.ElementsAs(ctx, &tags, false)...)
		if diags.HasError() {
			return
		}
		push := p.Push.ValueBool()
		_ = push
		for _, t := range tags {
			names = append(names, fullRef(p.Registry.ValueString(), p.Repository.ValueString(), t))
		}
	}

	req := buildengine.Request{
		Context:    m.Context.ValueString(),
		Dockerfile: dockerfile,
		Target:     m.Target.ValueString(),
		Platforms:  platforms,
		BuildArgs:  args,
		Labels:     labels,
		Secrets:    secrets,
		SSH:        ssh,
		CacheFrom:  toCacheEntries(ctx, m.CacheFrom, diags),
		CacheTo:    toCacheEntries(ctx, m.CacheTo, diags),
		Auth:       r.provider.auth,
	}
	if diags.HasError() {
		return
	}
	if m.Attestations != nil {
		req.Attest = &buildengine.Attestations{
			SBOM:           m.Attestations.SBOM.ValueBool(),
			ProvenanceMode: m.Attestations.Provenance.ValueString(),
		}
	}
	if len(names) > 0 {
		push := anyPush(m.Publish)
		insecure := anyInsecure(m.Publish)
		req.Exports = append(req.Exports, buildengine.ImageExport(names, push, insecure))
	}

	resp, err := buildengine.Run(ctx, r.provider.client, req)
	if err != nil {
		diags.AddError("image build failed", err.Error())
		return
	}

	digest := resp.ImageDigest()
	m.ImageDigest = types.StringValue(digest)
	m.ID = types.StringValue(digest)

	// context digest (best effort, for drift visibility).
	if ch, err := buildengine.HashContext(m.Context.ValueString(), ""); err == nil {
		m.ContextDigest = types.StringValue(ch)
	} else {
		m.ContextDigest = types.StringNull()
	}

	// build published list.
	var published []publishedModel
	for _, p := range m.Publish {
		var tags []string
		diags.Append(p.Tags.ElementsAs(ctx, &tags, false)...)
		if diags.HasError() {
			return
		}
		for _, t := range tags {
			reg := p.Registry.ValueString()
			repo := p.Repository.ValueString()
			tagURL := fullRef(reg, repo, t)
			pm := publishedModel{
				Registry:   types.StringValue(reg),
				Repository: types.StringValue(repo),
				Tag:        types.StringValue(t),
				TagURL:     types.StringValue(tagURL),
				DigestURL:  types.StringNull(),
				Insecure:   types.BoolValue(p.Insecure.ValueBool()),
			}
			if p.Push.ValueBool() && digest != "" {
				pm.DigestURL = types.StringValue(reg + "/" + repo + "@" + digest)
			}
			published = append(published, pm)
		}
	}
	sort.Slice(published, func(i, j int) bool {
		return published[i].TagURL.ValueString() < published[j].TagURL.ValueString()
	})
	lv, d := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: publishedAttrTypes()}, published)
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	m.Published = lv
}

func (r *imageResource) collectSSH(ctx context.Context, m *imageResourceModel, diags *diag.Diagnostics) []buildengine.SSHConfig {
	var out []buildengine.SSHConfig
	if m.ForwardSSHAgentSocket.ValueBool() {
		out = append(out, buildengine.SSHConfig{ID: "default"})
	}
	for _, s := range m.SSH {
		var paths []string
		diags.Append(s.Paths.ElementsAs(ctx, &paths, false)...)
		if diags.HasError() {
			return nil
		}
		out = append(out, buildengine.SSHConfig{ID: s.ID.ValueString(), Paths: paths})
	}
	return out
}

// ---- helpers ----

func toCacheEntries(ctx context.Context, in []cacheEntryModel, diags *diag.Diagnostics) []buildengine.CacheEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]buildengine.CacheEntry, 0, len(in))
	for _, e := range in {
		attrs := map[string]string{}
		if !e.Attrs.IsNull() {
			diags.Append(e.Attrs.ElementsAs(ctx, &attrs, false)...)
		}
		out = append(out, buildengine.CacheEntry{Type: e.Type.ValueString(), Attrs: attrs})
	}
	return out
}

func mergeSecrets(ctx context.Context, plain, b64 types.Map, diags *diag.Diagnostics) (map[string][]byte, error) {
	out := map[string][]byte{}
	if !plain.IsNull() {
		m := map[string]string{}
		diags.Append(plain.ElementsAs(ctx, &m, false)...)
		for k, v := range m {
			out[k] = []byte(v)
		}
	}
	if !b64.IsNull() {
		m := map[string]string{}
		diags.Append(b64.ElementsAs(ctx, &m, false)...)
		for k, v := range m {
			dec, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				diags.AddError("invalid secrets_base64", fmt.Sprintf("secret %q is not valid base64: %s", k, err))
				continue
			}
			out[k] = dec
		}
	}
	return out, nil
}

func stringSet(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out
}

func stringMap(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	if m.IsNull() {
		return nil
	}
	out := map[string]string{}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out
}

func fullRef(registry, repository, tag string) string {
	registry = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(registry, "https://"), "http://"), "/")
	return registry + "/" + repository + ":" + tag
}

func anyPush(blocks []publishModel) bool {
	for _, b := range blocks {
		if b.Push.ValueBool() {
			return true
		}
	}
	return false
}

func anyInsecure(blocks []publishModel) bool {
	for _, b := range blocks {
		if b.Insecure.ValueBool() {
			return true
		}
	}
	return false
}
