package parser

import (
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
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

	file, diags := hclparse.NewParser().ParseHCL(source, filePath)
	if diags.HasErrors() {
		t.Fatalf("ParseHCL() diagnostics = %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("file.Body = %T, want *hclsyntax.Body", file.Body)
	}

	config := parseTerragruntConfig(body, source, filePath)
	if config["terraform_source"] != "../modules/app" {
		t.Fatalf("terraform_source = %#v, want %#v", config["terraform_source"], "../modules/app")
	}

	rows := parseTerragruntModuleSources(body, source, filePath)
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

	file, diags := hclparse.NewParser().ParseHCL(source, filePath)
	if diags.HasErrors() {
		t.Fatalf("ParseHCL() diagnostics = %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("file.Body = %T, want *hclsyntax.Body", file.Body)
	}

	config := parseTerragruntConfig(body, source, filePath)

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

	file, diags := hclparse.NewParser().ParseHCL(source, filePath)
	if diags.HasErrors() {
		t.Fatalf("ParseHCL() diagnostics = %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("file.Body = %T, want *hclsyntax.Body", file.Body)
	}

	config := parseTerragruntConfig(body, source, filePath)

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

	file, diags := hclparse.NewParser().ParseHCL(source, filePath)
	if diags.HasErrors() {
		t.Fatalf("ParseHCL() diagnostics = %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("file.Body = %T, want *hclsyntax.Body", file.Body)
	}

	config := parseTerragruntConfig(body, source, filePath)

	if got, want := config["local_config_asset_paths"], "global.yaml,templates/runtime.json"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigExtractsServiceLevelAssetsFromNamedPathRelativeToInclude(t *testing.T) {
	t.Parallel()

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

	file, diags := hclparse.NewParser().ParseHCL(source, filePath)
	if diags.HasErrors() {
		t.Fatalf("ParseHCL() diagnostics = %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("file.Body = %T, want *hclsyntax.Body", file.Body)
	}

	config := parseTerragruntConfig(body, source, filePath)

	if got, want := config["local_config_asset_paths"], "accounts/bg-dev/account.yaml,accounts/bg-dev/us-east-1/dev.network-us-east-1/vpc.yaml,accounts/bg-dev/us-east-1/region.yaml"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigExtractsServiceLevelAssetsFromUnnamedPathRelativeToInclude(t *testing.T) {
	t.Parallel()

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

	file, diags := hclparse.NewParser().ParseHCL(source, filePath)
	if diags.HasErrors() {
		t.Fatalf("ParseHCL() diagnostics = %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("file.Body = %T, want *hclsyntax.Body", file.Body)
	}

	config := parseTerragruntConfig(body, source, filePath)

	if got, want := config["local_config_asset_paths"], "accounts/bg-dev/account.yaml,accounts/bg-dev/us-east-1/dev.network-us-east-1/vpc.yaml,accounts/bg-dev/us-east-1/region.yaml"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}
