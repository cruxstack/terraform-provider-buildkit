# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately rather than opening a public
issue. Use GitHub's private vulnerability reporting on this repository (Security
→ Report a vulnerability), or contact the maintainers directly.

We will acknowledge receipt, investigate, and coordinate a fix and disclosure
timeline with you.

## Handling of secrets

- `secrets`, `secrets_base64`, and `registry_auth` credentials are marked
  sensitive in the provider schema and are not written to logs.
- Build secrets are delivered to BuildKit over the session gRPC channel using
  the standard `--mount=type=secret` mechanism; they are not baked into image
  layers unless your Dockerfile copies them there.
- Prefer supplying registry credentials via `~/.docker/config.json` / credential
  helpers or environment-sourced Terraform variables rather than hardcoding them
  in configuration.
