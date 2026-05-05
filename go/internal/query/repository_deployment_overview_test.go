package query

import "testing"

func TestBuildSharedConfigPathsGroupsDuplicatePathsAcrossRepos(t *testing.T) {
	t.Parallel()

	got := buildSharedConfigPaths(map[string]any{
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
			{
				"path":          "/configd/payments/*",
				"source_repo":   "helm-charts",
				"relative_path": "deploy/policy.yaml",
				"evidence_kind": "kustomize_policy_document_resource",
			},
			{"path": "/api/payments/*", "source_repo": "helm-charts"},
		},
	})

	if len(got) != 1 {
		t.Fatalf("len(shared_config_paths) = %d, want 1", len(got))
	}
	if got[0]["path"] != "/configd/payments/*" {
		t.Fatalf("shared_config_paths[0].path = %#v, want %q", got[0]["path"], "/configd/payments/*")
	}
	sourceRepos, ok := got[0]["source_repositories"].([]string)
	if !ok {
		t.Fatalf("source_repositories type = %T, want []string", got[0]["source_repositories"])
	}
	if len(sourceRepos) != 2 {
		t.Fatalf("len(source_repositories) = %d, want 2", len(sourceRepos))
	}
	if sourceRepos[0] != "helm-charts" || sourceRepos[1] != "terraform-stack-payments" {
		t.Fatalf("source_repositories = %#v, want sorted unique repos", sourceRepos)
	}

	evidenceKinds, ok := got[0]["evidence_kinds"].([]string)
	if !ok {
		t.Fatalf("evidence_kinds type = %T, want []string", got[0]["evidence_kinds"])
	}
	if len(evidenceKinds) != 2 {
		t.Fatalf("len(evidence_kinds) = %d, want 2", len(evidenceKinds))
	}
	if evidenceKinds[0] != "kustomize_policy_document_resource" || evidenceKinds[1] != "terragrunt_dependency_config_path" {
		t.Fatalf("evidence_kinds = %#v, want sorted unique evidence kinds", evidenceKinds)
	}

	relativePaths, ok := got[0]["relative_paths"].([]string)
	if !ok {
		t.Fatalf("relative_paths type = %T, want []string", got[0]["relative_paths"])
	}
	if len(relativePaths) != 2 {
		t.Fatalf("len(relative_paths) = %d, want 2", len(relativePaths))
	}
	if relativePaths[0] != "deploy/policy.yaml" || relativePaths[1] != "env/prod/terragrunt.hcl" {
		t.Fatalf("relative_paths = %#v, want sorted unique relative paths", relativePaths)
	}
}

func TestBuildSharedConfigPathsOmitsBlankAndSingleSourceRows(t *testing.T) {
	t.Parallel()

	got := buildSharedConfigPaths(map[string]any{
		"config_paths": []map[string]any{
			{"path": "", "source_repo": "helm-charts"},
			{"path": "/configd/payments/*", "source_repo": ""},
			{"path": "/configd/payments/*", "source_repo": "helm-charts"},
		},
	})

	if len(got) != 0 {
		t.Fatalf("shared_config_paths = %#v, want empty", got)
	}
}

