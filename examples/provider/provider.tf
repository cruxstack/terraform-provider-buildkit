terraform {
  required_providers {
    buildkit = {
      source = "cruxstack/buildkit"
    }
  }
}

# With no configuration, the provider auto-discovers a BuildKit endpoint:
# BUILDKIT_HOST, then the Docker-engine embedded BuildKit (OrbStack / Docker
# Desktop / Colima) via the daemon /grpc endpoint, then local buildkitd sockets.
provider "buildkit" {}

# Target an explicit endpoint (e.g. a standalone rootless buildkitd in CI) and
# authenticate to a private registry:
#
# provider "buildkit" {
#   buildkit_address      = "tcp://127.0.0.1:1234"
#   buildkit_autodiscover = false
#
#   registry_auth {
#     address  = "ghcr.io"
#     username = "my-user"
#     password = var.ghcr_token
#   }
# }

# On Linux, run an embedded buildkitd (requires a `buildkitd` binary, and
# `rootlesskit` for unprivileged use, on PATH):
#
# provider "buildkit" {
#   buildkit_autodiscover = false
#   embedded_buildkitd    = true
# }
