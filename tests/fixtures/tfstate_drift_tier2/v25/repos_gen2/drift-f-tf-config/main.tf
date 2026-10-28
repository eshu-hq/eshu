# Bucket F gen-2: same backend, resource removed from config. The Tier-2 v2.5
# verifier runs collector-instance-2 against this gen-2 tree after instance-1
# already collected the gen-1 tree, so Postgres has both an active gen-2
# scope_generations row (without the resource) and a superseded gen-1 row
# (with the resource) on the same repo scope_id.

terraform {
  backend "s3" {
    bucket = "eshu-drift-f"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}
