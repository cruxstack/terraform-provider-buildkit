schema_version = 1

project {
  license        = "MPL-2.0"
  copyright_year = 2026
  copyright_holder = "Cruxstack"

  header_ignore = [
    # generated and vendored content
    "docs/**",
    "examples/**",
    ".github/**",
    # markdown / meta docs
    "*.md",
  ]
}
