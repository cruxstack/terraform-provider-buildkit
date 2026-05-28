# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `buildkit_image` resource: build and (optionally) push multi-platform images
  via BuildKit. Supports `platforms`, `target`, `labels`, `args`, `secrets`,
  `secrets_base64`, `forward_ssh_agent_socket`, `ssh`, `triggers`, repeatable
  `publish` blocks (multi-registry / multi-tag in a single solve), `cache_from`
  / `cache_to`, and an `attestations` block (SBOM + provenance). Exposes
  `image_digest`, `context_digest`, and a computed `published` list. Supports
  `terraform import` by digest URL.
- `buildkit_context` data source: dockerignore-aware SHA256 of a build context,
  usable as a stable, plan-time-known idempotency key.
- `buildkit_registry_image` data source: resolve a `registry/repo:tag` (or
  `@digest`) reference to its digest, platforms, labels, and creation time.
- `buildkit_images` data source: query a registry repository for images by tag
  regex, required labels, and required platforms.
- `insecure` option on `buildkit_image` publish blocks and on both registry data
  sources, for plain-HTTP / untrusted-TLS registries.
- Provider-level registry authentication: repeatable `registry_auth` blocks plus
  automatic `~/.docker/config.json` + credential-helper resolution
  (`docker_config`, default `true`).
- `embedded_buildkitd` (Linux only, opt-in): locate a `buildkitd` binary on PATH
  and supervise it (rootless-aware) on a private socket for the lifetime of the
  provider when no endpoint is configured. Requires `buildkitd` (and
  `rootlesskit` for unprivileged use) on the host; errors clearly otherwise and
  on non-Linux platforms.
- Build progress is now streamed to Terraform logs (`tflog`).

### Changed

- **BREAKING** `buildkit_artifact`: `force_rebuild_id` (string) replaced by
  `triggers` (map of string) with replace-on-change semantics.
- **BREAKING** `buildkit_artifact`: `artifact_src_type` is now validated to be
  one of `zip` or `directory`.
- **BREAKING** `buildkit_image`: `publish`, `ssh`, `cache_from`, and `cache_to`
  are now sets rather than ordered lists, so reordering blocks no longer shows a
  diff or forces a rebuild.
- `buildkit_image`: all `publish` blocks must now agree on the `push` and
  `insecure` flags; mixed values are rejected at apply time instead of being
  silently OR-ed together (which could downgrade a secure push to insecure).
- `buildkit_image`: with no `publish` blocks the image is now built and a
  deterministic `image_digest` is still computed (previously `image_digest` was
  an empty string).
- `buildkit_image` import now parses the `registry/repo@sha256:...` reference
  into `image_digest` and a seeded `published` entry instead of storing the raw
  id in `image_digest`.
- `buildkit_artifact` `Read` now re-hashes the destination artifact and detects
  drift: a missing file or a content hash that differs from the one recorded at
  build time drops the resource from state so the next apply rebuilds it.
- The BuildKit connection is now established lazily on first build, so
  configurations that use only `buildkit_context` (or the registry data sources)
  no longer require a reachable daemon at plan time.
- Registry credentials resolved from `~/.docker/config.json` now work for every
  registry a build touches; previously only the first host queried during a
  provider's lifetime resolved correctly.
- `buildkit_image`: `attestations.provenance` is validated to be `min` or `max`,
  and `cache_from`/`cache_to` `type` is validated against the supported
  exporters.
- Embedded buildkitd is now started in its own process group and its whole group
  is signalled on cleanup, avoiding orphaned `buildkitd`/`runc` processes.
- A warning is logged when connecting to a non-loopback `tcp://` endpoint, which
  the BuildKit client speaks unencrypted.
- Bumped `github.com/moby/buildkit` to `v0.29.0`.

### Notes

- This is the first registry-published release line. The previous local-only
  prototype is superseded.

[unreleased]: https://github.com/cruxstack/terraform-provider-buildkit/compare/main...HEAD
