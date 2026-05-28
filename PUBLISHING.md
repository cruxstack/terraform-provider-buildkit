# Publishing to the Terraform and OpenTofu registries

This provider publishes the same GPG-signed GitHub Release artifacts to both the
Terraform Registry (`registry.terraform.io`) and the OpenTofu Registry
(`registry.opentofu.org`). The release pipeline (GoReleaser via
`.github/workflows/release.yml`) already produces everything both registries
require:

- `terraform-provider-buildkit_{VERSION}_{OS}_{ARCH}.zip` for each platform
- `terraform-provider-buildkit_{VERSION}_SHA256SUMS`
- `terraform-provider-buildkit_{VERSION}_SHA256SUMS.sig` (GPG, binary)
- `terraform-provider-buildkit_{VERSION}_manifest.json`

## One-time setup

### 1. Repository

- The GitHub repository must be **public** and named
  `terraform-provider-buildkit` (it is).

### 2. Signing key

1. Generate a GPG key (RSA or DSA — **not** ECC):
   ```sh
   gpg --full-generate-key
   ```
2. Export the **private** key and add it as the `GPG_PRIVATE_KEY` repository
   secret, and its passphrase as `PASSPHRASE`:
   ```sh
   gpg --armor --export-secret-keys "<KEY_ID>"
   ```
3. Export the **public** key (used by both registries):
   ```sh
   gpg --armor --export "<KEY_ID>" > buildkit-signing-key.asc
   ```

### 3. Terraform Registry

1. Sign in to https://registry.terraform.io with the GitHub account/org that
   owns the repo.
2. User Settings → Signing Keys → add the **public** key for the `cruxstack`
   namespace.
3. Publish → Provider → select the repository. This installs a `release`
   webhook; future tagged releases are ingested automatically.

### 4. OpenTofu Registry

1. Submit the signing key + provider via the issue forms in
   https://github.com/opentofu/registry (do **not** open PRs directly):
   - "Submit new Provider Signing Key" (attach `buildkit-signing-key.asc`)
   - "Submit new Provider" (namespace `cruxstack`, name `buildkit`)
2. The automation validates the latest GitHub Release's signatures against the
   submitted key.

## Cutting a release

```sh
# 1. Move CHANGELOG [Unreleased] entries under a new version heading.
# 2. Ensure no branch shares the tag name.
git tag v1.0.0
git push origin v1.0.0
```

The `release` workflow runs GoReleaser, signs the checksums, and publishes the
GitHub Release. Within a few minutes:

- The Terraform Registry webhook ingests the new version.
- The OpenTofu Registry picks up the new tag on its next sync (or immediately
  after the initial submission is processed).

## Verifying

```sh
# Terraform
terraform init   # with required_providers source = "cruxstack/buildkit"

# OpenTofu
tofu init        # resolves from registry.opentofu.org/cruxstack/buildkit
```

Check published versions:

```sh
curl -s https://registry.terraform.io/v1/providers/cruxstack/buildkit/versions | jq .
curl -s https://registry.opentofu.org/v1/providers/cruxstack/buildkit/versions | jq .
```
