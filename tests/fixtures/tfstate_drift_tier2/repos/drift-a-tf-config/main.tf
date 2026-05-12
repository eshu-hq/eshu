# Bucket A: drift_added_in_state — config has the s3 backend block only;
# no resource block for aws_s3_bucket.unmanaged. The state file at
# s3://eshu-drift-a/prod/terraform.tfstate carries that resource, so the
# drift handler should emit drift_kind="added_in_state".

terraform {
  backend "s3" {
    bucket = "eshu-drift-a"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}
