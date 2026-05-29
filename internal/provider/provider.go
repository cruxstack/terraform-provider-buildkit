// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"sync"

	"github.com/cruxstack/terraform-provider-buildkit/internal/provider/buildengine"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	bkclient "github.com/moby/buildkit/client"
)

// ensure the implementation satisfies the provider interface.
var _ provider.Provider = (*buildkitProvider)(nil)

// buildkitProvider is the provider implementation.
type buildkitProvider struct {
	version string
}

// registryAuthModel maps a single registry_auth block.
type registryAuthModel struct {
	Address       types.String `tfsdk:"address"`
	Username      types.String `tfsdk:"username"`
	Password      types.String `tfsdk:"password"`
	Auth          types.String `tfsdk:"auth"`
	IdentityToken types.String `tfsdk:"identity_token"`
}

// providerModel maps provider schema data to a Go type.
type providerModel struct {
	BuildkitAddress      types.String        `tfsdk:"buildkit_address"`
	BuildkitAutodiscover types.Bool          `tfsdk:"buildkit_autodiscover"`
	DockerConfig         types.Bool          `tfsdk:"docker_config"`
	EmbeddedBuildkitd    types.Bool          `tfsdk:"embedded_buildkitd"`
	RegistryAuth         []registryAuthModel `tfsdk:"registry_auth"`
}

// providerData is passed to resources after Configure. it carries resolved
// registry auth plus a lazily-established buildkit connection: discovery and the
// ListWorkers ping only happen the first time a resource actually needs to build
// (so e.g. the buildkit_context data source, which only hashes a directory, does
// not require a reachable daemon).
type providerData struct {
	auth buildengine.AuthConfig
	opts resolveOptions

	mu       sync.Mutex
	resolved *resolvedEndpoint
	resolErr error
}

// client returns the shared buildkit client, establishing the connection on
// first use. It is safe for concurrent callers.
func (d *providerData) client(ctx context.Context) (*bkclient.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.resolved != nil {
		return d.resolved.client, nil
	}
	if d.resolErr != nil {
		return nil, d.resolErr
	}

	endpoint, err := resolveBuildkit(ctx, d.opts)
	if err != nil {
		d.resolErr = err
		return nil, err
	}
	tflog.Info(ctx, "resolved buildkit endpoint", map[string]any{"source": endpoint.source})

	d.resolved = endpoint
	if endpoint.cleanup != nil {
		registerProcessCleanup(endpoint.cleanup)
	}
	return endpoint.client, nil
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &buildkitProvider{version: version}
	}
}

func (p *buildkitProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "buildkit"
	resp.Version = p.version
}

