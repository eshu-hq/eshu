// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertNamedBucketContains(t, got, "terraform_blocks", "terraform")
	parsertest.AssertBucketContainsFieldValue(t, got, "terraform_blocks", "required_providers", "aws")
	parsertest.AssertBucketContainsFieldValue(t, got, "terraform_blocks", "required_provider_sources", "aws=hashicorp/aws")

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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	backend := parsertest.AssertBucketItemByName(t, got, "terraform_backends", "s3")
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	backend := parsertest.AssertBucketItemByName(t, got, "terraform_backends", "s3")
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

func TestDefaultEngineParsePathHCLTerraformBackendAttributeLineNumbers(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "backend.tf")
	writeTestFile(
		t,
		filePath,
		`terraform {
  backend "s3" {
    bucket = var.state_bucket
    key    = "services/api/terraform.tfstate"
    region = "us-east-1"
    workspace_key_prefix = terraform.workspace
  }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	backend := parsertest.AssertBucketItemByName(t, got, "terraform_backends", "s3")
	cases := []struct {
		field string
		want  int
	}{
		{field: "bucket_line_number", want: 3},
		{field: "key_line_number", want: 4},
		{field: "region_line_number", want: 5},
		{field: "workspace_key_prefix_line_number", want: 6},
	}
	for _, tc := range cases {
		if got := backend[tc.field]; got != tc.want {
			t.Fatalf("terraform_backends[0].%s = %#v, want %#v", tc.field, got, tc.want)
		}
	}
}

// TestDefaultEngineParsePathHCLLocalBackendBareBlockOmitsPathAttribute is the
// parser-layer regression for issue #5594: a bare `backend "local" {}` block
// (no `path` attribute — the ordinary way to write a local backend, since
// Terraform itself defaults path to "terraform.tfstate") must not emit a
// state_path row field. The row's own "path" field (the source .tf file this
// backend block was parsed from) must stay untouched.
func TestDefaultEngineParsePathHCLLocalBackendBareBlockOmitsPathAttribute(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "env", "prod", "main.tf")
	writeTestFile(
		t,
		filePath,
		`terraform {
  backend "local" {}
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	backend := parsertest.AssertBucketItemByName(t, got, "terraform_backends", "local")
	if got, want := backend["backend_kind"], "local"; got != want {
		t.Fatalf("terraform_backends[0].backend_kind = %#v, want %#v", got, want)
	}
	if _, ok := backend["state_path"]; ok {
		t.Fatalf("terraform_backends[0].state_path = %#v, want omitted for a bare block", backend["state_path"])
	}
	if got, ok := backend["path"].(string); !ok || !strings.HasSuffix(got, filepath.Join("env", "prod", "main.tf")) {
		t.Fatalf("terraform_backends[0].path = %#v, want the source file path (unchanged by the local backend fix)", backend["path"])
	}
}

// TestDefaultEngineParsePathHCLLocalBackendCapturesPathAttributeSeparately
// proves an explicit `path` attribute on a local backend is captured under
// "state_path" (not "path", which already holds the source .tf file path for
// every backend row) so both values survive without one silently overwriting
// the other (issue #5594).
func TestDefaultEngineParsePathHCLLocalBackendCapturesPathAttributeSeparately(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "env", "prod", "main.tf")
	writeTestFile(
		t,
		filePath,
		`terraform {
  backend "local" {
    path = "custom/terraform.tfstate"
  }
}
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	backend := parsertest.AssertBucketItemByName(t, got, "terraform_backends", "local")
	if got, want := backend["state_path"], "custom/terraform.tfstate"; got != want {
		t.Fatalf("terraform_backends[0].state_path = %#v, want %#v", got, want)
	}
	if got, want := backend["state_path_is_literal"], true; got != want {
		t.Fatalf("terraform_backends[0].state_path_is_literal = %#v, want %#v", got, want)
	}
	if got, want := backend["state_path_line_number"], 3; got != want {
		t.Fatalf("terraform_backends[0].state_path_line_number = %#v, want %#v", got, want)
	}
	if got, ok := backend["path"].(string); !ok || !strings.HasSuffix(got, filepath.Join("env", "prod", "main.tf")) {
		t.Fatalf("terraform_backends[0].path = %#v, want the source file path, not overwritten by the "+
			"backend's own path attribute", backend["path"])
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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
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

	bucket := parsertest.AssertBucketItemByName(t, got, "terraform_resources", "aws_s3_bucket.logs")
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

	user := parsertest.AssertBucketItemByName(t, got, "terraform_resources", "aws_iam_user.writer")
	if got, want := user["for_each"], `{ alice = "reader" }`; got != want {
		t.Fatalf("terraform_resources[aws_iam_user.writer].for_each = %#v, want %#v", got, want)
	}
	if got, want := user["resource_category"], "security"; got != want {
		t.Fatalf("terraform_resources[aws_iam_user.writer].resource_category = %#v, want %#v", got, want)
	}
}
