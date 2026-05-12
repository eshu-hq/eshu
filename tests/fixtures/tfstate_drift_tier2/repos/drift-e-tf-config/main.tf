# Bucket E: attribute_drift — both config and state declare aws_s3_bucket.logs.
# Config carries SSE algorithm AES256; the state at
# s3://eshu-drift-e/prod/terraform.tfstate carries the same key wrapped in the
# Terraform-state nested-singleton-array shape resolving to aws:kms. The acl
# value matches on both sides (private) so only the SSE path drifts.

terraform {
  backend "s3" {
    bucket = "eshu-drift-e"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}

resource "aws_s3_bucket" "logs" {
  acl = "private"

  server_side_encryption_configuration {
    rule {
      apply_server_side_encryption_by_default {
        sse_algorithm = "AES256"
      }
    }
  }
}
