package hcl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestTerragruntParseRemoteStateLiteralS3Backend(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "terragrunt.hcl", `remote_state {
  backend = "s3"
  config = {
    bucket               = "app-tfstate-prod"
    key                  = "services/api/terraform.tfstate"
    region               = "us-east-1"
    dynamodb_table       = "tfstate-locks-api"
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "terragrunt_remote_states")
	if len(rows) != 1 {
		t.Fatalf("len(terragrunt_remote_states) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row["backend_kind"], "s3"; got != want {
		t.Fatalf("backend_kind = %#v, want %#v", got, want)
	}
	if got, want := row["bucket"], "app-tfstate-prod"; got != want {
		t.Fatalf("bucket = %#v, want %#v", got, want)
	}
	if got, want := row["bucket_is_literal"], true; got != want {
		t.Fatalf("bucket_is_literal = %#v, want %#v", got, want)
	}
	if got, want := row["key"], "services/api/terraform.tfstate"; got != want {
		t.Fatalf("key = %#v, want %#v", got, want)
	}
	if got, want := row["region"], "us-east-1"; got != want {
		t.Fatalf("region = %#v, want %#v", got, want)
	}
	if got, want := row["dynamodb_table"], "tfstate-locks-api"; got != want {
		t.Fatalf("dynamodb_table = %#v, want %#v", got, want)
	}
	if got, want := row["resolved_from"], "self"; got != want {
		t.Fatalf("resolved_from = %#v, want %#v", got, want)
	}
}

func TestTerragruntParseRemoteStateLiteralLocalBackend(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "terragrunt.hcl", `remote_state {
  backend = "local"
  config = {
    path = "terraform.tfstate"
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "terragrunt_remote_states")
	if len(rows) != 1 {
		t.Fatalf("len(terragrunt_remote_states) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row["backend_kind"], "local"; got != want {
		t.Fatalf("backend_kind = %#v, want %#v", got, want)
	}
	if got, want := row["path"], "terraform.tfstate"; got != want {
		t.Fatalf("path = %#v, want %#v", got, want)
	}
	if got, want := row["path_is_literal"], true; got != want {
		t.Fatalf("path_is_literal = %#v, want %#v", got, want)
	}
}

func TestTerragruntParseRemoteStateMarksDynamicAttributesNonLiteral(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "terragrunt.hcl", `remote_state {
  backend = "s3"
  config = {
    bucket = "app-${local.env}-tfstate"
    key    = "services/${path_relative_to_include()}/terraform.tfstate"
    region = "us-east-1"
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "terragrunt_remote_states")
	if len(rows) != 1 {
		t.Fatalf("len(terragrunt_remote_states) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row["bucket_is_literal"], false; got != want {
		t.Fatalf("bucket_is_literal = %#v, want %#v", got, want)
	}
	if got, want := row["key_is_literal"], false; got != want {
		t.Fatalf("key_is_literal = %#v, want %#v", got, want)
	}
	if got, want := row["region_is_literal"], true; got != want {
		t.Fatalf("region_is_literal = %#v, want %#v", got, want)
	}
}

func TestTerragruntParseRemoteStateNoBlockYieldsNoRows(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "terragrunt.hcl", `terraform {
  source = "../modules/app"
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "terragrunt_remote_states")
	if len(rows) != 0 {
		t.Fatalf("len(terragrunt_remote_states) = %d, want 0", len(rows))
	}
}

func TestTerragruntParseRemoteStateResolvesFromIncludeChain(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	parentDir := filepath.Join(root, "live")
	childDir := filepath.Join(parentDir, "prod", "api")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", childDir, err)
	}

	parentPath := filepath.Join(parentDir, "terragrunt.hcl")
	if err := os.WriteFile(parentPath, []byte(`remote_state {
  backend = "s3"
  config = {
    bucket = "app-tfstate-prod"
    key    = "services/api/terraform.tfstate"
    region = "us-east-1"
  }
}
`), 0o644); err != nil {
		t.Fatalf("write parent terragrunt.hcl error = %v", err)
	}

	childPath := filepath.Join(childDir, "terragrunt.hcl")
	if err := os.WriteFile(childPath, []byte(`include "root" {
  path = find_in_parent_folders("terragrunt.hcl")
}
`), 0o644); err != nil {
		t.Fatalf("write child terragrunt.hcl error = %v", err)
	}

	got, err := Parse(childPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "terragrunt_remote_states")
	if len(rows) != 1 {
		t.Fatalf("len(terragrunt_remote_states) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row["backend_kind"], "s3"; got != want {
		t.Fatalf("backend_kind = %#v, want %#v", got, want)
	}
	if got, want := row["bucket"], "app-tfstate-prod"; got != want {
		t.Fatalf("bucket = %#v, want %#v", got, want)
	}
	if got, want := row["resolved_from"], "include_chain"; got != want {
		t.Fatalf("resolved_from = %#v, want %#v", got, want)
	}
	if got, want := row["resolved_source"], parentPath; got != want {
		t.Fatalf("resolved_source = %#v, want %#v", got, want)
	}
}

func TestTerragruntParseRemoteStateMultipleBlocks(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "terragrunt.hcl", `remote_state {
  backend = "s3"
  config = {
    bucket = "first"
    key    = "first.tfstate"
    region = "us-east-1"
  }
}

remote_state {
  backend = "local"
  config = {
    path = "second.tfstate"
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "terragrunt_remote_states")
	if len(rows) != 2 {
		t.Fatalf("len(terragrunt_remote_states) = %d, want 2", len(rows))
	}
}
