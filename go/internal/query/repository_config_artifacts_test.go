package query

import (
	"strings"
	"testing"
)

func TestBuildRepositoryConfigArtifactsExtractsKustomizePolicyConfigPaths(t *testing.T) {
	t.Parallel()

	files := []FileContent{
		{
			RelativePath: "deploy/kustomization.yaml",
			Content: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - policy.yaml
`,
		},
		{
			RelativePath: "deploy/policy.yaml",
			Content: `apiVersion: iam.aws.upbound.io/v1beta1
kind: RolePolicy
spec:
  policyDocument:
    Statement:
      - Effect: Allow
        Action:
          - ssm:GetParameter
        Resource:
          - arn:aws:ssm:us-east-1:123456789012:parameter/configd/payments/*
          - /api/payments/runtime/*
`,
		},
		{
			RelativePath: "deploy/unreachable.yaml",
			Content: `apiVersion: iam.aws.upbound.io/v1beta1
kind: RolePolicy
spec:
  policyDocument:
    Statement:
      - Resource:
          - arn:aws:ssm:us-east-1:123456789012:parameter/configd/ignored/*
`,
		},
	}

	got := buildRepositoryConfigArtifacts("helm-charts", files)
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 2 {
		t.Fatalf("len(config_paths) = %d, want 2", len(configPaths))
	}

	if got, want := configPaths[0]["path"], "/api/payments/runtime/*"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["source_repo"], "helm-charts"; got != want {
		t.Fatalf("config_paths[0].source_repo = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["relative_path"], "deploy/policy.yaml"; got != want {
		t.Fatalf("config_paths[0].relative_path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "kustomize_policy_document_resource"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}

	if got, want := configPaths[1]["path"], "/configd/payments/*"; got != want {
		t.Fatalf("config_paths[1].path = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsTerragruntDependencyConfigPaths(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-stack-payments", []FileContent{
		{
			RelativePath: "env/prod/terragrunt.hcl",
			Content: `terraform {
  source = "../modules/service"
}

dependency "network" {
  config_path = "../network"
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
	if got, want := configPaths[0]["path"], "../modules/service"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["source_repo"], "terraform-stack-payments"; got != want {
		t.Fatalf("config_paths[0].source_repo = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "terraform_module_source_path"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
	if got, want := configPaths[1]["path"], "../network"; got != want {
		t.Fatalf("config_paths[1].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[1]["source_repo"], "terraform-stack-payments"; got != want {
		t.Fatalf("config_paths[1].source_repo = %#v, want %#v", got, want)
	}
	if got, want := configPaths[1]["evidence_kind"], "terragrunt_dependency_config_path"; got != want {
		t.Fatalf("config_paths[1].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsDockerComposeConfigLinks(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("payments-service", []FileContent{
		{
			RelativePath: "docker-compose.yaml",
			ArtifactType: "docker_compose",
			Content: `services:
  api:
    env_file:
      - .env
      - deploy/api.env
    configs:
      - source: app-config
        target: /etc/api/config.yaml
    secrets:
      - source: db-password
        target: db-password

configs:
  app-config:
    file: ./config/app.yaml

secrets:
  db-password:
    file: ./secrets/db-password.txt
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 4 {
		t.Fatalf("len(config_paths) = %d, want 4", len(configPaths))
	}

	wantKinds := map[string]string{
		".env":                    "docker_compose_env_file",
		"config/app.yaml":         "docker_compose_config_file",
		"deploy/api.env":          "docker_compose_env_file",
		"secrets/db-password.txt": "docker_compose_secret_file",
	}
	for _, row := range configPaths {
		path, _ := row["path"].(string)
		wantKind, ok := wantKinds[path]
		if !ok {
			t.Fatalf("unexpected config_paths row = %#v", row)
		}
		if got, want := row["source_repo"], "payments-service"; got != want {
			t.Fatalf("config_paths[%q].source_repo = %#v, want %#v", path, got, want)
		}
		if got, want := row["relative_path"], "docker-compose.yaml"; got != want {
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

func TestBuildRepositoryConfigArtifactsExtractsTerragruntAndTerraformConfigAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-stack-payments", []FileContent{
		{
			RelativePath: "env/prod/terragrunt.hcl",
			Content: `include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  env = read_terragrunt_config(find_in_parent_folders("env.hcl"))
}

inputs = yamldecode(file("${get_repo_root()}/config/runtime.yaml"))
`,
		},
		{
			RelativePath: "modules/build/main.tf",
			Content: `resource "aws_codebuild_project" "build" {
  buildspec = file("${path.module}/buildspec.yaml")
}

locals {
  rendered = templatefile("${path.module}/templates/runtime.json", {})
}

module "service" {
  source = "./modules/service"
}
`,
		},
		{
			RelativePath: "env/prod/terraform.tfvars",
			Content: `app_repo = "payments-service"
`,
		},
		{
			RelativePath: "env/prod/terraform.tfvars.json",
			Content:      `{"app_repo":"payments-service"}`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	wantPaths := []string{
		"buildspec.yaml",
		"config/runtime.yaml",
		"env.hcl",
		"env.hcl",
		"env/prod/terraform.tfvars",
		"env/prod/terraform.tfvars.json",
		"modules/service",
		"root.hcl",
		"root.hcl",
		"templates/runtime.json",
	}
	if len(configPaths) != len(wantPaths) {
		t.Fatalf("len(config_paths) = %d, want %d", len(configPaths), len(wantPaths))
	}
	for index, want := range wantPaths {
		if got, ok := configPaths[index]["path"].(string); !ok || got != want {
			t.Fatalf("config_paths[%d].path = %#v, want %#v", index, configPaths[index]["path"], want)
		}
	}
}

func TestBuildRepositoryConfigArtifactsExtractsTerragruntFindInParentFoldersSidecars(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("iac-terragrunt-core-infra", []FileContent{
		{
			RelativePath: "aws/accounts/qa/us-east-1/qa.network-us-east-1/root.hcl",
			Content: `locals {
  global_vars = try(
    yamldecode(file("${path_relative_to_include()}/global.yaml")),
    yamldecode(file(find_in_parent_folders("global.yaml"))),
    {}
  )
  account_vars = try(
    yamldecode(file(find_in_parent_folders("account.yaml"))),
    {}
  )
  region_vars = try(
    yamldecode(file(find_in_parent_folders("region.yaml"))),
    {}
  )
  env_vars = try(
    yamldecode(file(find_in_parent_folders("env.yaml"))),
    {}
  )
}
`,
		},
		{
			RelativePath: "aws/accounts/qa/us-east-1/qa.network-us-east-1/services/qa-eks/terragrunt.hcl",
			Content: `include "root" {
  path = find_in_parent_folders("root.hcl")
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 7 {
		t.Fatalf("len(config_paths) = %d, want 7", len(configPaths))
	}

	pathCounts := map[string]int{}
	evidenceKinds := map[string]map[string]int{}
	for _, row := range configPaths {
		if got, want := row["source_repo"], "iac-terragrunt-core-infra"; got != want {
			t.Fatalf("config_paths row source_repo = %#v, want %#v", got, want)
		}
		path, _ := row["path"].(string)
		kind, _ := row["evidence_kind"].(string)
		pathCounts[path]++
		if evidenceKinds[path] == nil {
			evidenceKinds[path] = map[string]int{}
		}
		evidenceKinds[path][kind]++
	}

	wantCounts := map[string]int{
		"account.yaml": 1,
		"env.yaml":     1,
		"global.yaml":  2,
		"region.yaml":  1,
		"root.hcl":     2,
	}
	for path, want := range wantCounts {
		if got := pathCounts[path]; got != want {
			t.Fatalf("pathCounts[%q] = %d, want %d", path, got, want)
		}
	}
	if got := evidenceKinds["global.yaml"]["local_config_asset"]; got != 1 {
		t.Fatalf("global.yaml local_config_asset count = %d, want 1", got)
	}
	if got := evidenceKinds["global.yaml"]["terragrunt_find_in_parent_folders"]; got != 1 {
		t.Fatalf("global.yaml terragrunt_find_in_parent_folders count = %d, want 1", got)
	}
	if got := evidenceKinds["root.hcl"]["terragrunt_find_in_parent_folders"]; got != 1 {
		t.Fatalf("root.hcl terragrunt_find_in_parent_folders count = %d, want 1", got)
	}
	if got := evidenceKinds["root.hcl"]["terragrunt_include_path"]; got != 1 {
		t.Fatalf("root.hcl terragrunt_include_path count = %d, want 1", got)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsDefaultTerragruntParentConfig(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("iac-terragrunt-core-infra", []FileContent{
		{
			RelativePath: "modules/eks/terragrunt.hcl",
			Content: `include "root" {
  path = find_in_parent_folders()
}

locals {
  inherited = read_terragrunt_config(find_in_parent_folders())
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 3 {
		t.Fatalf("len(config_paths) = %d, want 3", len(configPaths))
	}

	pathCounts := map[string]int{}
	evidenceKinds := map[string]map[string]int{}
	for _, row := range configPaths {
		path, _ := row["path"].(string)
		kind, _ := row["evidence_kind"].(string)
		pathCounts[path]++
		if evidenceKinds[path] == nil {
			evidenceKinds[path] = map[string]int{}
		}
		evidenceKinds[path][kind]++
	}

	if got, want := pathCounts["terragrunt.hcl"], 3; got != want {
		t.Fatalf("pathCounts[terragrunt.hcl] = %d, want %d", got, want)
	}
	if got, want := evidenceKinds["terragrunt.hcl"]["terragrunt_include_path"], 1; got != want {
		t.Fatalf("terragrunt.hcl terragrunt_include_path count = %d, want %d", got, want)
	}
	if got, want := evidenceKinds["terragrunt.hcl"]["terragrunt_read_config"], 1; got != want {
		t.Fatalf("terragrunt.hcl terragrunt_read_config count = %d, want %d", got, want)
	}
	if got, want := evidenceKinds["terragrunt.hcl"]["terragrunt_find_in_parent_folders"], 1; got != want {
		t.Fatalf("terragrunt.hcl terragrunt_find_in_parent_folders count = %d, want %d", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsAnsibleConfigAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("ansible-ops", []FileContent{
		{
			RelativePath: "deploy.yml",
			Content: `- hosts: workers
  tasks:
    - debug:
        msg: deploy
`,
		},
		{
			RelativePath: "playbooks/site.yml",
			Content: `- hosts: all
  roles:
    - web
`,
		},
		{
			RelativePath: "inventories/prod/hosts.yml",
			Content: `all:
  children:
    web:
      hosts:
        web-1.example.com:
`,
		},
		{
			RelativePath: "group_vars/all.yml",
			Content:      "app_name: demo\n",
		},
		{
			RelativePath: "roles/web/tasks/main.yml",
			Content:      "- debug:\n    msg: hello\n",
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want ansible config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 6 {
		t.Fatalf("len(config_paths) = %d, want 6", len(configPaths))
	}

	want := map[string]string{
		"deploy.yml":                 "ansible_playbook",
		"playbooks/site.yml":         "ansible_playbook",
		"inventories/prod/hosts.yml": "ansible_inventory",
		"group_vars/all.yml":         "ansible_vars",
		"roles/web":                  "ansible_role",
		"roles/web/tasks/main.yml":   "ansible_task_entrypoint",
	}
	for _, row := range configPaths {
		path, _ := row["path"].(string)
		if path == "" {
			t.Fatalf("config_paths row missing path: %#v", row)
		}
		if gotKind, ok := row["evidence_kind"].(string); !ok || gotKind != want[path] {
			t.Fatalf("config_paths[%q].evidence_kind = %#v, want %#v", path, row["evidence_kind"], want[path])
		}
		if gotRepo, ok := row["source_repo"].(string); !ok || gotRepo != "ansible-ops" {
			t.Fatalf("config_paths[%q].source_repo = %#v, want %q", path, row["source_repo"], "ansible-ops")
		}
	}
}

func TestBuildRepositoryConfigArtifactsDoesNotTreatHelmValuesAsAnsiblePlaybook(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("helm-comprehensive", []FileContent{
		{
			RelativePath: "values-prod.yaml",
			Content: `ingress:
  enabled: true
  hosts:
    - host: api.production.example.com
      paths:
        - path: /
`,
		},
	})

	assertNoConfigArtifactEvidenceKind(t, got, "ansible_playbook")
}

func TestBuildRepositoryConfigArtifactsDoesNotTreatKubernetesIngressAsAnsiblePlaybook(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("kubernetes-comprehensive", []FileContent{
		{
			RelativePath: "ingress.yaml",
			Content: `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api
spec:
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
`,
		},
	})

	assertNoConfigArtifactEvidenceKind(t, got, "ansible_playbook")
}

func TestBuildRepositoryConfigArtifactsExtractsLocalVariableConfigAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-modules-aws", []FileContent{
		{
			RelativePath: "modules/service/main.tf",
			Content: `locals {
  config_file_path  = "${path.module}/config/runtime.yaml"
  lifecycle_template = "${path.module}/templates/lifecycle.tpl"
}

resource "example" "service" {
  rendered_config = file(local.config_file_path)
  lifecycle       = templatefile(local.lifecycle_template, {})
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

	wantPaths := []string{"config/runtime.yaml", "templates/lifecycle.tpl"}
	for index, want := range wantPaths {
		if got, ok := configPaths[index]["path"].(string); !ok || got != want {
			t.Fatalf("config_paths[%d].path = %#v, want %#v", index, configPaths[index]["path"], want)
		}
		if got, wantRepo := configPaths[index]["source_repo"], "terraform-modules-aws"; got != wantRepo {
			t.Fatalf("config_paths[%d].source_repo = %#v, want %#v", index, got, wantRepo)
		}
		if got, wantRelative := configPaths[index]["relative_path"], "modules/service/main.tf"; got != wantRelative {
			t.Fatalf("config_paths[%d].relative_path = %#v, want %#v", index, got, wantRelative)
		}
		if got, wantKind := configPaths[index]["evidence_kind"], "local_config_asset"; got != wantKind {
			t.Fatalf("config_paths[%d].evidence_kind = %#v, want %#v", index, got, wantKind)
		}
	}
}

func TestBuildRepositoryConfigArtifactsExtractsLookupBackedLocalVariableConfigAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-credentials", []FileContent{
		{
			RelativePath: "modules/2021.01/ec2/instance/main.tf",
			Content: `locals {
  template = lookup(
    var.configuration,
    "template_file",
    "${path.module}/templates/user_data.tpl",
  )
}

data "template_file" "user_data" {
  template = file(local.template)
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
	if got, want := configPaths[0]["path"], "templates/user_data.tpl"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsTerragruntGetRepoRootModuleSource(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terragrunt-deployment", []FileContent{
		{
			RelativePath: "accounts/dev/us-east-1/dev.network-us-east-1/services/sample-service/root.hcl",
			Content: `terraform {
  source = "${get_repo_root()}/terraform-module-sample-service"
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
	if got, want := configPaths[0]["path"], "terraform-module-sample-service"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "terraform_module_source_path"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsJoinedPathModuleTemplateAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-stack-aws-client-vpn", []FileContent{
		{
			RelativePath: "modules/2024.02/custom/ecs-application/pipeline_node/main.tf",
			Content: `locals {
  appspec_yaml_file = templatefile(join("", [path.module, "/specs/AppSpec.yaml"]), {
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

func TestBuildRepositoryConfigArtifactsExtractsNestedLocalInterpolationConfigAssets(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terraform-stack-event-linking", []FileContent{
		{
			RelativePath: "modules/2021.01/batch/job-definition/main.tf",
			Content: `locals {
  container_template = var.platform_capabilities[0] == "FARGATE" ? "container-fargate.tpl" : "container-ec2.tpl"

  container_properties = templatefile(lookup(var.configuration, "template", "${path.module}/batch/${local.container_template}"),
    {
      name = var.name
    }
  )
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
	if got, want := configPaths[0]["path"], "batch/container-fargate.tpl"; got != want {
		t.Fatalf("config_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := configPaths[0]["evidence_kind"], "local_config_asset"; got != want {
		t.Fatalf("config_paths[0].evidence_kind = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryConfigArtifactsExtractsTerragruntServiceLevelFileAssetsFromNamedPathRelativeToInclude(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terragrunt-deployment", []FileContent{
		{
			RelativePath: "accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl",
			Content: `include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  path_parts   = split("/", path_relative_to_include("root"))
  account_name = local.path_parts[1]  # bg-dev
  region_name  = local.path_parts[2]  # us-east-1
  vpc_name     = local.path_parts[3]  # dev.network-us-east-1

  account_vars = yamldecode(file("${get_repo_root()}/tf-modules/terragrunt-deployment/accounts/${local.account_name}/account.yaml"))
  region_vars  = yamldecode(file("${get_repo_root()}/tf-modules/terragrunt-deployment/accounts/${local.account_name}/${local.region_name}/region.yaml"))
  vpc_vars     = yamldecode(file("${get_repo_root()}/tf-modules/terragrunt-deployment/accounts/${local.account_name}/${local.region_name}/${local.vpc_name}/vpc.yaml"))
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 5 {
		t.Fatalf("len(config_paths) = %d, want 5", len(configPaths))
	}

	pathCounts := map[string]int{}
	for _, row := range configPaths {
		pathCounts[StringVal(row, "path")]++
	}

	wantCounts := map[string]int{
		"accounts/bg-dev/account.yaml":                             1,
		"accounts/bg-dev/us-east-1/region.yaml":                    1,
		"accounts/bg-dev/us-east-1/dev.network-us-east-1/vpc.yaml": 1,
		"root.hcl": 2,
	}
	for wantPath, wantCount := range wantCounts {
		if gotCount := pathCounts[wantPath]; gotCount != wantCount {
			t.Fatalf("pathCounts[%q] = %d, want %d", wantPath, gotCount, wantCount)
		}
	}
}

func TestBuildRepositoryConfigArtifactsExtractsTerragruntServiceLevelFileAssetsFromUnnamedPathRelativeToInclude(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terragrunt-deployment", []FileContent{
		{
			RelativePath: "accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl",
			Content: `include "root" {
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
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths := mapSliceValue(got, "config_paths")
	if len(configPaths) != 5 {
		t.Fatalf("len(config_paths) = %d, want 5", len(configPaths))
	}

	pathCounts := map[string]int{}
	for _, row := range configPaths {
		pathCounts[StringVal(row, "path")]++
	}

	wantCounts := map[string]int{
		"accounts/bg-dev/account.yaml":                             1,
		"accounts/bg-dev/us-east-1/region.yaml":                    1,
		"accounts/bg-dev/us-east-1/dev.network-us-east-1/vpc.yaml": 1,
		"root.hcl": 2,
	}
	for wantPath, wantCount := range wantCounts {
		if gotCount := pathCounts[wantPath]; gotCount != wantCount {
			t.Fatalf("pathCounts[%q] = %d, want %d", wantPath, gotCount, wantCount)
		}
	}
}

func TestBuildRepositoryConfigArtifactsDoesNotPromoteTerragruntRemoteStateKeyAsConfigAsset(t *testing.T) {
	t.Parallel()

	got := buildRepositoryConfigArtifacts("terragrunt-deployment", []FileContent{
		{
			RelativePath: "accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl",
			Content: `include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  path_parts   = split("/", path_relative_to_include("root"))
  account_name = local.path_parts[1]
  region_name  = local.path_parts[2]
  vpc_name     = local.path_parts[3]
}

remote_state {
  backend = "s3"
  config = {
    key = "${local.vpc_name}/services/${path_relative_to_include("root")}/terraform.tfstate"
  }
}
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	for _, row := range mapSliceValue(got, "config_paths") {
		if strings.Contains(StringVal(row, "path"), "terraform.tfstate") {
			t.Fatalf("config_paths contains remote state key row = %#v, want omitted", row)
		}
	}
}

// assertNoConfigArtifactEvidenceKind guards reporting regressions where generic
// YAML config files are surfaced as stronger deployment evidence than they are.
func assertNoConfigArtifactEvidenceKind(t *testing.T, got map[string]any, evidenceKind string) {
	t.Helper()

	if got == nil {
		return
	}
	for _, row := range mapSliceValue(got, "config_paths") {
		if row["evidence_kind"] == evidenceKind {
			t.Fatalf("unexpected config_paths row with evidence_kind %q: %#v", evidenceKind, row)
		}
	}
}
