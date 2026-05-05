package query

import "testing"

func TestBuildRepositoryConfigArtifactsExtractsSlashJoinedPathModuleTemplateAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-stack-aws-client-vpn", []FileContent{
		{
			RelativePath: "modules/2024.02/custom/ecs-application/pipeline_node/main.tf",
			Content: `locals {
  appspec_yaml_file = templatefile(join("/", [path.module, "specs/AppSpec.yaml"]), {
    app_name = var.app_name
  })
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 1 {
		t.Fatalf("len(config_paths) = %d, want 1", len(configPaths))
	}
	if got, want := configPaths[0]["path"], "specs/AppSpec.yaml"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsSlashJoinedRepoRootAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terragrunt-deployment", []FileContent{
		{
			RelativePath: "accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl",
			Content: `locals {
  account_name = "bg-dev"
  account_vars = yamldecode(file(join("/", [get_repo_root(), "accounts/${local.account_name}/account.yaml"])))
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 1 {
		t.Fatalf("len(config_paths) = %d, want 1", len(configPaths))
	}
	if got, want := configPaths[0]["path"], "accounts/bg-dev/account.yaml"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsNestedHelperBackedLocalJoinedAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-stack-aws-client-vpn", []FileContent{
		{
			RelativePath: "modules/2024.02/custom/ecs-application/pipeline_node/main.tf",
			Content: `locals {
  templates_dir = "${path.module}/templates"
  template_name  = "runtime.json"
  rendered      = templatefile(join("/", [local.templates_dir, local.template_name]), {})
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 1 {
		t.Fatalf("len(config_paths) = %d, want 1", len(configPaths))
	}
	if got, want := configPaths[0]["path"], "templates/runtime.json"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsNormalizesHelperBuiltTerraformSourceAndDependencyConfigPath(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-stack-payments", []FileContent{
		{
			RelativePath: "env/prod/terragrunt.hcl",
			Content: `terraform {
  source = join("/", [get_repo_root(), "modules/service"])
}

dependency "network" {
  config_path = join("/", [get_repo_root(), "network/root.hcl"])
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 2 {
		t.Fatalf("len(config_paths) = %d, want 2", len(configPaths))
	}

	wantKinds := map[string]string{
		"modules/service":  "terraform_module_source_path",
		"network/root.hcl": "terragrunt_dependency_config_path",
	}
	for _, row := range configPaths {
		path, _ := row["path"].(string)
		wantKind, ok := wantKinds[path]
		if !ok {
			t.Fatalf("unexpected config_paths row = %#v", row)
		}
		if got, want := row["source_repo"], "terraform-stack-payments"; got != want {
			t.Fatalf("config_paths[%q].source_repo = %#v, want %#v", path, got, want)
		}
		if got, want := row["relative_path"], "env/prod/terragrunt.hcl"; got != want {
			t.Fatalf("config_paths[%q].relative_path = %#v, want %#v", path, got, want)
		}
		if got := row["evidence_kind"]; got != wantKind {
			t.Fatalf("config_paths[%q].evidence_kind = %#v, want %#v", path, got, wantKind)
		}
		delete(wantKinds, path)
	}
	if len(wantKinds) != 0 {
		t.Fatalf("missing config_paths rows for %#v", wantKinds)
	}
}
