# Build a multi-platform image and push it to a registry, with an SBOM and
# max-mode provenance attestation, build args, a build secret, and registry
# cache. Credentials come from the provider's registry_auth or docker config.

resource "buildkit_image" "app" {
  context    = "${path.module}/app"
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  target     = "runtime"

  args = {
    NODE_ENV = "production"
  }

  labels = {
    "org.opencontainers.image.source" = "https://github.com/org/app"
  }

  secrets = {
    npm_token = var.npm_token
  }

  publish {
    registry   = "ghcr.io"
    repository = "org/app"
    tags       = ["latest", var.version]
  }

  cache_from {
    type  = "registry"
    attrs = { ref = "ghcr.io/org/app:buildcache" }
  }

  cache_to {
    type  = "registry"
    attrs = { ref = "ghcr.io/org/app:buildcache", mode = "max" }
  }

  attestations {
    sbom       = true
    provenance = "max"
  }

  # Rebuild when the context content changes.
  triggers = {
    context = data.buildkit_context.app.digest
  }
}

data "buildkit_context" "app" {
  path = "${path.module}/app"
}

output "image_digest" {
  value = buildkit_image.app.image_digest
}

output "pushed" {
  value = buildkit_image.app.published
}
