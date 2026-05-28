terraform {
  required_providers {
    buildkit = {
      source = "cruxstack/buildkit"
    }
  }
}

provider "buildkit" {
  # no buildkit_address set: the provider auto-discovers an endpoint. on this
  # machine that resolves to the Docker-engine embedded BuildKit (OrbStack /
  # Docker Desktop / Colima) via the daemon /grpc endpoint. set
  # buildkit_address or BUILDKIT_HOST to use a specific endpoint, or
  # buildkit_autodiscover = false to require one explicitly.
}

resource "buildkit_artifact" "this" {
  build_context     = "${path.module}/fixtures/echo-app"
  dockerfile        = "Dockerfile"
  target            = "package"
  artifact_src_path = "/tmp/package.zip"
  artifact_src_type = "zip"
  artifact_dst_path = "${path.module}/dist/package.zip"
}

output "artifact_path" {
  value = buildkit_artifact.this.artifact_path
}

output "artifact_sha256" {
  value = buildkit_artifact.this.artifact_sha256
}
