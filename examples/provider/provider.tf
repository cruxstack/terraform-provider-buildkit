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

# To target an explicit endpoint (e.g. a standalone rootless buildkitd in CI):
#
# provider "buildkit" {
#   buildkit_address      = "tcp://127.0.0.1:1234"
#   buildkit_autodiscover = false
# }
