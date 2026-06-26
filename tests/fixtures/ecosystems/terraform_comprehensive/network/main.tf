terraform {
  required_version = ">= 1.5.0"
}

# Minimal network stack so the Atlantis `network` project's dir resolves to a
# real Directory node (the MANAGES edge target).
resource "null_resource" "network_placeholder" {}
