terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# Minimal provider block for schema extraction only.
# No real credentials are needed — terraform providers schema -json
# reads the schema from the downloaded binary, not from AWS APIs.
provider "aws" {
  region                      = "us-east-1"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
}
