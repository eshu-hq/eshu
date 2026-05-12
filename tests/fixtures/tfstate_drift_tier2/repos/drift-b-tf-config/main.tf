# Bucket B: drift_added_in_config — config declares aws_s3_bucket.declared
# but the state at s3://eshu-drift-b/prod/terraform.tfstate is empty of
# resources. The drift handler should emit drift_kind="added_in_config".

terraform {
  backend "s3" {
    bucket = "eshu-drift-b"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}

resource "aws_s3_bucket" "declared" {
  acl = "private"
}
