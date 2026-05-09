package parser

import (
	"path/filepath"
	"testing"
)

func TestParseTerragruntConfigExtractsRepoRootAndPathModuleJoinedHelperPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  runtime = yamldecode(file(join("/", [get_repo_root(), "config/runtime.yaml"])))
  rendered = templatefile(join("/", [path.module, "templates/runtime.json"]), {})
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "config/runtime.yaml,templates/runtime.json"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigResolvesLocalInterpolationsInJoinedHelperPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  account_name = "bg-dev"
  account_vars = yamldecode(file(join("/", [get_repo_root(), "accounts/${local.account_name}/account.yaml"])))
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "accounts/bg-dev/account.yaml"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigResolvesNestedLocalBackedHelperPaths(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  container_template = var.platform_capabilities[0] == "FARGATE" ? "container-fargate.tpl" : "container-ec2.tpl"

  container_properties = templatefile(lookup(var.configuration, "template", "${path.module}/batch/${local.container_template}"), {
    name = var.name
  })
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "batch/container-fargate.tpl"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigResolvesLookupBackedTemplateLocalsWithTrimspaceWrapper(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  userdata_template = lookup(
    var.configuration,
    "userdata_template",
    "${path.module}/templates/user_data.tpl",
  )
  dashboard_template = lookup(
    var.configuration,
    "dashboard_template",
    "${path.module}/templates/cloudwatch-dashboard.tpl",
  )

  userdata = trimspace(templatefile(local.userdata_template, {
    userdata = var.user_data
  }))
  cloudwatch_dashboard = templatefile(local.dashboard_template, {
    name = var.name
  })
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "templates/cloudwatch-dashboard.tpl,templates/user_data.tpl"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigResolvesLegacyInterpolationWrappedLookupAndFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  task_template = "${lookup(var.configuration, "template", "${path.module}/templates/ecs/container.tpl")}"
}

data "template_file" "task" {
  template = "${file(local.task_template)}"
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "templates/ecs/container.tpl"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigResolvesLegacyInterpolationWrappedLookupAndTemplatefile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`locals {
  template = "${lookup(var.configuration, "template_file", "${path.module}/templates/user_data.tpl")}"
}

resource "example_instance" "this" {
  user_data = templatefile(local.template, {
    userdata = var.user_data
  })
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "templates/user_data.tpl"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func TestParseTerragruntConfigResolvesDirectLookupWrappedByFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	source := []byte(`data "template_file" "job" {
  template = "${file(lookup(var.configuration, "template", "${path.module}/batch/container.tpl"))}"
}
`)

	config := parseTerragruntConfigForTest(t, filePath, source)

	if got, want := config["local_config_asset_paths"], "batch/container.tpl"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}
