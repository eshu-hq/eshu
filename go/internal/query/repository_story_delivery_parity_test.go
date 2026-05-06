package query

import "testing"

func TestBuildRepositoryStoryResponsePreservesCombinedDeliverySurfaces(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:platform-service", Name: "platform-service"}
	got := buildRepositoryStoryResponse(
		repo,
		64,
		[]string{"go", "yaml"},
		[]string{"platform-runtime"},
		[]string{"argocd_application", "github_actions"},
		5,
		map[string]any{
			"families": []string{"argocd", "ansible", "github_actions", "docker_compose", "terraform"},
			"deployment_artifacts": map[string]any{
				"controller_artifacts": []map[string]any{
					{
						"path":             "Jenkinsfile",
						"controller_kind":  "jenkins_pipeline",
						"shared_libraries": []string{"pipelines"},
						"pipeline_calls":   []string{"pipelineDeploy"},
						"entry_points":     []string{"dist/api.js"},
						"ansible_inventories": []string{
							"inventory/dynamic_hosts.py",
						},
						"ansible_var_files": []string{
							"group_vars/all.yml",
							"host_vars/web-prod.yml",
						},
						"ansible_task_entrypoints": []string{
							"roles/website_import/tasks/main.yml",
						},
						"ansible_playbook_hints": []map[string]any{
							{"playbook": "deploy.yml"},
						},
					},
				},
				"workflow_artifacts": []map[string]any{
					{
						"relative_path": ".github/workflows/deploy.yaml",
						"artifact_type": "github_actions_workflow",
						"workflow_name": "deploy",
						"signals":       []string{"workflow_file"},
					},
				},
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "docker-compose.yaml",
						"artifact_type": "docker_compose",
						"service_name":  "api",
						"signals":       []string{"build", "ports"},
						"build_context": "./",
					},
				},
				"config_paths": []map[string]any{
					{
						"path":          "root.hcl",
						"source_repo":   "terraform-stack-payments",
						"relative_path": "env/prod/terragrunt.hcl",
						"evidence_kind": "terragrunt_include_path",
					},
				},
			},
		},
		nil,
	)

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}

	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 4 {
		t.Fatalf("len(direct_story) = %d, want 4", len(directStory))
	}

	want := []string{
		"Workflow delivery paths include .github/workflows/deploy.yaml as github_actions_workflow deploy (workflow_file).",
		"Controller delivery paths include Jenkinsfile via jenkins_pipeline (entry points dist/api.js; shared libraries pipelines; pipeline calls pipelineDeploy; ansible playbooks deploy.yml; ansible inventories inventory/dynamic_hosts.py; ansible vars group_vars/all.yml, host_vars/web-prod.yml; ansible task entrypoints roles/website_import/tasks/main.yml).",
		"Runtime artifacts include docker_compose service api in docker-compose.yaml built from ./ (build, ports).",
		"Config provenance includes root.hcl from terraform-stack-payments via terragrunt_include_path in env/prod/terragrunt.hcl.",
	}
	for i, wantLine := range want {
		if directStory[i] != wantLine {
			t.Fatalf("direct_story[%d] = %q, want %q", i, directStory[i], wantLine)
		}
	}

	topologyStory, ok := deploymentOverview["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", deploymentOverview["topology_story"])
	}
	if len(topologyStory) != len(want) {
		t.Fatalf("len(topology_story) = %d, want %d", len(topologyStory), len(want))
	}
	for i, wantLine := range want {
		if topologyStory[i] != wantLine {
			t.Fatalf("topology_story[%d] = %q, want %q", i, topologyStory[i], wantLine)
		}
	}
}