func (p *buildkitProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds container images and filesystem artifacts from a Dockerfile using BuildKit, without requiring the Docker daemon as the build mechanism. " +
			"Connects to a local or remote `buildkitd` gRPC endpoint (with optional auto-discovery), and on Linux can automatically provision and supervise an embedded `buildkitd` (downloading a pinned, checksum-verified release) so builds work on hosts with no Docker or BuildKit preinstalled. Authenticates to registries via explicit credentials and/or the host Docker config.",
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
			"docker_config": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "When `true` (default), registry credentials are resolved from the host's Docker config (`~/.docker/config.json`) and its configured credential helpers (e.g. `osxkeychain`, `ecr-login`, `gcr`) as a fallback after explicit `registry_auth` blocks. Set to `false` for fully hermetic credential resolution from `registry_auth` only.",
			},
			"embedded_buildkitd": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Controls the embedded `buildkitd` (Linux only). Tri-state:\n\n" +
					"- **unset (default)**: *auto* — if no endpoint is configured or discovered, the provider provisions and supervises a `buildkitd` for the lifetime of the provider. The `buildkitd` (and `rootlesskit` for unprivileged use) binaries are resolved from `BUILDKIT_EMBEDDED_BIN_DIR`, a provider-managed cache, the host `PATH`, or a pinned, checksum-verified download from the upstream GitHub releases (requires outbound network on first use; results are cached). On non-Linux hosts this fallback is unavailable.\n" +
					"- **`true`**: *force* — always use the embedded `buildkitd` (after an explicit `buildkit_address` / `BUILDKIT_HOST`), skipping auto-discovery.\n" +
					"- **`false`**: *never* — disable the embedded `buildkitd` entirely; resolution fails if no endpoint is configured or discovered.\n\n" +
					"Rootless use (non-root euid) requires user-namespace support and `/etc/subuid` + `/etc/subgid` entries for the user. Set `BUILDKIT_EMBEDDED_BIN_DIR` to a directory containing the binaries for fully air-gapped operation.",
			},
		},
		Blocks: map[string]schema.Block{
			"registry_auth": schema.SetNestedBlock{
				MarkdownDescription: "Explicit registry credentials. Each block authenticates to one registry host and takes precedence over Docker config for that host.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"address": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Registry host this credential applies to (e.g. `docker.io`, `ghcr.io`, `123.dkr.ecr.us-east-1.amazonaws.com`). Scheme and trailing slash are ignored; Docker Hub aliases normalize to `docker.io`.",
						},
						"username": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Registry username.",
						},
						"password": schema.StringAttribute{
							Optional:            true,
							Sensitive:           true,
							MarkdownDescription: "Registry password or token.",
						},
						"auth": schema.StringAttribute{
							Optional:            true,
							Sensitive:           true,
							MarkdownDescription: "Base64-encoded `username:password`, as stored in Docker config.",
						},
						"identity_token": schema.StringAttribute{
							Optional:            true,
							Sensitive:           true,
							MarkdownDescription: "Identity token used to obtain a registry access token (OAuth2 flows).",
						},
					},
				},
			},
		},
	}
}

func (p *buildkitProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	autodiscover := true
	if !config.BuildkitAutodiscover.IsNull() && !config.BuildkitAutodiscover.IsUnknown() {
		autodiscover = config.BuildkitAutodiscover.ValueBool()
	}

	dockerConfig := true
	if !config.DockerConfig.IsNull() && !config.DockerConfig.IsUnknown() {
		dockerConfig = config.DockerConfig.ValueBool()
	}

	// embedded_buildkitd is tri-state:
	//   unset -> auto  (Linux fallback when nothing else is found; default)
	//   true  -> force (always use embedded, after explicit address/BUILDKIT_HOST)
	//   false -> never (disable embedded entirely)
	embedded := embeddedAuto
	if !config.EmbeddedBuildkitd.IsNull() && !config.EmbeddedBuildkitd.IsUnknown() {
		if config.EmbeddedBuildkitd.ValueBool() {
			embedded = embeddedForce
		} else {
			embedded = embeddedNever
		}
	}

	auth := buildengine.AuthConfig{
		Explicit:        map[string]buildengine.RegistryAuth{},
		UseDockerConfig: dockerConfig,
	}
	for _, ra := range config.RegistryAuth {
		addr := ra.Address.ValueString()
		auth.Explicit[addr] = buildengine.RegistryAuth{
			Address:       addr,
			Username:      ra.Username.ValueString(),
			Password:      ra.Password.ValueString(),
			Auth:          ra.Auth.ValueString(),
			IdentityToken: ra.IdentityToken.ValueString(),
		}
	}

	// The buildkit connection is established lazily (see providerData.client) so
	// configurations that only use the buildkit_context data source or the
	// registry data sources do not require a reachable daemon at plan time.
	data := &providerData{
		auth: auth,
		opts: resolveOptions{
			address:      config.BuildkitAddress.ValueString(),
			autodiscover: autodiscover,
			embedded:     embedded,
		},
	}

	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *buildkitProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewArtifactResource,
		NewImageResource,
	}
}

func (p *buildkitProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewContextDataSource,
		NewRegistryImageDataSource,
		NewImagesDataSource,
	}
}
