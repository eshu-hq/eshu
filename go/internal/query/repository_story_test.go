package query

import (
	"strings"
	"testing"
)

func TestBuildRepositoryStoryResponseIncludesStructuredOverviews(t *testing.T) {
	t.Parallel()

	repo := RepoRef{
		ID:        "repository:payments",
		Name:      "payments",
		LocalPath: "/workspace/payments",
		RemoteURL: "https://github.com/acme/payments.git",
		RepoSlug:  "acme/payments",
		HasRemote: true,
	}

	got := buildRepositoryStoryResponse(
		repo,
		42,
		[]string{"go", "yaml"},
		[]string{"payments-api"},
		[]string{"argocd_application"},
		4,
		map[string]any{
			"families": []string{"argocd", "helm", "terraform"},
		},
		nil,
	)

	subject, ok := got["subject"].(map[string]any)
	if !ok {
		t.Fatalf("subject type = %T, want map[string]any", got["subject"])
	}
	if subject["name"] != "payments" {
		t.Fatalf("subject.name = %#v, want %q", subject["name"], "payments")
	}

	if got["story"] == "" {
		t.Fatal("story is empty, want narrative summary")
	}

	storySections, ok := got["story_sections"].([]map[string]any)
	if !ok {
		t.Fatalf("story_sections type = %T, want []map[string]any", got["story_sections"])
	}
	if len(storySections) == 0 {
		t.Fatal("story_sections is empty, want grouped context")
	}

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	if deploymentOverview["workload_count"] != 1 {
		t.Fatalf("deployment_overview.workload_count = %#v, want 1", deploymentOverview["workload_count"])
	}
	if deploymentOverview["platform_count"] != 1 {
		t.Fatalf("deployment_overview.platform_count = %#v, want 1", deploymentOverview["platform_count"])
	}
	if got, ok := deploymentOverview["infrastructure_families"].([]string); !ok || len(got) != 3 {
		t.Fatalf("deployment_overview.infrastructure_families = %#v, want 3 families", deploymentOverview["infrastructure_families"])
	}

	gitopsOverview, ok := got["gitops_overview"].(map[string]any)
	if !ok {
		t.Fatalf("gitops_overview type = %T, want map[string]any", got["gitops_overview"])
	}
	if gitopsOverview["enabled"] != true {
		t.Fatalf("gitops_overview.enabled = %#v, want true", gitopsOverview["enabled"])
	}

	documentationOverview, ok := got["documentation_overview"].(map[string]any)
	if !ok {
		t.Fatalf("documentation_overview type = %T, want map[string]any", got["documentation_overview"])
	}
	if documentationOverview["repo_slug"] != "acme/payments" {
		t.Fatalf("documentation_overview.repo_slug = %#v, want %q", documentationOverview["repo_slug"], "acme/payments")
	}

	supportOverview, ok := got["support_overview"].(map[string]any)
	if !ok {
		t.Fatalf("support_overview type = %T, want map[string]any", got["support_overview"])
	}
	if supportOverview["dependency_count"] != 4 {
		t.Fatalf("support_overview.dependency_count = %#v, want 4", supportOverview["dependency_count"])
	}

	coverageSummary, ok := got["coverage_summary"].(map[string]any)
	if !ok {
		t.Fatalf("coverage_summary type = %T, want map[string]any", got["coverage_summary"])
	}
	if coverageSummary["status"] != "unknown" {
		t.Fatalf("coverage_summary.status = %#v, want %q", coverageSummary["status"], "unknown")
	}

	limitations, ok := got["limitations"].([]string)
	if !ok {
		t.Fatalf("limitations type = %T, want []string", got["limitations"])
	}
	if len(limitations) == 0 {
		t.Fatal("limitations is empty, want truthful limitation note")
	}

	drilldowns, ok := got["drilldowns"].(map[string]any)
	if !ok {
		t.Fatalf("drilldowns type = %T, want map[string]any", got["drilldowns"])
	}
	if drilldowns["context_path"] != "/api/v0/repositories/repository:payments/context" {
		t.Fatalf("drilldowns.context_path = %#v", drilldowns["context_path"])
	}
}

