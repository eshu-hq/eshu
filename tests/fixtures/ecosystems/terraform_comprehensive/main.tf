terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # bucket/key intentionally match testdata/cassettes/terraformstate/
  # supply-chain-demo.json's backend locator (issue #5442) so
  # tfstatebackend.ResolveConfigCommitForBackend resolves this repo as the
  # sole owner of that state snapshot in the golden corpus, letting the
  # terraform_config_state_drift domain materialize real drift findings
  # (this fixture's declared resources vs. the cassette's ECS/EC2 state
  # resources; all but one address pair are deliberately non-overlapping, so
  # both added_in_config and added_in_state still fire -- see aws_instance
  # "supply-chain-demo" below for the ONE deliberate overlap).
  backend "s3" {
    bucket = "supply-chain-demo-tfstate"
    key    = "supply-chain-demo/terraform.tfstate"
    region = "us-east-1"
  }
}

provider "aws" {
  region = var.aws_region
}

resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = var.instance_type

  tags = {
    Name        = "web-server"
    Environment = var.environment
  }
}

# aws_instance.supply-chain-demo is the ONE deliberate address overlap with
# testdata/cassettes/terraformstate/supply-chain-demo.json's
# aws_instance.supply-chain-demo state resource (issue #5453): both declare
# ami-0123456789abcdef0 so this pair converges cleanly in the
# terraform_config_state_drift (config-vs-state) domain -- it exists to give
# cloudruntime's AWS/multi-cloud runtime-drift domain (cloud-vs-state) a
# resolvable Terraform CONFIG owner for that state resource's address, which
# Classify requires before it will ever compare AMI values. Do not let this
# ami literal drift from the state cassette's declared ami without also
# checking testdata/golden/e2e-20repo-snapshot.json's
# list_terraform_config_state_drift_findings assertions for a new,
# unaccounted-for attribute_drift finding.
resource "aws_instance" "supply-chain-demo" {
  ami           = "ami-0123456789abcdef0"
  instance_type = "t3.micro"
}

resource "aws_s3_bucket" "data" {
  bucket = var.bucket_name

  tags = {
    Name = "data-bucket"
  }
}

resource "aws_s3_bucket_versioning" "data" {
  bucket = aws_s3_bucket.data.id

  versioning_configuration {
    status = "Enabled"
  }
}

data "aws_caller_identity" "current" {}

data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    principals {
      type        = "Federated"
      identifiers = ["arn:aws:iam::oidc-provider/oidc.eks.us-east-1.amazonaws.com"]
    }
  }
}
