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
  # resources; all but two address pairs are deliberately non-overlapping, so
  # both added_in_config and added_in_state still fire -- see aws_instance
  # "supply-chain-demo" and aws_lambda_function "supply-chain-demo-partial"
  # below for the two deliberate overlaps).
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

# aws_lambda_function.supply-chain-demo-partial is the SECOND deliberate address
# overlap, and it exists for one reason: to give the golden corpus a
# PARTIALLY comparable runtime-drift pair (issue #5861).
#
# aws_instance above is covered for exactly one comparable value (ami), so it
# can only ever produce a fully readable comparison or none at all. Lambda is
# covered for two -- image_uri and version -- which is the only shape where a
# pass can prove drift on one attribute while being unable to READ another. That
# shape is what #5861 changed, and without a Lambda pair here the B-7 gate has
# nothing to assert it against: the finding would be exercised only by unit
# tests, and a regression in replay, payload writing, or HTTP/MCP readback of
# the coverage gap would ship green.
#
# The three sides are deliberately mismatched, each in a specific way:
#   - here (config): present, so Classify has a resolvable owner and reaches the
#     value comparison at all.
#   - state cassette: image_uri present, version "9".
#   - awscloud cassette: package_type "Image" with NO image_uri (the
#     unobservable side), version "7".
# That yields Degraded=[image_uri] and Drifted=[version], so the pair reports
# image_version_drift AND carries missing_evidence
# comparable_attribute:image_uri. Changing any of the three without the others
# silently converts this into an ordinary drift or an inconclusive row and
# stops proving what it was added to prove.
resource "aws_lambda_function" "supply-chain-demo-partial" {
  function_name = "supply-chain-demo-partial"
  package_type  = "Image"
  image_uri     = "123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:v9"
  role          = "arn:aws:iam::123456789012:role/supply-chain-demo-partial"
}
