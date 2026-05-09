package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hclparser "github.com/eshu-hq/eshu/go/internal/parser/hcl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestParseTerragruntTerraformSourceMaterializesModuleSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`terraform {
  source = "../modules/app"
}

include "root" {
  path = find_in_parent_folders()
}
`)

	payload := parseTerragruntPayloadForTest(t, filePath, source)
	config := firstTerragruntConfigForTest(t, payload)
	if config["terraform_source"] != "../modules/app" {
		t.Fatalf("terraform_source = %#v, want %#v", config["terraform_source"], "../modules/app")
	}

	rows, ok := payload["terraform_modules"].([]map[string]any)
	if !ok {
		t.Fatalf("terraform_modules = %T, want []map[string]any", payload["terraform_modules"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0]["name"] != "terragrunt" {
		t.Fatalf("name = %#v, want %#v", rows[0]["name"], "terragrunt")
	}
	if rows[0]["source"] != "../modules/app" {
		t.Fatalf("source = %#v, want %#v", rows[0]["source"], "../modules/app")
	}
}

func TestParseTerragruntConfigExtractsHelperPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`terraform {
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

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["include_paths"], "root.hcl"; got != want {
		t.Fatalf("include_paths = %#v, want %#v", got, want)
	}
	if got, want := config["read_config_paths"], "env.hcl"; got != want {
		t.Fatalf("read_config_paths = %#v, want %#v", got, want)
	}
	if got, want := config["find_in_parent_folders_paths"], "env.hcl,root.hcl"; got != want {
		t.Fatalf("find_in_parent_folders_paths = %#v, want %#v", got, want)
	}
	if got, want := config["local_config_asset_paths"], "config/runtime.yaml,templates/runtime.json"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigExtractsJoinedHelperPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  global_vars = try(
    yamldecode(file(join("/", [path_relative_to_include(), "global.yaml"]))),
    {}
  )
  rendered = templatefile(join("/", [get_terragrunt_dir(), "templates/runtime.json"]), {})
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "global.yaml,templates/runtime.json"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigExtractsParentDirJoinedHelperPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  parent_runtime = templatefile(join("/", [get_parent_terragrunt_dir(), "templates/runtime.json"]), {})
  parent_global  = file(join("/", [get_parent_terragrunt_dir(), "global.yaml"]))
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "global.yaml,templates/runtime.json"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigExtractsServiceLevelAssetsFromNamedPathRelativeToInclude(t *testing.T) {
	filePath := filepath.FromSlash("accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl")
	source := []byte(`include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  path_parts   = split("/", path_relative_to_include("root"))
  account_name = local.path_parts[1]
  region_name  = local.path_parts[2]
  vpc_name     = local.path_parts[3]

  account_vars = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/account.yaml"))
  region_vars  = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/${local.region_name}/region.yaml"))
  vpc_vars     = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/${local.region_name}/${local.vpc_name}/vpc.yaml"))
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "accounts/bg-dev/account.yaml,accounts/bg-dev/us-east-1/dev.network-us-east-1/vpc.yaml,accounts/bg-dev/us-east-1/region.yaml"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigExtractsServiceLevelAssetsFromUnnamedPathRelativeToInclude(t *testing.T) {
	filePath := filepath.FromSlash("accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl")
	source := []byte(`include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  path_parts   = split("/", path_relative_to_include())
  account_name = local.path_parts[1]
  region_name  = local.path_parts[2]
  vpc_name     = local.path_parts[3]

  account_vars = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/account.yaml"))
  region_vars  = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/${local.region_name}/region.yaml"))
  vpc_vars     = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/${local.region_name}/${local.vpc_name}/vpc.yaml"))
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "accounts/bg-dev/account.yaml,accounts/bg-dev/us-east-1/dev.network-us-east-1/vpc.yaml,accounts/bg-dev/us-east-1/region.yaml"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func parseTerragruntConfigForTest(t *testing.T, filePath string, source []byte) map[string]any {
	t.Helper()
	return firstTerragruntConfigForTest(t, parseTerragruntPayloadForTest(t, filePath, source))
}

func parseTerragruntPayloadForTest(t *testing.T, filePath string, source []byte) map[string]any {
	t.Helper()
	cleanupRelativeTestPath(t, filePath)
	writeTestFile(t, filePath, string(source))
	payload, err := hclparser.Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("hcl.Parse() error = %v, want nil", err)
	}
	return payload
}

func cleanupRelativeTestPath(t *testing.T, filePath string) {
	t.Helper()
	if filepath.IsAbs(filePath) {
		return
	}
	cleanPath := filepath.Clean(filePath)
	cleanupRoot, _, _ := strings.Cut(cleanPath, string(filepath.Separator))
	t.Cleanup(func() {
		if err := os.RemoveAll(cleanupRoot); err != nil {
			t.Fatalf("os.RemoveAll(%q) error = %v, want nil", cleanupRoot, err)
		}
	})
}

func firstTerragruntConfigForTest(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	rows, ok := payload["terragrunt_configs"].([]map[string]any)
	if !ok {
		t.Fatalf("terragrunt_configs = %T, want []map[string]any", payload["terragrunt_configs"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(terragrunt_configs) = %d, want 1", len(rows))
	}
	return rows[0]
}