func TestBuildRepositoryStoryResponseIncludesSharedConfigInDirectStory(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:payments", Name: "payments"}
	got := buildRepositoryStoryResponse(
		repo,
		42,
		[]string{"go"},
		[]string{"payments-api"},
		[]string{"argocd_application"},
		2,
		map[string]any{
			"families": []string{"terraform"},
			"deployment_artifacts": map[string]any{
				"config_paths": []map[string]any{
					{"path": "/configd/payments/*", "source_repo": "helm-charts"},
					{"path": "/configd/payments/*", "source_repo": "terraform-stack-payments"},
				},
			},
		},
		nil,
	)

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	topologyStory, ok := deploymentOverview["topology_story"].([]string)
	if !ok || len(topologyStory) != 1 {
		t.Fatalf("topology_story = %#v, want one shared-config line", deploymentOverview["topology_story"])
	}
	if got, want := topologyStory[0], "Shared config families span /configd/payments/* across helm-charts, terraform-stack-payments."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 1 {
		t.Fatalf("len(direct_story) = %d, want 1", len(directStory))
	}
	if got, want := directStory[0], "Shared config families span /configd/payments/* across helm-charts, terraform-stack-payments."; got != want {
		t.Fatalf("direct_story[0] = %q, want %q", got, want)
	}
	sharedConfigPaths, ok := deploymentOverview["shared_config_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("shared_config_paths type = %T, want []map[string]any", deploymentOverview["shared_config_paths"])
	}
	if len(sharedConfigPaths) != 1 {
		t.Fatalf("len(shared_config_paths) = %d, want 1", len(sharedConfigPaths))
	}
	if got, want := sharedConfigPaths[0]["path"], "/configd/payments/*"; got != want {
		t.Fatalf("shared_config_paths[0].path = %#v, want %#v", got, want)
	}
	if _, ok := deploymentOverview["trace_limitations"]; ok {
		t.Fatalf("trace_limitations = %#v, want omitted", deploymentOverview["trace_limitations"])
	}
}

