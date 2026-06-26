terraform {
  required_version = ">= 1.5.0"
}

# Minimal staging stack so the Atlantis `staging` project's dir resolves to a
# real Directory node (the MANAGES edge target).
resource "null_resource" "staging_placeholder" {}
