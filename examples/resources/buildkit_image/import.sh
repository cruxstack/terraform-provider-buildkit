# buildkit_image is imported by its fully-qualified digest reference
# (registry/repo@sha256:<64-hex>). image_digest and a published entry are seeded
# from the reference; context/dockerfile/platforms are reconciled from config on
# the next plan.
terraform import buildkit_image.app ghcr.io/org/app@sha256:0000000000000000000000000000000000000000000000000000000000000000
