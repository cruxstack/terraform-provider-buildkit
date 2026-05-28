# Prototype: terraform-provider-buildkit

A **local-only prototype** of a Terraform/OpenTofu provider that builds an
arbitrary Dockerfile and extracts an artifact (a file as a zip, or a directory)
to the host filesystem — **without using the Docker daemon/socket** and
**without `local-exec`**.

This is the proposed engine for a v2.0.0 of the
`terraform-docker-artifact-packager` module. It is intentionally unpublished;
install is via Terraform/OpenTofu `dev_overrides`.

## How it works

- Speaks the **BuildKit gRPC API** directly (via
  `github.com/moby/buildkit/client`). It dials a running `buildkitd` at
  `buildkit_address` (or `BUILDKIT_HOST`). It never touches
  `/var/run/docker.sock`.
- Builds with the `dockerfile.v0` frontend (supports multi-stage `target`,
  `build_args`, arbitrary `RUN` steps — Go, Node, Python, etc.).
- Uses the BuildKit **`local` exporter** to write the target stage's filesystem
  to a temp dir, then extracts `artifact_src_path` to `artifact_dst_path`. This
  is the capability the closest existing provider (`RutledgePaulV/buildkit`, the
  reference for this prototype) lacks — it only pushes images to a registry.
- Computes a `artifact_sha256` so re-applies are idempotent (no spurious Lambda
  redeploys), and re-creates the artifact if the on-disk file disappears.

## Endpoint discovery

The provider needs a BuildKit gRPC endpoint. It resolves one in this order:

1. **`buildkit_address`** (provider config) — when set, used as-is and discovery
   is skipped. Supports `tcp://`, `unix://`, and connhelper schemes like
   `docker-container://`.
2. **`BUILDKIT_HOST`** environment variable — same address formats.
3. *(auto-discovery, when `buildkit_autodiscover = true`, the default)*
   **Docker-engine embedded BuildKit** via the daemon's `/grpc` endpoint — this
   is what OrbStack / Docker Desktop / Colima expose with their default `docker`
   driver. The Docker socket is used **purely as transport** to reach BuildKit;
   the provider does not use the docker CLI, the `kreuzwerker/docker` provider,
   or `local-exec`.
4. *(auto-discovery)* **Conventional local buildkitd sockets**
   (`$XDG_RUNTIME_DIR/buildkit/buildkitd.sock`, `/run/buildkit/buildkitd.sock`).

Each candidate is validated with a `ListWorkers` ping before it is accepted. Set
`buildkit_autodiscover = false` to require an explicit address / `BUILDKIT_HOST`
and never touch the Docker socket (recommended for hermetic CI).

### Why an endpoint is required at all

Executing a Dockerfile's `RUN` steps needs a Linux container runtime.
`buildkitd` runs only on Linux (and Windows containers). So:

- **Linux CI runners:** run `buildkitd` directly (rootless) and point at it via
  `BUILDKIT_HOST` — fully daemonless / no Docker socket.
- **macOS dev laptops:** auto-discovery finds the BuildKit embedded in OrbStack
  / Docker Desktop / Colima. There is no native macOS container runtime; this is
  unavoidable for any tool that runs Dockerfiles.

## Try it locally

1. Build the provider binary:

   ```sh
   go build -o terraform-provider-buildkit .
   ```

2. Provide a BuildKit endpoint. Easiest on macOS: just have OrbStack / Docker
   Desktop / Colima running — auto-discovery will find it. The example sets no
   `buildkit_address`, so it uses discovery.

   To use an explicit standalone endpoint instead (e.g. on Linux CI), run one
   and export `BUILDKIT_HOST`:

   ```sh
   docker run -d --name buildkit-proto --privileged \
     -p 127.0.0.1:13340:13340 \
     moby/buildkit:v0.18.2 --addr tcp://0.0.0.0:13340
   export BUILDKIT_HOST=tcp://127.0.0.1:13340
   ```

3. Generate a dev-override CLI config pointing at this directory:

   ```sh
   sed "s#DEV_OVERRIDE_DIR#$PWD#" dev.tfrc > /tmp/buildkit-dev.tfrc
   export TF_CLI_CONFIG_FILE=/tmp/buildkit-dev.tfrc
   ```

4. Apply the example (works with `terraform` or `tofu`):

   ```sh
   cd examples/local
   terraform apply -auto-approve
   unzip -l dist/package.zip
   ```

## Provider configuration

| Attribute               | Req | Description                                                                                 |
| ----------------------- | --- | ------------------------------------------------------------------------------------------- |
| `buildkit_address`      | no  | Explicit BuildKit gRPC address. When set, discovery is skipped.                             |
| `buildkit_autodiscover` | no  | Default `true`. Auto-discover Docker-engine `/grpc` then local sockets. `false` to disable. |

## Resource: `buildkit_artifact`

| Attribute           | Req | Description                                                |
| ------------------- | --- | ---------------------------------------------------------- |
| `build_context`     | yes | Path to the Docker build context.                          |
| `dockerfile`        | no  | Dockerfile name relative to context. Default `Dockerfile`. |
| `target`            | no  | Multi-stage target whose filesystem is exported.           |
| `build_args`        | no  | Map of build arguments.                                    |
| `artifact_src_path` | yes | Path inside the built stage to extract.                    |
| `artifact_src_type` | no  | `zip` (default) or `directory`.                            |
| `artifact_dst_path` | yes | Host destination for the artifact.                         |
| `force_rebuild_id`  | no  | Change to force a rebuild.                                 |
| `artifact_path`     | out | Absolute host path of the produced artifact.               |
| `artifact_sha256`   | out | Content hash, used for drift detection.                    |

## Status / TODO (not done in this prototype)

- Embedded rootless buildkitd on Linux (no separate endpoint needed).
- `force_rebuild_id` / build-arg / context-hash plan-time triggers (currently a
  rebuild happens on create/update only; content hash is computed post-build).
- Registry auth for private base images, secrets, ssh forwarding.
- Acceptance tests, docs generation, GoReleaser + registry publishing.