func TestBuildRepositoryStoryResponsePreservesDeliveryPathsInDirectStory(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:payments", Name: "payments"}
	got := buildRepositoryStoryResponse(
		repo,
		42,
		[]string{"go"},
		[]string{"payments-api"},
		[]string{"argocd_application"},
		2,
		map[string]any{
			"families": []string{"terraform"},
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
				"config_paths": []map[string]any{
					{"path": "/configd/payments/*", "source_repo": "helm-charts"},
					{"path": "/configd/payments/*", "source_repo": "terraform-stack-payments"},
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
	if got, want := directStory[0], "Controller delivery paths include Jenkinsfile via jenkins_pipeline (entry points dist/api.js; shared libraries pipelines; ansible playbooks deploy.yml; ansible inventories inventory/dynamic_hosts.py; ansible vars group_vars/all.yml, host_vars/web-prod.yml; ansible task entrypoints roles/website_import/tasks/main.yml)."; got != want {
		t.Fatalf("direct_story[0] = %q, want %q", got, want)
	}
	if got, want := directStory[1], "Runtime artifacts include docker_compose service api in docker-compose.yaml built from ./ (build, ports)."; got != want {
		t.Fatalf("direct_story[1] = %q, want %q", got, want)
	}
	if got, want := directStory[2], "Config provenance includes root.hcl from terraform-stack-payments via terragrunt_include_path in env/prod/terragrunt.hcl."; got != want {
		t.Fatalf("direct_story[2] = %q, want %q", got, want)
	}
	if got, want := directStory[3], "Shared config families span /configd/payments/* across helm-charts, terraform-stack-payments."; got != want {
		t.Fatalf("direct_story[3] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryStoryResponseIncludesDockerfileRuntimeArtifactsInDirectStory(t *testing.T) {
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

func TestBuildRepositoryStoryResponseIncludesWorkflowArtifactsForWorkflowOnlyRepo(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:ci-workflows", Name: "ci-workflows"}
	got := buildRepositoryStoryResponse(
		repo,
		8,
		[]string{"yaml"},
		nil,
		nil,
		0,
		map[string]any{
			"families": []string{"github_actions"},
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
		nil,
	)

	if gotStory, ok := got["story"].(string); !ok || gotStory == "" {
		t.Fatalf("story = %#v, want non-empty string", got["story"])
	}

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	if got, want := deploymentOverview["workload_count"], 0; got != want {
		t.Fatalf("deployment_overview.workload_count = %#v, want %#v", got, want)
	}
	if got, want := deploymentOverview["platform_count"], 0; got != want {
		t.Fatalf("deployment_overview.platform_count = %#v, want %#v", got, want)
	}

	deploymentArtifacts, ok := deploymentOverview["deployment_artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_artifacts type = %T, want map[string]any", deploymentOverview["deployment_artifacts"])
	}
	workflowArtifacts, ok := deploymentArtifacts["workflow_artifacts"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow_artifacts type = %T, want []map[string]any", deploymentArtifacts["workflow_artifacts"])
	}
	if len(workflowArtifacts) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(workflowArtifacts))
	}

	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 1 {
		t.Fatalf("len(direct_story) = %d, want 1", len(directStory))
	}
	if got, want := directStory[0], "Workflow delivery paths include .github/workflows/deploy.yaml as github_actions_workflow deploy (workflow_file)."; got != want {
		t.Fatalf("direct_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryStoryResponseIncludesControllerAndWorkflowProofTogether(t *testing.T) {
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
			"families": []string{"argocd", "ansible", "github_actions", "terraform"},
			"relationship_overview": map[string]any{
				"relationship_count": 5,
				"story":              "Controller-driven relationships: DEPLOYS_FROM infra-configs via argocd_application_source. Workflow-driven relationships: DEPLOYS_FROM ci-workflows via github_actions_reusable_workflow_ref. Controller-driven relationships: DISCOVERS_CONFIG_IN controller-pipelines via jenkins_shared_library. Controller-driven relationships: DEPENDS_ON ansible-ops via ansible_role_reference. IaC-driven relationships: DEPENDS_ON terraform-modules via terraform_module_source.",
				"controller_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "infra-configs",
						"target_id":     "repo-2",
						"evidence_type": "argocd_application_source",
					},
					{
						"type":          "DISCOVERS_CONFIG_IN",
						"target_name":   "controller-pipelines",
						"target_id":     "repo-3",
						"evidence_type": "jenkins_shared_library",
					},
					{
						"type":          "DEPENDS_ON",
						"target_name":   "ansible-ops",
						"target_id":     "repo-4",
						"evidence_type": "ansible_role_reference",
					},
				},
				"workflow_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "ci-workflows",
						"target_id":     "repo-5",
						"evidence_type": "github_actions_reusable_workflow_ref",
					},
				},
				"iac_driven": []map[string]any{
					{
						"type":          "DEPENDS_ON",
						"target_name":   "terraform-modules",
						"target_id":     "repo-6",
						"evidence_type": "terraform_module_source",
					},
				},
			},
			"deployment_artifacts": map[string]any{
				"controller_artifacts": []map[string]any{
					{
						"path":             "Jenkinsfile",
						"controller_kind":  "jenkins_pipeline",
						"shared_libraries": []string{"pipelines"},
						"pipeline_calls":   []string{"pipelineDeploy"},
						"entry_points":     []string{"dist/api.js"},
						"shell_commands":   []string{"./scripts/deploy.sh"},
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
			},
		},
		nil,
	)

	if gotStory, ok := got["story"].(string); !ok || gotStory == "" {
		t.Fatalf("story = %#v, want non-empty string", got["story"])
	} else {
		for _, want := range []string{
			"Controller-driven relationships:",
			"Workflow-driven relationships:",
			"IaC-driven relationships:",
			"argocd_application_source",
			"github_actions_reusable_workflow_ref",
			"terraform_module_source",
		} {
			if !strings.Contains(gotStory, want) {
				t.Fatalf("story = %q, want %q", gotStory, want)
			}
		}
	}

	storySections, ok := got["story_sections"].([]map[string]any)
	if !ok {
		t.Fatalf("story_sections type = %T, want []map[string]any", got["story_sections"])
	}
	var relationshipsSummary string
	for _, section := range storySections {
		if section["title"] == "relationships" {
			relationshipsSummary, _ = section["summary"].(string)
			break
		}
	}
	if relationshipsSummary == "" {
		t.Fatal("story_sections missing relationships section")
	}
	for _, want := range []string{"Controller-driven relationships:", "Workflow-driven relationships:"} {
		if !strings.Contains(relationshipsSummary, want) {
			t.Fatalf("relationships summary = %q, want %q", relationshipsSummary, want)
		}
	}

	relationshipOverview, ok := got["relationship_overview"].(map[string]any)
	if !ok {
		t.Fatalf("relationship_overview type = %T, want map[string]any", got["relationship_overview"])
	}
	controllerDriven, ok := relationshipOverview["controller_driven"].([]map[string]any)
	if !ok {
		t.Fatalf("relationship_overview.controller_driven type = %T, want []map[string]any", relationshipOverview["controller_driven"])
	}
	controllerEvidence := map[string]struct{}{}
	for _, row := range controllerDriven {
		controllerEvidence[row["evidence_type"].(string)] = struct{}{}
	}
	for _, want := range []string{"argocd_application_source", "jenkins_shared_library", "ansible_role_reference"} {
		if _, ok := controllerEvidence[want]; !ok {
			t.Fatalf("relationship_overview.controller_driven missing evidence_type %q", want)
		}
	}

	deploymentOverview, ok := got["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", got["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 2 {
		t.Fatalf("len(direct_story) = %d, want 2", len(directStory))
	}
	wantControllerLine := "Controller delivery paths include Jenkinsfile via jenkins_pipeline (entry points dist/api.js; shared libraries pipelines; pipeline calls pipelineDeploy; ansible playbooks deploy.yml; ansible inventories inventory/dynamic_hosts.py; ansible vars group_vars/all.yml, host_vars/web-prod.yml; ansible task entrypoints roles/website_import/tasks/main.yml)."
	wantWorkflowLine := "Workflow delivery paths include .github/workflows/deploy.yaml as github_actions_workflow deploy (workflow_file)."
	foundControllerLine := false
	foundWorkflowLine := false
	for _, line := range directStory {
		if line == wantControllerLine {
			foundControllerLine = true
		}
		if line == wantWorkflowLine {
			foundWorkflowLine = true
		}
	}
	if !foundControllerLine {
		t.Fatalf("direct_story = %#v, want controller line %q", directStory, wantControllerLine)
	}
	if !foundWorkflowLine {
		t.Fatalf("direct_story = %#v, want workflow line %q", directStory, wantWorkflowLine)
	}
}
