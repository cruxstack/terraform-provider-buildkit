# Contributing

Thanks for your interest in improving `terraform-provider-buildkit`.

## Development requirements

- Go (see `go.mod` for the minimum version).
- A reachable BuildKit endpoint for acceptance tests. Easiest options:
  - macOS: OrbStack / Docker Desktop / Colima running (auto-discovery finds the
    Docker-engine embedded BuildKit).
  - Linux/CI: a standalone daemon, e.g.
    `docker run -d --name buildkitd --privileged -p 1234:1234 moby/buildkit:v0.29.0 --addr tcp://0.0.0.0:1234`
    then `export BUILDKIT_HOST=tcp://127.0.0.1:1234`.

## Common tasks

```sh
make build      # build the provider binary
make fmt        # gofmt -s -w .
make vet        # go vet ./...
make lint       # golangci-lint run
make test       # unit tests
make testacc    # acceptance tests (requires TF_ACC=1 + a BuildKit endpoint)
make docs       # regenerate docs/ via tfplugindocs
```

## Local install for manual testing

```sh
go build -o terraform-provider-buildkit .
sed "s#DEV_OVERRIDE_DIR#$PWD#" dev.tfrc > /tmp/buildkit-dev.tfrc
export TF_CLI_CONFIG_FILE=/tmp/buildkit-dev.tfrc
cd examples/local && terraform apply -auto-approve
```

## Pull requests

- Run `make fmt vet lint test` before pushing.
- Add or update acceptance tests for behavior changes.
- Regenerate docs (`make docs`) when schema descriptions change.
- Add a `CHANGELOG.md` entry under `## [Unreleased]`.

## Cutting a release (maintainers)

1. Update `CHANGELOG.md`: move `[Unreleased]` items under a new `## vX.Y.Z`.
2. Ensure there is **no branch** named the same as the tag.
3. Tag and push:
   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```
4. The `release` GitHub Actions workflow runs GoReleaser, which builds all
   platforms, generates the `SHA256SUMS`, GPG-signs them, and publishes a GitHub
   Release.
5. Verify the release assets include the zips, `_SHA256SUMS`, `_SHA256SUMS.sig`,
   and `_manifest.json`.
6. The Terraform Registry webhook ingests the release automatically. For
   OpenTofu, ensure the signing key is registered and the provider submission
   issue has been processed.

### Signing key

Releases must be signed with a GPG key (RSA or DSA, **not** ECC). The CI
workflow imports `GPG_PRIVATE_KEY` / `PASSPHRASE` repository secrets. The
matching ASCII-armored public key must be uploaded to the Terraform Registry
namespace settings and submitted to the OpenTofu registry.
