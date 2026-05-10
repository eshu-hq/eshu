package hcl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestTerraformParseResourceMetadata(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "main.tf", `resource "aws_s3_bucket" "logs" {
  count = 2
}

resource "aws_iam_user" "writer" {
  for_each = { alice = "reader" }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	resources := bucketForTest(t, got, "terraform_resources")
	if len(resources) != 2 {
		t.Fatalf("len(terraform_resources) = %d, want 2", len(resources))
	}

	bucket := namedItemForTest(t, resources, "aws_s3_bucket.logs")
	if got, want := bucket["count"], "2"; got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := bucket["provider"], "aws"; got != want {
		t.Fatalf("provider = %#v, want %#v", got, want)
	}
	if got, want := bucket["resource_service"], "s3"; got != want {
		t.Fatalf("resource_service = %#v, want %#v", got, want)
	}

	user := namedItemForTest(t, resources, "aws_iam_user.writer")
	if got, want := user["for_each"], `{ alice = "reader" }`; got != want {
		t.Fatalf("for_each = %#v, want %#v", got, want)
	}
}

func TestTerraformParseS3BackendDynamoDBTableMetadata(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "backend.tf", `terraform {
  backend "s3" {
    bucket         = "app-tfstate-prod"
    key            = "services/api/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "tfstate-locks-api"
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	backends := bucketForTest(t, got, "terraform_backends")
	if got, want := len(backends), 1; got != want {
		t.Fatalf("len(terraform_backends) = %d, want %d", got, want)
	}
	backend := backends[0]
	if got, want := backend["dynamodb_table"], "tfstate-locks-api"; got != want {
		t.Fatalf("dynamodb_table = %#v, want %#v", got, want)
	}
	if got, want := backend["dynamodb_table_is_literal"], true; got != want {
		t.Fatalf("dynamodb_table_is_literal = %#v, want %#v", got, want)
	}
}

func TestTerragruntParseHelperPaths(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "terragrunt.hcl", `terraform {
  source = "../modules/app"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  env = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  runtime = yamldecode(file("${get_repo_root()}/config/runtime.yaml"))
  rendered = templatefile("${path.module}/templates/runtime.json", {})
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	configs := bucketForTest(t, got, "terragrunt_configs")
	if len(configs) != 1 {
		t.Fatalf("len(terragrunt_configs) = %d, want 1", len(configs))
	}
	config := configs[0]
	if got, want := config["terraform_source"], "../modules/app"; got != want {
		t.Fatalf("terraform_source = %#v, want %#v", got, want)
	}
	if got, want := config["include_paths"], "root.hcl"; got != want {
		t.Fatalf("include_paths = %#v, want %#v", got, want)
	}
	if got, want := config["read_config_paths"], "env.hcl"; got != want {
		t.Fatalf("read_config_paths = %#v, want %#v", got, want)
	}
	if got, want := config["local_config_asset_paths"], "config/runtime.yaml,templates/runtime.json"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func writeHCLTestFile(t *testing.T, name string, body string) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", filePath, err)
	}
	return filePath
}

func bucketForTest(t *testing.T, payload map[string]any, key string) []map[string]any {
	t.Helper()
	rows, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	return rows
}

func namedItemForTest(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("missing row named %q in %#v", name, rows)
	return nil
}
