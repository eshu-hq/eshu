# Bucket C gen-2: identical config to gen-1. Only the state file rotates to
# serial=2 with the resource removed; see ../../state_gen2/drift-c.tfstate.

terraform {
  backend "s3" {
    bucket = "eshu-drift-c"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}

resource "aws_s3_bucket" "cached" {
  acl = "private"
}