func TestBuildTopologyStoryIncludesSharedConfigLine(t *testing.T) {
	t.Parallel()

	got := buildOverviewTopologyStory(nil, []map[string]any{
		{
			"path":                "/configd/payments/*",
			"source_repositories": []string{"helm-charts", "terraform-stack-payments"},
		},
	})

	if len(got) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(got))
	}
	want := "Shared config families span /configd/payments/* across helm-charts, terraform-stack-payments."
	if got[0] != want {
		t.Fatalf("topology_story[0] = %q, want %q", got[0], want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesDeliveryPathsAndWorkflows(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"argocd_application"},
		[]string{"argocd", "docker_compose"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"controller_artifacts": []map[string]any{
					{
						"path":             "Jenkinsfile",
						"controller_kind":  "jenkins_pipeline",
						"shared_libraries": []string{"pipelines"},
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
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 2 {
		t.Fatalf("len(delivery_paths) = %d, want 2", len(deliveryPaths))
	}
	if got, want := deliveryPaths[0]["path"], "Jenkinsfile"; got != want {
		t.Fatalf("delivery_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[1]["path"], "docker-compose.yaml"; got != want {
		t.Fatalf("delivery_paths[1].path = %#v, want %#v", got, want)
	}

	deliveryWorkflows, ok := got["delivery_workflows"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_workflows type = %T, want []map[string]any", got["delivery_workflows"])
	}
	if len(deliveryWorkflows) != 1 {
		t.Fatalf("len(delivery_workflows) = %d, want 1", len(deliveryWorkflows))
	}
	if got, want := deliveryWorkflows[0]["controller_kind"], "jenkins_pipeline"; got != want {
		t.Fatalf("delivery_workflows[0].controller_kind = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 2 {
		t.Fatalf("len(topology_story) = %d, want 2", len(topologyStory))
	}
	if got, want := topologyStory[0], "Controller delivery paths include Jenkinsfile via jenkins_pipeline (entry points dist/api.js; shared libraries pipelines; ansible playbooks deploy.yml; ansible inventories inventory/dynamic_hosts.py; ansible vars group_vars/all.yml, host_vars/web-prod.yml; ansible task entrypoints roles/website_import/tasks/main.yml)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
	if got, want := topologyStory[1], "Runtime artifacts include docker_compose service api in docker-compose.yaml built from ./ (build, ports)."; got != want {
		t.Fatalf("topology_story[1] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesDockerfileRuntimeStory(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"ecs_service"},
		[]string{"docker"},
		map[string]any{
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
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := deliveryPaths[0]["artifact_name"], "runtime"; got != want {
		t.Fatalf("delivery_paths[0].artifact_name = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["cmd"], `["/app", "--serve"]`; got != want {
		t.Fatalf("delivery_paths[0].cmd = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Runtime artifacts include dockerfile stage runtime in Dockerfile based on alpine with cmd [\"/app\", \"--serve\"] (base_image, copy_from, cmd, ports)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesComposeRuntimeLinkageMetadata(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"docker_compose"},
		[]string{"docker_compose"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "docker-compose.yaml",
						"artifact_type": "docker_compose",
						"service_name":  "api",
						"env_files":     []string{".env", "deploy/api.env"},
						"configs":       []string{"app-config", "api-runtime"},
						"secrets":       []string{"db-password", "api-token"},
						"signals":       []string{"env_files", "configs", "secrets"},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "env_files"), []string{".env", "deploy/api.env"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].env_files = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deliveryPaths[0], "configs"), []string{"app-config", "api-runtime"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].configs = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deliveryPaths[0], "secrets"), []string{"db-password", "api-token"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].secrets = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Runtime artifacts include docker_compose service api in docker-compose.yaml with env files .env, deploy/api.env, configs app-config, api-runtime, and secrets db-password, api-token (env_files, configs, secrets)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesWorkflowArtifactsInDeliveryPaths(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"argocd_application"},
		[]string{"github_actions"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"workflow_artifacts": []map[string]any{
					{
						"relative_path": ".github/workflows/deploy.yaml",
						"artifact_type": "github_actions_workflow",
						"workflow_name": "deploy",
						"signals":       []string{"workflow_file"},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := deliveryPaths[0]["kind"], "workflow_artifact"; got != want {
		t.Fatalf("delivery_paths[0].kind = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["workflow_name"], "deploy"; got != want {
		t.Fatalf("delivery_paths[0].workflow_name = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy.yaml as github_actions_workflow deploy (workflow_file)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesSingleSourceConfigPathsInDeliveryPaths(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"argocd_application"},
		[]string{"terraform", "terragrunt"},
		map[string]any{
			"deployment_artifacts": map[string]any{
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
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := deliveryPaths[0]["kind"], "config_artifact"; got != want {
		t.Fatalf("delivery_paths[0].kind = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["path"], "root.hcl"; got != want {
		t.Fatalf("delivery_paths[0].path = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["source_repo"], "terraform-stack-payments"; got != want {
		t.Fatalf("delivery_paths[0].source_repo = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["relative_path"], "env/prod/terragrunt.hcl"; got != want {
		t.Fatalf("delivery_paths[0].relative_path = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["evidence_kind"], "terragrunt_include_path"; got != want {
		t.Fatalf("delivery_paths[0].evidence_kind = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Config provenance includes root.hcl from terraform-stack-payments via terragrunt_include_path in env/prod/terragrunt.hcl."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesWorkflowInputRepositoriesInDeliveryPaths(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"github_actions"},
		[]string{"github_actions"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"workflow_artifacts": []map[string]any{
					{
						"relative_path":                  ".github/workflows/dispatch.yaml",
						"artifact_type":                  "github_actions_workflow",
						"workflow_name":                  "dispatch",
						"reusable_workflow_repositories": []string{"example-org/shared-automation"},
						"workflow_input_repositories":    []string{"example-org/automation-fallback", "example-org/shared-automation"},
						"signals":                        []string{"workflow_file", "reusable_workflow_refs", "workflow_input_repositories"},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "workflow_input_repositories"), []string{"example-org/automation-fallback", "example-org/shared-automation"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].workflow_input_repositories = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/dispatch.yaml as github_actions_workflow dispatch via reusable workflow repos example-org/shared-automation and workflow input repos example-org/automation-fallback, example-org/shared-automation (workflow_file, reusable_workflow_refs, workflow_input_repositories)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesCheckoutRepositoriesInDeliveryPaths(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"github_actions"},
		[]string{"github_actions"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"workflow_artifacts": []map[string]any{
					{
						"relative_path":         ".github/workflows/deploy.yaml",
						"artifact_type":         "github_actions_workflow",
						"workflow_name":         "deploy",
						"checkout_repositories": []string{"example-org/deployment-helm", "example-org/deployment-kustomize"},
						"signals":               []string{"workflow_file", "checkout_repositories"},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "checkout_repositories"), []string{"example-org/deployment-helm", "example-org/deployment-kustomize"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].checkout_repositories = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy.yaml as github_actions_workflow deploy via checkout repos example-org/deployment-helm, example-org/deployment-kustomize (workflow_file, checkout_repositories)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesActionRepositoriesInDeliveryPaths(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"github_actions"},
		[]string{"github_actions"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"workflow_artifacts": []map[string]any{
					{
						"relative_path":       ".github/workflows/update-providers.yml",
						"artifact_type":       "github_actions_workflow",
						"workflow_name":       "update-providers",
						"action_repositories": []string{"hashicorp/setup-terraform", "peter-evans/create-pull-request"},
						"signals":             []string{"workflow_file", "action_repositories"},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "action_repositories"), []string{"hashicorp/setup-terraform", "peter-evans/create-pull-request"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].action_repositories = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/update-providers.yml as github_actions_workflow update-providers via action repos hashicorp/setup-terraform, peter-evans/create-pull-request (workflow_file, action_repositories)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesWorkflowTriggerAndMatrixMetadata(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"argocd_application"},
		[]string{"github_actions"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"workflow_artifacts": []map[string]any{
					{
						"relative_path":            ".github/workflows/deploy-matrix.yaml",
						"artifact_type":            "github_actions_workflow",
						"workflow_name":            "deploy-matrix",
						"trigger_events":           []string{"push", "workflow_dispatch"},
						"workflow_inputs":          []string{"deploy_enabled"},
						"matrix_keys":              []string{"region", "runtime"},
						"matrix_combination_count": 4,
						"signals":                  []string{"workflow_file", "workflow_triggers", "matrix_strategy"},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "trigger_events"), []string{"push", "workflow_dispatch"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].trigger_events = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deliveryPaths[0], "workflow_inputs"), []string{"deploy_enabled"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("delivery_paths[0].workflow_inputs = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deliveryPaths[0], "matrix_keys"), []string{"region", "runtime"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].matrix_keys = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["matrix_combination_count"], 4; got != want {
		t.Fatalf("delivery_paths[0].matrix_combination_count = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy-matrix.yaml as github_actions_workflow deploy-matrix triggered by push, workflow_dispatch with workflow inputs deploy_enabled and matrix region, runtime (4 combination(s)) (workflow_file, workflow_triggers, matrix_strategy)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesWorkflowGovernanceMetadataInDeliveryPaths(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"github_actions"},
		[]string{"github_actions"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"workflow_artifacts": []map[string]any{
					{
						"relative_path":      ".github/workflows/deploy-governed.yaml",
						"artifact_type":      "github_actions_workflow",
						"workflow_name":      "deploy-governed",
						"permission_scopes":  []string{"contents:read", "deployments:write", "id-token:write"},
						"concurrency_groups": []string{"deploy-${{ github.ref }}", "deploy-production"},
						"environments":       []string{"production"},
						"job_timeout_minutes": []string{
							"deploy:30",
						},
						"signals": []string{
							"workflow_file",
							"workflow_permissions",
							"workflow_concurrency",
							"workflow_environments",
							"workflow_timeouts",
						},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "permission_scopes"), []string{"contents:read", "deployments:write", "id-token:write"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("delivery_paths[0].permission_scopes = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deliveryPaths[0], "concurrency_groups"), []string{"deploy-${{ github.ref }}", "deploy-production"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].concurrency_groups = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deliveryPaths[0], "environments"), []string{"production"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("delivery_paths[0].environments = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deliveryPaths[0], "job_timeout_minutes"), []string{"deploy:30"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("delivery_paths[0].job_timeout_minutes = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy-governed.yaml as github_actions_workflow deploy-governed with permissions contents:read, deployments:write, id-token:write, concurrency deploy-${{ github.ref }}, deploy-production, environments production, and job timeouts deploy:30 (workflow_file, workflow_permissions, workflow_concurrency, workflow_environments, workflow_timeouts)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesLocalWorkflowCallPaths(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		[]string{"github_actions"},
		[]string{"github_actions"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"workflow_artifacts": []map[string]any{
					{
						"relative_path":                 ".github/workflows/deploy-local.yaml",
						"artifact_type":                 "github_actions_workflow",
						"workflow_name":                 "deploy-local",
						"local_reusable_workflow_paths": []string{".github/workflows/release.yaml", ".github/workflows/verify.yaml"},
						"signals":                       []string{"workflow_file", "local_reusable_workflow_refs"},
					},
				},
			},
		},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "local_reusable_workflow_paths"), []string{".github/workflows/release.yaml", ".github/workflows/verify.yaml"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("delivery_paths[0].local_reusable_workflow_paths = %#v, want %#v", got, want)
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy-local.yaml as github_actions_workflow deploy-local via local reusable workflow paths .github/workflows/release.yaml, .github/workflows/verify.yaml (workflow_file, local_reusable_workflow_refs)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewSynthesizesDualDeliveryFamilies(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		nil,
		[]string{"cloudformation", "jenkins"},
		map[string]any{
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
						"signals":       []string{"template_file", "serverless_transform"},
					},
				},
			},
		},
	)

	deliveryFamilyPaths, ok := got["delivery_family_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_family_paths type = %T, want []map[string]any", got["delivery_family_paths"])
	}
	if len(deliveryFamilyPaths) != 2 {
		t.Fatalf("len(delivery_family_paths) = %d, want 2", len(deliveryFamilyPaths))
	}

	jenkins := requireDeliveryFamilyPath(t, deliveryFamilyPaths, "jenkins")
	if got, want := jenkins["mode"], "controller_delivery"; got != want {
		t.Fatalf("jenkins.mode = %#v, want %#v", got, want)
	}
	if got, want := jenkins["path"], "Jenkinsfile"; got != want {
		t.Fatalf("jenkins.path = %#v, want %#v", got, want)
	}
	if got, want := jenkins["production_evidence"], true; got != want {
		t.Fatalf("jenkins.production_evidence = %#v, want %#v", got, want)
	}

	cloudFormation := requireDeliveryFamilyPath(t, deliveryFamilyPaths, "cloudformation")
	if got, want := cloudFormation["mode"], "serverless_delivery"; got != want {
		t.Fatalf("cloudformation.mode = %#v, want %#v", got, want)
	}
	if got, want := cloudFormation["path"], "infra/serverless.yml"; got != want {
		t.Fatalf("cloudformation.path = %#v, want %#v", got, want)
	}
	if got, want := cloudFormation["artifact_type"], "cloudformation_serverless"; got != want {
		t.Fatalf("cloudformation.artifact_type = %#v, want %#v", got, want)
	}
	if got, want := cloudFormation["production_evidence"], true; got != want {
		t.Fatalf("cloudformation.production_evidence = %#v, want %#v", got, want)
	}

	deliveryFamilyStory, ok := got["delivery_family_story"].([]string)
	if !ok {
		t.Fatalf("delivery_family_story type = %T, want []string", got["delivery_family_story"])
	}
	wantStory := []string{
		"CloudFormation serverless delivery is evidenced by infra/serverless.yml via cloudformation_serverless.",
		"Jenkins delivery is evidenced by Jenkinsfile via jenkins_pipeline.",
	}
	if len(deliveryFamilyStory) != len(wantStory) {
		t.Fatalf("len(delivery_family_story) = %d, want %d", len(deliveryFamilyStory), len(wantStory))
	}
	for index, want := range wantStory {
		if deliveryFamilyStory[index] != want {
			t.Fatalf("delivery_family_story[%d] = %q, want %q", index, deliveryFamilyStory[index], want)
		}
	}
}

func TestBuildRepositoryDeploymentOverviewElevatesGitOpsFamilyFromArgoRelationshipEvidence(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		nil,
		nil,
		map[string]any{
			"relationship_overview": map[string]any{
				"controller_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "delivery-configs",
						"target_id":     "repo-argocd",
						"evidence_type": "argocd_application_source",
					},
				},
			},
		},
	)

	deliveryFamilyPaths, ok := got["delivery_family_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_family_paths type = %T, want []map[string]any", got["delivery_family_paths"])
	}
	if len(deliveryFamilyPaths) != 1 {
		t.Fatalf("len(delivery_family_paths) = %d, want 1", len(deliveryFamilyPaths))
	}

	gitops := requireDeliveryFamilyPath(t, deliveryFamilyPaths, "gitops")
	if got, want := gitops["mode"], "gitops_delivery"; got != want {
		t.Fatalf("gitops.mode = %#v, want %#v", got, want)
	}
	if got, want := gitops["target_name"], "delivery-configs"; got != want {
		t.Fatalf("gitops.target_name = %#v, want %#v", got, want)
	}
	if got, want := gitops["evidence_type"], "argocd_application_source"; got != want {
		t.Fatalf("gitops.evidence_type = %#v, want %#v", got, want)
	}
	if got, want := gitops["production_evidence"], true; got != want {
		t.Fatalf("gitops.production_evidence = %#v, want %#v", got, want)
	}

	deliveryFamilyStory, ok := got["delivery_family_story"].([]string)
	if !ok {
		t.Fatalf("delivery_family_story type = %T, want []string", got["delivery_family_story"])
	}
	if len(deliveryFamilyStory) != 1 {
		t.Fatalf("len(delivery_family_story) = %d, want 1", len(deliveryFamilyStory))
	}
	if got, want := deliveryFamilyStory[0], "GitOps delivery is evidenced by DEPLOYS_FROM delivery-configs via argocd_application_source."; got != want {
		t.Fatalf("delivery_family_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewMarksComposeAsDevelopmentRuntimeEvidence(t *testing.T) {
	t.Parallel()

	got := BuildRepositoryDeploymentOverview(
		[]string{"payments-api"},
		nil,
		[]string{"docker_compose"},
		map[string]any{
			"deployment_artifacts": map[string]any{
				"deployment_artifacts": []map[string]any{
					{
						"relative_path": "docker-compose.yaml",
						"artifact_type": "docker_compose",
						"service_name":  "api",
					},
				},
			},
		},
	)

	deliveryFamilyPaths, ok := got["delivery_family_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_family_paths type = %T, want []map[string]any", got["delivery_family_paths"])
	}
	if len(deliveryFamilyPaths) != 1 {
		t.Fatalf("len(delivery_family_paths) = %d, want 1", len(deliveryFamilyPaths))
	}

	compose := requireDeliveryFamilyPath(t, deliveryFamilyPaths, "docker_compose")
	if got, want := compose["mode"], "development_runtime"; got != want {
		t.Fatalf("docker_compose.mode = %#v, want %#v", got, want)
	}
	if got, want := compose["path"], "docker-compose.yaml"; got != want {
		t.Fatalf("docker_compose.path = %#v, want %#v", got, want)
	}
	if got, want := compose["service_name"], "api"; got != want {
		t.Fatalf("docker_compose.service_name = %#v, want %#v", got, want)
	}
	if got, want := compose["production_evidence"], false; got != want {
		t.Fatalf("docker_compose.production_evidence = %#v, want %#v", got, want)
	}

	deliveryFamilyStory, ok := got["delivery_family_story"].([]string)
	if !ok {
		t.Fatalf("delivery_family_story type = %T, want []string", got["delivery_family_story"])
	}
	if len(deliveryFamilyStory) != 1 {
		t.Fatalf("len(delivery_family_story) = %d, want 1", len(deliveryFamilyStory))
	}
	if got, want := deliveryFamilyStory[0], "Docker Compose runtime evidence is present via docker-compose.yaml for service api; treat it as development/runtime evidence unless stronger production deployment proof exists."; got != want {
		t.Fatalf("delivery_family_story[0] = %q, want %q", got, want)
	}
}

func requireDeliveryFamilyPath(t *testing.T, rows []map[string]any, family string) map[string]any {
	t.Helper()

	for _, row := range rows {
		if StringVal(row, "family") == family {
			return row
		}
	}
	t.Fatalf("delivery_family_paths = %#v, want family %q", rows, family)
	return nil
}
