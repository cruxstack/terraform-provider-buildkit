// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = (*artifactResource)(nil)
	_ resource.ResourceWithConfigure = (*artifactResource)(nil)
)

type artifactResource struct {
	provider *providerData
}

// artifactResourceModel maps the schema for the buildkit_artifact
// resource. names intentionally mirror the v1 terraform module variables so the
// eventual module rewrite is close to a drop-in.
type artifactResourceModel struct {
	ID              types.String `tfsdk:"id"`
	BuildContext    types.String `tfsdk:"build_context"`
	Dockerfile      types.String `tfsdk:"dockerfile"`
	Target          types.String `tfsdk:"target"`
	BuildArgs       types.Map    `tfsdk:"build_args"`
	ArtifactSrcPath types.String `tfsdk:"artifact_src_path"`
	ArtifactSrcType types.String `tfsdk:"artifact_src_type"`
	ArtifactDstPath types.String `tfsdk:"artifact_dst_path"`
	ForceRebuildID  types.String `tfsdk:"force_rebuild_id"`
	ArtifactPath    types.String `tfsdk:"artifact_path"`
	ArtifactSHA256  types.String `tfsdk:"artifact_sha256"`
}

func NewArtifactResource() resource.Resource {
	return &artifactResource{}
}

func (r *artifactResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_artifact"
}

func (r *artifactResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds a Dockerfile via BuildKit and extracts an artifact (file as zip, or a directory) from the built stage onto the host filesystem.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Synthetic resource id (equals artifact_sha256).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"build_context": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Path to the Docker build context directory.",
			},
			"dockerfile": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Dockerfile path relative to the build context. Defaults to `Dockerfile`.",
			},
			"target": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional multi-stage build target whose filesystem is exported.",
			},
			"build_args": schema.MapAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Build arguments passed to the build.",
			},
			"artifact_src_path": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Path inside the built stage filesystem to extract.",
			},
			"artifact_src_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Either `zip` (default) or `directory`.",
			},
			"artifact_dst_path": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Destination path on the host for the extracted artifact.",
			},
			"force_rebuild_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Change this value to force a rebuild.",
			},
			"artifact_path": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Absolute host path of the produced artifact.",
			},
			"artifact_sha256": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "SHA256 of the produced artifact (file) used for drift detection.",
			},
		},
	}
}

func (r *artifactResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *artifactResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan artifactResourceModel
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

func (r *artifactResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state artifactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// if the produced artifact no longer exists on disk, drop it from state so
	// it gets rebuilt on the next apply.
	if state.ArtifactPath.ValueString() != "" {
		if _, err := os.Stat(state.ArtifactPath.ValueString()); err != nil {
			resp.State.RemoveResource(ctx)
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *artifactResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan artifactResourceModel
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

func (r *artifactResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state artifactResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// best-effort cleanup of the produced artifact.
	if p := state.ArtifactPath.ValueString(); p != "" {
		_ = os.RemoveAll(p)
	}
}

// build runs the buildkit build, exports the stage fs, extracts the requested
// artifact to the destination, and fills in computed fields on the model.
func (r *artifactResource) build(ctx context.Context, m *artifactResourceModel, diags *diag.Diagnostics) {
	dockerfile := m.Dockerfile.ValueString()
	if dockerfile == "" {
		dockerfile = "Dockerfile"
		m.Dockerfile = types.StringValue(dockerfile)
	}
	srcType := m.ArtifactSrcType.ValueString()
	if srcType == "" {
		srcType = "zip"
		m.ArtifactSrcType = types.StringValue(srcType)
	}

	buildArgs := map[string]string{}
	if !m.BuildArgs.IsNull() {
		diags.Append(m.BuildArgs.ElementsAs(ctx, &buildArgs, false)...)
		if diags.HasError() {
			return
		}
	}

	buildCtx, err := filepath.Abs(m.BuildContext.ValueString())
	if err != nil {
		diags.AddError("invalid build_context", err.Error())
		return
	}

	exportDir, err := os.MkdirTemp("", "buildkit-artifact-export-")
	if err != nil {
		diags.AddError("creating temp export dir", err.Error())
		return
	}
	defer os.RemoveAll(exportDir)

	err = runBuild(ctx, buildInput{
		Client:     r.provider.client,
		Context:    buildCtx,
		Dockerfile: dockerfile,
		Target:     m.Target.ValueString(),
		BuildArgs:  buildArgs,
		ExportDir:  exportDir,
	})
	if err != nil {
		diags.AddError("build failed", err.Error())
		return
	}

	// the requested src path is relative to the exported stage root.
	srcPath := filepath.Join(exportDir, filepath.Clean("/"+m.ArtifactSrcPath.ValueString()))
	dstPath, err := filepath.Abs(m.ArtifactDstPath.ValueString())
	if err != nil {
		diags.AddError("invalid artifact_dst_path", err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		diags.AddError("creating destination dir", err.Error())
		return
	}

	switch srcType {
	case "zip":
		if err := zipPath(srcPath, dstPath); err != nil {
			diags.AddError("packaging zip artifact", err.Error())
			return
		}
	case "directory":
		if err := copyTree(srcPath, dstPath); err != nil {
			diags.AddError("copying directory artifact", err.Error())
			return
		}
	default:
		diags.AddError("invalid artifact_src_type", fmt.Sprintf("expected zip or directory, got %q", srcType))
		return
	}

	sum, err := sha256File(dstPath)
	if err != nil {
		// directories won't hash as a single file; fall back to a tree hash.
		sum, err = sha256Tree(dstPath)
		if err != nil {
			diags.AddError("hashing artifact", err.Error())
			return
		}
	}

	m.ArtifactPath = types.StringValue(dstPath)
	m.ArtifactSHA256 = types.StringValue(sum)
	m.ID = types.StringValue(sum)
}

// ---- helpers -----------------------------------------------------------------

func zipPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	defer zw.Close()

	if !info.IsDir() {
		// src is already a single file (e.g. a pre-built package.zip produced
		// inside the dockerfile). copy it through verbatim rather than nesting
		// it inside another zip, matching the v1 `docker cp` behavior.
		zw.Close()
		out.Close()
		return copyFile(src, dst, info.Mode())
	}
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return addFileToZip(zw, path, rel)
	})
}

func addFileToZip(zw *zip.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, fi.Mode())
		}
		return copyFile(path, target, fi.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func sha256File(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Tree(root string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		h.Write([]byte(rel))
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
