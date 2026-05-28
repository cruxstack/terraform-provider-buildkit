# Compute a dockerignore-aware hash of a build context. Wire it into a
# resource's `triggers` so plans only change when the context content changes.

data "buildkit_context" "app" {
  path = "${path.module}/app"
}

output "context_digest" {
  value = data.buildkit_context.app.digest
}
