package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathHCLTerraformBlockMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.tf")
	writeTestFile(
		t,
		filePath,
		`terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "terraform_blocks", "terraform")
	assertBucketContainsFieldValue(t, got, "terraform_blocks", "required_providers", "aws")
	assertBucketContainsFieldValue(t, got, "terraform_blocks", "required_provider_sources", "aws=hashicorp/aws")

	blocks, ok := got["terraform_blocks"].([]map[string]any)
	if !ok {
		t.Fatalf("terraform_blocks = %T, want []map[string]any", got["terraform_blocks"])
	}
	if len(blocks) != 1 {
		t.Fatalf("len(terraform_blocks) = %d, want 1", len(blocks))
	}
	if got, want := blocks[0]["required_provider_count"], 1; got != want {
		t.Fatalf("terraform_blocks[0].required_provider_count = %#v, want %#v", got, want)
	}
}

func TestDefaultEngineParsePathHCLTerraformBackendMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "backend.tf")
	writeTestFile(
		t,
		filePath,
		`terraform {
  backend "s3" {
    bucket = "app-tfstate-prod"
    key    = "services/api/terraform.tfstate"
    region = "us-east-1"
    secret_key = "should-not-be-indexed"
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	backend := findNamedBucketItem(t, got, "terraform_backends", "s3")
	if got, want := backend["backend_kind"], "s3"; got != want {
		t.Fatalf("terraform_backends[0].backend_kind = %#v, want %#v", got, want)
	}
	if got, want := backend["bucket"], "app-tfstate-prod"; got != want {
		t.Fatalf("terraform_backends[0].bucket = %#v, want %#v", got, want)
	}
	if got, want := backend["key"], "services/api/terraform.tfstate"; got != want {
		t.Fatalf("terraform_backends[0].key = %#v, want %#v", got, want)
	}
	if got, want := backend["region"], "us-east-1"; got != want {
		t.Fatalf("terraform_backends[0].region = %#v, want %#v", got, want)
	}
	if got, want := backend["key_is_literal"], true; got != want {
		t.Fatalf("terraform_backends[0].key_is_literal = %#v, want %#v", got, want)
	}
	if _, ok := backend["secret_key"]; ok {
		t.Fatalf("terraform_backends[0].secret_key = %#v, want omitted", backend["secret_key"])
	}
	if _, ok := backend["secret_key_is_literal"]; ok {
		t.Fatalf("terraform_backends[0].secret_key_is_literal = %#v, want omitted", backend["secret_key_is_literal"])
	}
}

func TestDefaultEngineParsePathHCLTerraformBackendMarksDynamicMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "backend.tf")
	writeTestFile(
		t,
		filePath,
		`terraform {
  backend "s3" {
    bucket = var.state_bucket
    key    = "services/${terraform.workspace}/terraform.tfstate"
    region = "us-east-1"
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	backend := findNamedBucketItem(t, got, "terraform_backends", "s3")
	if got, want := backend["bucket_is_literal"], false; got != want {
		t.Fatalf("terraform_backends[0].bucket_is_literal = %#v, want %#v", got, want)
	}
	if got, want := backend["key_is_literal"], false; got != want {
		t.Fatalf("terraform_backends[0].key_is_literal = %#v, want %#v", got, want)
	}
	if got, want := backend["region_is_literal"], true; got != want {
		t.Fatalf("terraform_backends[0].region_is_literal = %#v, want %#v", got, want)
	}
}

func TestDefaultEngineParsePathHCLTerraformResourceMultiplicityMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.tf")
	writeTestFile(
		t,
		filePath,
		`resource "aws_s3_bucket" "logs" {
  count = 2
}

resource "aws_iam_user" "writer" {
  for_each = { alice = "reader" }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	resources, ok := got["terraform_resources"].([]map[string]any)
	if !ok {
		t.Fatalf("terraform_resources = %T, want []map[string]any", got["terraform_resources"])
	}
	if len(resources) != 2 {
		t.Fatalf("len(terraform_resources) = %d, want 2", len(resources))
	}

	bucket := findNamedBucketItem(t, got, "terraform_resources", "aws_s3_bucket.logs")
	if got, want := bucket["count"], "2"; got != want {
		t.Fatalf("terraform_resources[aws_s3_bucket.logs].count = %#v, want %#v", got, want)
	}
	if got, want := bucket["provider"], "aws"; got != want {
		t.Fatalf("terraform_resources[aws_s3_bucket.logs].provider = %#v, want %#v", got, want)
	}
	if got, want := bucket["resource_service"], "s3"; got != want {
		t.Fatalf("terraform_resources[aws_s3_bucket.logs].resource_service = %#v, want %#v", got, want)
	}
	if got, want := bucket["resource_category"], "storage"; got != want {
		t.Fatalf("terraform_resources[aws_s3_bucket.logs].resource_category = %#v, want %#v", got, want)
	}

	user := findNamedBucketItem(t, got, "terraform_resources", "aws_iam_user.writer")
	if got, want := user["for_each"], `{ alice = "reader" }`; got != want {
		t.Fatalf("terraform_resources[aws_iam_user.writer].for_each = %#v, want %#v", got, want)
	}
	if got, want := user["resource_category"], "security"; got != want {
		t.Fatalf("terraform_resources[aws_iam_user.writer].resource_category = %#v, want %#v", got, want)
	}
}
