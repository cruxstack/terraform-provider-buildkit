# terraform-provider-buildkit

A Terraform / OpenTofu provider that builds container images and filesystem
artifacts from a Dockerfile using [BuildKit](https://github.com/moby/buildkit)
directly over its gRPC API — **without** driving the Docker CLI, the Docker
daemon as a build mechanism, or `local-exec`.

It speaks to a local or remote `buildkitd`, can auto-discover the BuildKit
embedded in OrbStack / Docker Desktop / Colima, and can optionally supervise an
embedded rootless `buildkitd` on Linux.

## Features

- **`buildkit_image`** — build and push multi-platform images to one or more
  registries in a single build. Build args, labels, build secrets, SSH agent
  forwarding, registry/local/gha cache import & export, and SBOM + provenance
  attestations.
- **`buildkit_artifact`** — build a Dockerfile and extract a file (as a zip) or
  a directory tree from the built stage onto the host filesystem. Ideal for
  producing deployment packages (e.g. AWS Lambda zips) with no `docker cp` and
  no `local-exec`.
- **`buildkit_context`** — dockerignore-aware SHA256 of a build context, for use
  as a stable, plan-time idempotency key.
- **`buildkit_registry_image`** / **`buildkit_images`** — resolve or query
  images already in a registry.
- **Endpoint discovery** — explicit address, `BUILDKIT_HOST`, the Docker-engine
  embedded BuildKit `/grpc`, local sockets, or an embedded rootless buildkitd.
- **Registry auth** — explicit `registry_auth` blocks and/or the host Docker
  config (`~/.docker/config.json`) with credential helpers.
- Built on the Terraform Plugin Framework (protocol 6); works with Terraform and
  OpenTofu.

## How it compares

| Capability                               | this provider | `RutledgePaulV/buildkit` | `kreuzwerker/docker` |
| ---------------------------------------- | :-----------: | :----------------------: | :------------------: |
| Build + push images via BuildKit         |      yes      |           yes            |      via daemon      |
| Multi-platform images                    |      yes      |           yes            |       limited        |
| Build secrets / SSH forwarding           |      yes      |           yes            |       limited        |
| SBOM / provenance attestations           |      yes      |            no            |          no          |
| Cache import/export (registry/local/gha) |      yes      |            no            |       limited        |
| Extract a file/dir artifact to the host  |      yes      |            no            |          no          |
| Endpoint auto-discovery                  |      yes      |            no            |         n/a          |
| Embedded rootless buildkitd (Linux)      |      yes      |            no            |          no          |
| Docker config.json + credential helpers  |      yes      |            no            |         yes          |
| Plugin Framework (protocol 6)            |      yes      |        SDKv2 (5)         |      SDKv2 (5)       |

## Install

```hcl
terraform {
  required_providers {
    buildkit = {
      source  = "cruxstack/buildkit"
      version = "~> 1.0"
    }
  }
}

provider "buildkit" {}
```

## Quickstart

### Build and push an image

```hcl
resource "buildkit_image" "app" {
  context    = "${path.module}/app"
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]

  publish {
    registry   = "ghcr.io"
    repository = "org/app"
    tags       = ["latest"]
  }
}
```

### Extract a Lambda zip

```hcl
resource "buildkit_artifact" "lambda" {
  build_context     = "${path.module}/app"
  target            = "package"
  artifact_src_path = "/tmp/package.zip"
  artifact_src_type = "zip"
  artifact_dst_path = "${path.module}/dist/package.zip"
}

resource "aws_lambda_function" "this" {
  filename         = buildkit_artifact.lambda.artifact_path
  source_code_hash = buildkit_artifact.lambda.artifact_sha256
  # ...
}
```

## Choosing a BuildKit endpoint

A Linux container runtime is required to execute a Dockerfile's `RUN` steps, so
the provider needs a `buildkitd` to talk to. It is resolved in this order:

1. `buildkit_address` (provider config) — `tcp://`, `unix://`, or connection
   helpers like `docker-container://`.
2. `BUILDKIT_HOST` environment variable.
3. *(auto-discovery)* Docker-engine embedded BuildKit via the daemon `/grpc`
   endpoint (OrbStack / Docker Desktop / Colima).
4. *(auto-discovery)* Conventional local `buildkitd` sockets.
5. *(opt-in, Linux)* `embedded_buildkitd = true` — supervise a `buildkitd`
   binary found on PATH for the lifetime of the provider.

Set `buildkit_autodiscover = false` to require an explicit address /
`BUILDKIT_HOST` (recommended for hermetic CI).

> **Pushing images** requires a `buildkitd` whose worker can export images (a
> standalone `moby/buildkit` daemon, or a containerd-image-store worker). The
> Docker-engine `/grpc` BuildKit used by Docker Desktop does **not** support the
> `image` exporter; use it for `buildkit_artifact` and local builds, and a
> standalone daemon for pushing.

## Registry authentication

```hcl
provider "buildkit" {
  # explicit credentials (take precedence per-host)
  registry_auth {
    address  = "ghcr.io"
    username = "my-user"
    password = var.ghcr_token
  }

  # also consult ~/.docker/config.json + credential helpers (default true)
  docker_config = true
}
```

## Documentation

Generated documentation lives under [`docs/`](./docs) and on the Terraform and
OpenTofu registries:

- Resources: `buildkit_image`, `buildkit_artifact`
- Data sources: `buildkit_context`, `buildkit_registry_image`, `buildkit_images`

## Development

See [CONTRIBUTING.md](./CONTRIBUTING.md). Common tasks:

```sh
make build     # build the provider
make test      # unit tests
make testacc   # acceptance tests (TF_ACC=1 + a BuildKit endpoint)
make docs      # regenerate docs/
```

## License

[MPL-2.0](./LICENSE).
