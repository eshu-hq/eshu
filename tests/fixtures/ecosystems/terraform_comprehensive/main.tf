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
  # (this fixture's ~12 declared resources vs. the cassette's 2 ECS state
  # resources; none of the addresses overlap, so both added_in_config and
  # added_in_state fire).
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