func TestBuildRepositoryStoryResponsePreservesDockerfileRelationshipAndRuntimeStory(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:payments", Name: "payments"}
	got := buildRepositoryStoryResponse(
		repo,
		42,
		[]string{"go"},
		[]string{"payments-api"},
		[]string{"ecs_service"},
		1,
		map[string]any{
			"families": []string{"docker"},
			"relationship_overview": map[string]any{
				"relationship_count": 1,
				"story":              "IaC-driven relationships: DEPLOYS_FROM payments-service via dockerfile_source_label.",
				"iac_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "payments-service",
						"target_id":     "repo-7",
						"evidence_type": "dockerfile_source_label",
					},
				},
			},
			"deployment_artifacts": map[string]any{
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "Dockerfile",
						"artifact_type": "dockerfile",
						"artifact_name": "runtime",
						"base_image":    "alpine",
						"cmd":           `["/app", "--serve"]`,
						"signals":       []string{"base_image", "copy_from", "cmd", "ports"},
					},
				},
			},
		},
		nil,
	)

	story, ok := got["story"].(string)
	if !ok {
		t.Fatalf("story type = %T, want string", got["story"])
	}
	if story == "" {
		t.Fatal("story is empty, want combined narrative")
	}
	if want := "dockerfile_source_label"; !containsSubstring(story, want) {
		t.Fatalf("story = %q, want %q", story, want)
	}

	relationshipOverview, ok := got["relationship_overview"].(map[string]any)
	if !ok {
		t.Fatalf("relationship_overview type = %T, want map[string]any", got["relationship_overview"])
	}
	if got, want := relationshipOverview["relationship_count"], 1; got != want {
		t.Fatalf("relationship_overview.relationship_count = %#v, want %#v", got, want)
	}

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 1 {
		t.Fatalf("len(direct_story) = %d, want 1", len(directStory))
	}
	if got, want := directStory[0], "Runtime artifacts include dockerfile stage runtime in Dockerfile based on alpine with cmd [\"/app\", \"--serve\"] (base_image, copy_from, cmd, ports)."; got != want {
		t.Fatalf("direct_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryStoryResponsePreservesTerragruntNestedConfigStory(t *testing.T) {
	t.Parallel()

	configArtifacts := buildRepositoryConfigArtifacts("terragrunt-deployment", []FileContent{
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

  inherited    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  account_vars = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/account.yaml"))
  region_vars  = yamldecode(file("${get_repo_root()}/accounts/${local.account_name}/${local.region_name}/region.yaml"))
}
`,
		},
	})
	if configArtifacts == nil {
		t.Fatal("buildRepositoryConfigArtifacts() = nil, want config_paths")
	}

	configPaths, ok := configArtifacts["config_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("config_paths type = %T, want []map[string]any", configArtifacts["config_paths"])
	}

	repo := RepoRef{ID: "repository:terragrunt-deployment", Name: "terragrunt-deployment"}
	got := buildRepositoryStoryResponse(
		repo,
		12,
		[]string{"hcl"},
		nil,
		nil,
		0,
		map[string]any{
			"families": []string{"terragrunt", "terraform"},
			"deployment_artifacts": map[string]any{
				"config_paths": configPaths,
			},
		},
		nil,
	)

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}

	wantLines := []string{
		"Config provenance includes accounts/bg-dev/account.yaml from terragrunt-deployment via local_config_asset in accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl.",
		"Config provenance includes accounts/bg-dev/us-east-1/region.yaml from terragrunt-deployment via local_config_asset in accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl.",
		"Config provenance includes env.hcl from terragrunt-deployment via terragrunt_find_in_parent_folders in accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl.",
		"Config provenance includes env.hcl from terragrunt-deployment via terragrunt_read_config in accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl.",
		"Config provenance includes root.hcl from terragrunt-deployment via terragrunt_include_path in accounts/bg-dev/us-east-1/dev.network-us-east-1/services/terragrunt.hcl.",
	}
	for _, want := range wantLines {
		if !containsExactLine(directStory, want) {
			t.Fatalf("direct_story = %#v, want line %q", directStory, want)
		}
	}
}

func TestBuildRepositoryStoryResponsePreservesSharedConfigAlongsideDeliverySurfaces(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:platform-service", Name: "platform-service"}
	got := buildRepositoryStoryResponse(
		repo,
		64,
		[]string{"go", "yaml"},
		[]string{"platform-runtime"},
		[]string{"argocd_application", "github_actions"},
		5,
		map[string]any{
			"families": []string{"argocd", "ansible", "github_actions", "docker_compose", "terraform"},
			"deployment_artifacts": map[string]any{
				"controller_artifacts": []map[string]any{
					{
						"path":             "Jenkinsfile",
						"controller_kind":  "jenkins_pipeline",
						"shared_libraries": []string{"pipelines"},
						"entry_points":     []string{"dist/api.js"},
					},
				},
				"workflow_artifacts": []map[string]any{
					{
						"relative_path": ".github/workflows/deploy.yaml",
						"artifact_type": "github_actions_workflow",
						"workflow_name": "deploy",
						"signals":       []string{"workflow_file"},
					},
				},
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "docker-compose.yaml",
						"artifact_type": "docker_compose",
						"service_name":  "api",
						"signals":       []string{"build", "ports"},
						"build_context": "./",
					},
				},
				"config_paths": []map[string]any{
					{
						"path":          "/configd/payments/*",
						"source_repo":   "helm-charts",
						"relative_path": "deploy/policy.yaml",
						"evidence_kind": "kustomize_policy_document_resource",
					},
					{
						"path":          "/configd/payments/*",
						"source_repo":   "terraform-stack-payments",
						"relative_path": "env/prod/terragrunt.hcl",
						"evidence_kind": "terragrunt_dependency_config_path",
					},
				},
			},
		},
		nil,
	)

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}

	wantLines := []string{
		"Workflow delivery paths include .github/workflows/deploy.yaml as github_actions_workflow deploy (workflow_file).",
		"Controller delivery paths include Jenkinsfile via jenkins_pipeline (entry points dist/api.js; shared libraries pipelines).",
		"Runtime artifacts include docker_compose service api in docker-compose.yaml built from ./ (build, ports).",
		"Shared config families span /configd/payments/* across helm-charts, terraform-stack-payments.",
	}
	for _, want := range wantLines {
		if !containsExactLine(directStory, want) {
			t.Fatalf("direct_story = %#v, want line %q", directStory, want)
		}
	}
}

func TestBuildRepositoryStoryResponsePreservesDockerAndComposeRelationshipEvidenceWithDeliveryStory(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:platform-runtime", Name: "platform-runtime"}
	got := buildRepositoryStoryResponse(
		repo,
		64,
		[]string{"go", "yaml", "dockerfile"},
		[]string{"platform-runtime"},
		[]string{"argocd_application", "github_actions", "docker_compose"},
		6,
		map[string]any{
			"families": []string{"argocd", "github_actions", "docker", "docker_compose", "terraform"},
			"relationship_overview": map[string]any{
				"relationship_count": 6,
				"story":              "Controller-driven relationships: DEPLOYS_FROM infra-configs via argocd_application_source. Workflow-driven relationships: DEPLOYS_FROM ci-workflows via github_actions_reusable_workflow_ref. IaC-driven relationships: DEPLOYS_FROM runtime-image via dockerfile_source_label. IaC-driven relationships: DEPLOYS_FROM ../api via docker_compose_build_context. IaC-driven relationships: DEPLOYS_FROM ghcr.io/acme/api:1.2.3 via docker_compose_image. IaC-driven relationships: DEPENDS_ON database via docker_compose_depends_on.",
				"controller_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "infra-configs",
						"target_id":     "repo-ctrl",
						"evidence_type": "argocd_application_source",
					},
				},
				"workflow_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "ci-workflows",
						"target_id":     "repo-wf",
						"evidence_type": "github_actions_reusable_workflow_ref",
					},
				},
				"iac_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "runtime-image",
						"target_id":     "repo-dockerfile",
						"evidence_type": "dockerfile_source_label",
					},
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "../api",
						"target_id":     "repo-compose-build",
						"evidence_type": "docker_compose_build_context",
					},
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "ghcr.io/acme/api:1.2.3",
						"target_id":     "repo-compose-image",
						"evidence_type": "docker_compose_image",
					},
					{
						"type":          "DEPENDS_ON",
						"target_name":   "database",
						"target_id":     "repo-compose-db",
						"evidence_type": "docker_compose_depends_on",
					},
				},
			},
			"deployment_artifacts": map[string]any{
				"controller_artifacts": []map[string]any{
					{
						"path":            "Jenkinsfile",
						"controller_kind": "jenkins_pipeline",
						"entry_points":    []string{"dist/api.js"},
					},
				},
				"workflow_artifacts": []map[string]any{
					{
						"relative_path": ".github/workflows/deploy.yaml",
						"artifact_type": "github_actions_workflow",
						"workflow_name": "deploy",
						"signals":       []string{"workflow_file"},
					},
				},
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "docker-compose.yaml",
						"artifact_type": "docker_compose",
						"service_name":  "api",
						"signals":       []string{"build", "ports"},
						"build_context": "./",
					},
				},
			},
		},
		nil,
	)

	story, ok := got["story"].(string)
	if !ok {
		t.Fatalf("story type = %T, want string", got["story"])
	}
	for _, want := range []string{
		"argocd_application_source",
		"github_actions_reusable_workflow_ref",
		"dockerfile_source_label",
		"docker_compose_build_context",
		"docker_compose_image",
		"docker_compose_depends_on",
	} {
		if !containsSubstring(story, want) {
			t.Fatalf("story = %q, want %q", story, want)
		}
	}

	relationshipOverview, ok := got["relationship_overview"].(map[string]any)
	if !ok {
		t.Fatalf("relationship_overview type = %T, want map[string]any", got["relationship_overview"])
	}
	if got, want := relationshipOverview["relationship_count"], 6; got != want {
		t.Fatalf("relationship_overview.relationship_count = %#v, want %#v", got, want)
	}

	iacDriven, ok := relationshipOverview["iac_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("relationship_overview.iac_driven type = %T, want []map[string]any", relationshipOverview["iac_driven"])
	}
	wantEvidence := map[string]struct{}{
		"dockerfile_source_label":      {},
		"docker_compose_build_context": {},
		"docker_compose_image":         {},
		"docker_compose_depends_on":    {},
	}
	for _, row := range iacDriven {
		delete(wantEvidence, StringVal(row, "evidence_type"))
	}
	if len(wantEvidence) != 0 {
		t.Fatalf("relationship_overview.iac_driven missing evidence types: %#v", wantEvidence)
	}

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	for _, want := range []string{
		"Workflow delivery paths include .github/workflows/deploy.yaml as github_actions_workflow deploy (workflow_file).",
		"Controller delivery paths include Jenkinsfile via jenkins_pipeline (entry points dist/api.js).",
		"Runtime artifacts include docker_compose service api in docker-compose.yaml built from ./ (build, ports).",
	} {
		if !containsExactLine(directStory, want) {
			t.Fatalf("direct_story = %#v, want line %q", directStory, want)
		}
	}
}

func TestRepositoryStoryDeliveryParitySynthesizesDeliveryFamilyParityWithoutCollapsingControllers(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:payments-service", Name: "payments-service"}
	got := buildRepositoryStoryResponse(
		repo,
		18,
		[]string{"yaml", "groovy"},
		[]string{"payments-api"},
		nil,
		1,
		map[string]any{
			"families": []string{"cloudformation", "docker_compose", "jenkins"},
			"relationship_overview": map[string]any{
				"controller_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "delivery-configs",
						"target_id":     "repo-gitops",
						"evidence_type": "argocd_application_source",
					},
				},
			},
			"deployment_artifacts": map[string]any{
				"controller_artifacts": []map[string]any{
					{
						"path":            "Jenkinsfile",
						"controller_kind": "jenkins_pipeline",
					},
				},
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "infra/serverless.yml",
						"artifact_type": "cloudformation_serverless",
						"artifact_name": "payments-stack",
					},
					{
						"relative_path": "docker-compose.yaml",
						"artifact_type": "docker_compose",
						"service_name":  "api",
					},
				},
			},
		},
		nil,
	)

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}

	deliveryFamilyPaths, ok := deploymentOverview["delivery_family_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_family_paths type = %T, want []map[string]any", deploymentOverview["delivery_family_paths"])
	}
	if len(deliveryFamilyPaths) != 4 {
		t.Fatalf("len(delivery_family_paths) = %d, want 4", len(deliveryFamilyPaths))
	}
	for _, family := range []string{"cloudformation", "docker_compose", "gitops", "jenkins"} {
		if requireRepositoryStoryDeliveryFamily(deliveryFamilyPaths, family) == nil {
			t.Fatalf("delivery_family_paths = %#v, want family %q", deliveryFamilyPaths, family)
		}
	}

	deliveryFamilyStory, ok := deploymentOverview["delivery_family_story"].([]string)
	if !ok {
		t.Fatalf("delivery_family_story type = %T, want []string", deploymentOverview["delivery_family_story"])
	}
	wantLines := []string{
		"CloudFormation serverless delivery is evidenced by infra/serverless.yml via cloudformation_serverless.",
		"Docker Compose runtime evidence is present via docker-compose.yaml for service api; treat it as development/runtime evidence unless stronger production deployment proof exists.",
		"GitOps delivery is evidenced by DEPLOYS_FROM delivery-configs via argocd_application_source.",
		"Jenkins delivery is evidenced by Jenkinsfile via jenkins_pipeline.",
	}
	if len(deliveryFamilyStory) != len(wantLines) {
		t.Fatalf("len(delivery_family_story) = %d, want %d", len(deliveryFamilyStory), len(wantLines))
	}
	for _, want := range wantLines {
		if !containsExactLine(deliveryFamilyStory, want) {
			t.Fatalf("delivery_family_story = %#v, want line %q", deliveryFamilyStory, want)
		}
	}

	story, ok := got["story"].(string)
	if !ok {
		t.Fatalf("story type = %T, want string", got["story"])
	}
	for _, want := range wantLines {
		if !containsSubstring(story, want) {
			t.Fatalf("story = %q, want %q", story, want)
		}
	}

	gitopsOverview, ok := got["gitops_overview"].(map[string]any)
	if !ok {
		t.Fatalf("gitops_overview type = %T, want map[string]any", got["gitops_overview"])
	}
	if got, want := gitopsOverview["enabled"], true; got != want {
		t.Fatalf("gitops_overview.enabled = %#v, want %#v", got, want)
	}
	if !containsStringValue(StringSliceVal(gitopsOverview, "tool_families"), "argocd") {
		t.Fatalf("gitops_overview.tool_families = %#v, want argocd", gitopsOverview["tool_families"])
	}

	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if !containsExactLine(directStory, "Controller delivery paths include Jenkinsfile via jenkins_pipeline.") {
		t.Fatalf("direct_story = %#v, want Jenkins controller artifact line preserved", directStory)
	}
	if !containsExactLine(directStory, "Runtime artifacts include cloudformation_serverless stage payments-stack in infra/serverless.yml.") {
		t.Fatalf("direct_story = %#v, want CloudFormation artifact line preserved", directStory)
	}
	if !containsExactLine(directStory, "Runtime artifacts include docker_compose service api in docker-compose.yaml.") {
		t.Fatalf("direct_story = %#v, want Compose artifact line preserved", directStory)
	}
}

func requireRepositoryStoryDeliveryFamily(rows []map[string]any, family string) map[string]any {
	for _, row := range rows {
		if StringVal(row, "family") == family {
			return row
		}
	}
	return nil
}

func containsStringValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsExactLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
