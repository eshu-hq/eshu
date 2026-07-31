terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  # Deliberately bare: no explicit `path` attribute. Terraform's own
  # documented default applies -- "terraform.tfstate" relative to the root
  # module directory (https://developer.hashicorp.com/terraform/language/backend/local)
  # -- and issue #5594's fix (go/internal/collector/terraformstate/backend_config_local.go)
  # applies that same default when resolving config-vs-state drift ownership
  # for a `backend "local" {}` block that omits `path`. This file sits at
  # this repo's ROOT, so the resolved locator is exactly
  # filepath.Join(<this repo's checkout root>, "terraform.tfstate") -- no
  # module-relative directory component.
  #
  # This fixture exists because, before issue #5594's golden-corpus follow-up,
  # the corpus had exactly ONE backend fixture (terraform_comprehensive's
  # `backend "s3" {}`, deliberately aligned with
  # testdata/cassettes/terraformstate/supply-chain-demo.json by issue #5442)
  # and ZERO `backend "local" {}` fixtures -- so the default-path fix itself
  # had no live golden-corpus coverage. The state side of this fixture's
  # backend lives in the SAME cassette file, under a scope whose scope_id is
  # the $LOCAL_BACKEND_SCOPE_ID$ sentinel: a BackendLocal locator is an
  # absolute path rooted at this repo's real, run-time git-checkout location
  # (scripts/verify-golden-corpus-gate.sh stages every fixture inside a fresh
  # `mktemp -d` per run), so neither the cassette nor this snapshot can pin
  # the resolved scope_id as a literal. scripts/verify-golden-corpus-gate.sh
  # resolves it after bootstrap-index commits this repo's `repository` fact,
  # via `golden-corpus-gate -print-local-backend-scope-id` (see
  # go/cmd/golden-corpus-gate/local_backend_scope_id.go), and substitutes it
  # into both the cassette and the snapshot's query shapes before the query
  # phase runs.
  #
  # None of this fixture's resource addresses overlap the cassette's
  # terraform_local_backend_demo state resources by design, so every resource
  # on both sides drifts (added_in_config / added_in_state), proving a real,
  # non-vacuous "exact" outcome finding materializes for this scope --
  # distinct from terraform_comprehensive's S3-backed scope, and distinct
  # from the deliberately-unresolvable orphaned-backend scope in the same
  # cassette (issue #5594) that proves the "unresolved" outcome instead.
  backend "local" {}
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_instance" "local_backend_demo" {
  ami           = "ami-0localbackend000001"
  instance_type = "t3.micro"

  tags = {
    Name = "local-backend-demo"
  }
}

resource "aws_s3_bucket" "local_backend_demo" {
  bucket = "local-backend-demo-bucket"

  tags = {
    Name = "local-backend-demo-bucket"
  }
}
