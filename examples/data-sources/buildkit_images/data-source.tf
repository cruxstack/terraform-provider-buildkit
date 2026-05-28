# Query a repository for images matching a tag pattern, required labels, and
# required platforms.

data "buildkit_images" "releases" {
  registry    = "ghcr.io"
  repository  = "org/app"
  tag_pattern = "^v[0-9]+\\.[0-9]+\\.[0-9]+$"

  platforms = ["linux/amd64", "linux/arm64"]

  labels = {
    "org.opencontainers.image.source" = "https://github.com/org/app"
  }

  most_recent_only = true
}

output "latest_release" {
  value = try(data.buildkit_images.releases.images[0].digest_url, null)
}
