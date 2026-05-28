# Resolve an existing image reference to its digest and metadata.

data "buildkit_registry_image" "base" {
  reference = "ghcr.io/org/app:latest"
}

output "digest" {
  value = data.buildkit_registry_image.base.digest
}

output "platforms" {
  value = data.buildkit_registry_image.base.platforms
}
