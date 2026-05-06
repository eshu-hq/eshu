package query

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRelationshipPlatformWorkerWorkflowSurfacesReadSideDeliveryPath(t *testing.T) {
	t.Parallel()

	artifacts := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-modern.yml",
			ArtifactType: "github_actions_workflow",
			Content: readRelationshipPlatformFixture(
				t,
				"service-worker-jobs",
				".github",
				"workflows",
				"deploy-modern.yml",
			),
		},
	})
	if artifacts == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	overview := BuildRepositoryDeploymentOverview(
		[]string{"service-worker-jobs"},
		nil,
		[]string{"github_actions"},
		map[string]any{"deployment_artifacts": artifacts},
	)

	deliveryPaths, ok := overview["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", overview["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := deliveryPaths[0]["workflow_name"], "deploy-modern"; got != want {
		t.Fatalf("delivery_paths[0].workflow_name = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["kind"], "workflow_artifact"; got != want {
		t.Fatalf("delivery_paths[0].kind = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["command_count"], 1; got != want {
		t.Fatalf("delivery_paths[0].command_count = %#v, want %#v", got, want)
	}
	runCommands, ok := deliveryPaths[0]["run_commands"].([]string)
	if !ok {
		t.Fatalf("delivery_paths[0].run_commands type = %T, want []string", deliveryPaths[0]["run_commands"])
	}
	if len(runCommands) != 1 || runCommands[0] != `echo "deploying service-worker-jobs to modern kubernetes"` {
		t.Fatalf("delivery_paths[0].run_commands = %#v, want deploy command", runCommands)
	}

	topologyStory, ok := overview["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", overview["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy-modern.yml as github_actions_workflow deploy-modern triggered by workflow_dispatch with 1 run command(s) (workflow_file, run_commands, workflow_triggers)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}

	story := buildRepositoryStoryResponse(
		RepoRef{ID: "repository:service-worker-jobs", Name: "service-worker-jobs"},
		5,
		[]string{"yaml"},
		nil,
		nil,
		0,
		map[string]any{
			"families":             []string{"github_actions"},
			"deployment_artifacts": artifacts,
		},
		nil,
	)
	deploymentOverview, ok := story["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", story["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 1 {
		t.Fatalf("len(direct_story) = %d, want 1", len(directStory))
	}
	if got, want := directStory[0], "Workflow delivery paths include .github/workflows/deploy-modern.yml as github_actions_workflow deploy-modern triggered by workflow_dispatch with 1 run command(s) (workflow_file, run_commands, workflow_triggers)."; got != want {
		t.Fatalf("direct_story[0] = %q, want %q", got, want)
	}
}

func TestRelationshipPlatformLegacyWorkflowSurfacesReusableWorkflowAndRunCommands(t *testing.T) {
	t.Parallel()

	artifacts := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-legacy.yml",
			ArtifactType: "github_actions_workflow",
			Content: readRelationshipPlatformFixture(
				t,
				"service-edge-api",
				".github",
				"workflows",
				"deploy-legacy.yml",
			),
		},
	})
	if artifacts == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows, ok := artifacts["workflow_artifacts"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow_artifacts type = %T, want []map[string]any", artifacts["workflow_artifacts"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	if got, want := rows[0]["command_count"], 1; got != want {
		t.Fatalf("workflow_artifacts[0].command_count = %#v, want %#v", got, want)
	}
	reusableRepos, ok := rows[0]["reusable_workflow_repositories"].([]string)
	if !ok {
		t.Fatalf(
			"workflow_artifacts[0].reusable_workflow_repositories type = %T, want []string",
			rows[0]["reusable_workflow_repositories"],
		)
	}
	if len(reusableRepos) != 1 || reusableRepos[0] != "eshu-hq/delivery-legacy-automation" {
		t.Fatalf("workflow_artifacts[0].reusable_workflow_repositories = %#v, want delivery repo", reusableRepos)
	}
}

func TestRelationshipPlatformWorkerWorkflowSurfacesGatingAndNeeds(t *testing.T) {
	t.Parallel()

	artifacts := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-gated.yml",
			ArtifactType: "github_actions_workflow",
			Content: readRelationshipPlatformFixture(
				t,
				"service-worker-jobs",
				".github",
				"workflows",
				"deploy-gated.yml",
			),
		},
	})
	if artifacts == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows, ok := artifacts["workflow_artifacts"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow_artifacts type = %T, want []map[string]any", artifacts["workflow_artifacts"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["command_count"], 2; got != want {
		t.Fatalf("workflow_artifacts[0].command_count = %#v, want %#v", got, want)
	}

	gatingConditions, ok := rows[0]["gating_conditions"].([]string)
	if !ok {
		t.Fatalf("workflow_artifacts[0].gating_conditions type = %T, want []string", rows[0]["gating_conditions"])
	}
	wantGatingConditions := []string{
		"job deploy if ${{ github.ref == 'refs/heads/main' }}",
		"step deploy/Deploy gated worker if ${{ inputs.deploy_enabled == 'true' }}",
	}
	if len(gatingConditions) != len(wantGatingConditions) {
		t.Fatalf("len(workflow_artifacts[0].gating_conditions) = %d, want %d", len(gatingConditions), len(wantGatingConditions))
	}
	for i, want := range wantGatingConditions {
		if got := gatingConditions[i]; got != want {
			t.Fatalf("workflow_artifacts[0].gating_conditions[%d] = %q, want %q", i, got, want)
		}
	}

	needsDependencies, ok := rows[0]["needs_dependencies"].([]string)
	if !ok {
		t.Fatalf("workflow_artifacts[0].needs_dependencies type = %T, want []string", rows[0]["needs_dependencies"])
	}
	if len(needsDependencies) != 1 || needsDependencies[0] != "deploy<-verify" {
		t.Fatalf("workflow_artifacts[0].needs_dependencies = %#v, want deploy<-verify", needsDependencies)
	}

	overview := BuildRepositoryDeploymentOverview(
		[]string{"service-worker-jobs"},
		nil,
		[]string{"github_actions"},
		map[string]any{"deployment_artifacts": artifacts},
	)

	deliveryPaths, ok := overview["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", overview["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := deliveryPaths[0]["command_count"], 2; got != want {
		t.Fatalf("delivery_paths[0].command_count = %#v, want %#v", got, want)
	}

	topologyStory, ok := overview["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", overview["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy-gated.yml as github_actions_workflow deploy-gated triggered by workflow_dispatch with 2 run command(s), gating conditions job deploy if ${{ github.ref == 'refs/heads/main' }}; step deploy/Deploy gated worker if ${{ inputs.deploy_enabled == 'true' }}, and needs deploy<-verify (workflow_file, run_commands, gating_conditions, job_dependencies, workflow_triggers)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}

	story := buildRepositoryStoryResponse(
		RepoRef{ID: "repository:service-worker-jobs", Name: "service-worker-jobs"},
		6,
		[]string{"yaml"},
		nil,
		nil,
		0,
		map[string]any{
			"families":             []string{"github_actions"},
			"deployment_artifacts": artifacts,
		},
		nil,
	)
	deploymentOverview, ok := story["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", story["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 1 {
		t.Fatalf("len(direct_story) = %d, want 1", len(directStory))
	}
	if got, want := directStory[0], "Workflow delivery paths include .github/workflows/deploy-gated.yml as github_actions_workflow deploy-gated triggered by workflow_dispatch with 2 run command(s), gating conditions job deploy if ${{ github.ref == 'refs/heads/main' }}; step deploy/Deploy gated worker if ${{ inputs.deploy_enabled == 'true' }}, and needs deploy<-verify (workflow_file, run_commands, gating_conditions, job_dependencies, workflow_triggers)."; got != want {
		t.Fatalf("direct_story[0] = %q, want %q", got, want)
	}
}

func TestRelationshipPlatformWorkerDockerfileSurfacesRuntimeStory(t *testing.T) {
	t.Parallel()

	artifacts := buildRepositoryRuntimeArtifacts([]FileContent{
		{
			RelativePath: "Dockerfile",
			ArtifactType: "dockerfile",
			Content: readRelationshipPlatformFixture(
				t,
				"service-worker-jobs",
				"Dockerfile",
			),
		},
	})
	if artifacts == nil {
		t.Fatal("buildRepositoryRuntimeArtifacts() = nil, want deployment artifacts")
	}

	overview := BuildRepositoryDeploymentOverview(
		[]string{"service-worker-jobs"},
		nil,
		[]string{"docker"},
		map[string]any{"deployment_artifacts": artifacts},
	)

	deliveryPaths, ok := overview["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", overview["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := deliveryPaths[0]["artifact_name"], "node"; got != want {
		t.Fatalf("delivery_paths[0].artifact_name = %#v, want %#v", got, want)
	}
	if got, want := deliveryPaths[0]["cmd"], `["node", "service-worker-jobs.js"]`; got != want {
		t.Fatalf("delivery_paths[0].cmd = %#v, want %#v", got, want)
	}

	topologyStory, ok := overview["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", overview["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Runtime artifacts include dockerfile stage node in Dockerfile based on node with cmd [\"node\", \"service-worker-jobs.js\"] (base_image, cmd)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}

	story := buildRepositoryStoryResponse(
		RepoRef{ID: "repository:service-worker-jobs", Name: "service-worker-jobs"},
		4,
		[]string{"dockerfile"},
		nil,
		nil,
		0,
		map[string]any{
			"families":             []string{"docker"},
			"deployment_artifacts": artifacts,
		},
		nil,
	)
	deploymentOverview, ok := story["deployment_overview"].(map[string]any)
	if !ok {
		t.Fatalf("deployment_overview type = %T, want map[string]any", story["deployment_overview"])
	}
	directStory, ok := deploymentOverview["direct_story"].([]string)
	if !ok {
		t.Fatalf("direct_story type = %T, want []string", deploymentOverview["direct_story"])
	}
	if len(directStory) != 1 {
		t.Fatalf("len(direct_story) = %d, want 1", len(directStory))
	}
	if got, want := directStory[0], "Runtime artifacts include dockerfile stage node in Dockerfile based on node with cmd [\"node\", \"service-worker-jobs.js\"] (base_image, cmd)."; got != want {
		t.Fatalf("direct_story[0] = %q, want %q", got, want)
	}
}

func TestRelationshipPlatformServiceEdgeSynthesizesMixedDeliveryFamilies(t *testing.T) {
	t.Parallel()

	controllerArtifacts := buildRepositoryControllerArtifacts("service-edge-api", []FileContent{
		{
			RelativePath: "Jenkinsfile",
			ArtifactType: "groovy",
			Content:      readRelationshipPlatformFixture(t, "service-edge-api", "Jenkinsfile"),
		},
	})
	runtimeArtifacts := buildRepositoryRuntimeArtifacts([]FileContent{
		{
			RelativePath: "docker-compose.yaml",
			ArtifactType: "docker_compose",
			Content:      readRelationshipPlatformFixture(t, "service-edge-api", "docker-compose.yaml"),
		},
	})
	cloudFormationArtifacts := map[string]any{
		"deployment_artifacts": []map[string]any{
			{
				"relative_path": "infra/api-service.yaml",
				"artifact_type": "cloudformation_serverless",
				"artifact_name": "service-edge-api",
			},
		},
	}
	deploymentArtifacts := mergeDeploymentArtifactMaps(controllerArtifacts, runtimeArtifacts)
	deploymentArtifacts = mergeDeploymentArtifactMaps(deploymentArtifacts, cloudFormationArtifacts)
	if deploymentArtifacts == nil {
		t.Fatal("deploymentArtifacts = nil, want mixed delivery artifacts")
	}

	overview := BuildRepositoryDeploymentOverview(
		[]string{"service-edge-api"},
		[]string{"ecs", "kubernetes"},
		[]string{"argocd", "cloudformation", "docker_compose", "jenkins", "terraform"},
		map[string]any{
			"deployment_artifacts": deploymentArtifacts,
			"relationship_overview": map[string]any{
				"controller_driven": []map[string]any{
					{
						"type":          "DEPLOYS_FROM",
						"target_name":   "deployment-kustomize",
						"target_id":     "repo-deployment-kustomize",
						"evidence_type": "argocd_applicationset_discovery",
					},
				},
			},
		},
	)

	deliveryFamilyPaths, ok := overview["delivery_family_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_family_paths type = %T, want []map[string]any", overview["delivery_family_paths"])
	}
	if len(deliveryFamilyPaths) != 4 {
		t.Fatalf("len(delivery_family_paths) = %d, want 4", len(deliveryFamilyPaths))
	}
	for _, family := range []string{"cloudformation", "docker_compose", "gitops", "jenkins"} {
		requireRelationshipPlatformDeliveryFamily(t, deliveryFamilyPaths, family)
	}

	deliveryFamilyStory, ok := overview["delivery_family_story"].([]string)
	if !ok {
		t.Fatalf("delivery_family_story type = %T, want []string", overview["delivery_family_story"])
	}
	wantStory := []string{
		"CloudFormation serverless delivery is evidenced by infra/api-service.yaml via cloudformation_serverless.",
		"Docker Compose runtime evidence is present via docker-compose.yaml for service edge-api; treat it as development/runtime evidence unless stronger production deployment proof exists.",
		"GitOps delivery is evidenced by DEPLOYS_FROM deployment-kustomize via argocd_applicationset_discovery.",
		"Jenkins delivery is evidenced by Jenkinsfile via jenkins_pipeline.",
	}
	if len(deliveryFamilyStory) != len(wantStory) {
		t.Fatalf("len(delivery_family_story) = %d, want %d", len(deliveryFamilyStory), len(wantStory))
	}
	for index, want := range wantStory {
		if got := deliveryFamilyStory[index]; got != want {
			t.Fatalf("delivery_family_story[%d] = %q, want %q", index, got, want)
		}
	}
}

func requireRelationshipPlatformDeliveryFamily(t *testing.T, rows []map[string]any, family string) {
	t.Helper()

	for _, row := range rows {
		if StringVal(row, "family") == family {
			return
		}
	}
	t.Fatalf("delivery_family_paths = %#v, want family %q", rows, family)
}

func readRelationshipPlatformFixture(t *testing.T, parts ...string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	allParts := append(
		[]string{filepath.Dir(file), "..", "..", "..", "tests", "fixtures", "relationship_platform"},
		parts...,
	)
	body, err := os.ReadFile(filepath.Join(allParts...))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", filepath.Join(allParts...), err)
	}
	return string(body)
}
