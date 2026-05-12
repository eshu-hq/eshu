# Bucket D, repo "b": ambiguous-owner pair sibling of drift-d-tf-config-a.
# Both repos declare the same s3 backend at s3://eshu-drift-d/prod/terraform.tfstate
# so the resolver must reject as ambiguous. No counter delta expected.

terraform {
  backend "s3" {
    bucket = "eshu-drift-d"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}
