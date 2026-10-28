# Bucket D, repo "a": ambiguous-owner pair — this repo and drift-d-tf-config-b
# both declare the same s3 backend at s3://eshu-drift-d/prod/terraform.tfstate.
# The drift handler's resolver should return ErrAmbiguousBackendOwner and emit
# a WARN log with failure_class="ambiguous_backend_owner". No counter delta.

terraform {
  backend "s3" {
    bucket = "eshu-drift-d"
    key    = "prod/terraform.tfstate"
    region = "us-east-1"
  }
}
