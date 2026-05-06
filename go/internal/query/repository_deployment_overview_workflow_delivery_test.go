package query

import "testing"

func TestBuildRepositoryDeploymentOverviewIncludesWorkflowDeliveryCommandFamilies(t *testing.T) {
	t.Parallel()

	artifacts := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-platform.yml",
			ArtifactType: "github_actions_workflow",
			Content: `name: deploy-platform
on:
  workflow_dispatch:
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: terraform -chdir=terraform/environments/prod apply -auto-approve
      - run: helm upgrade --install edge-api ./charts/edge-api
      - run: kubectl apply -f deploy/prod.yaml
      - run: docker compose -f docker-compose.yaml up -d --build
`,
		},
	})
	if artifacts == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	got := BuildRepositoryDeploymentOverview(
		[]string{"edge-api"},
		nil,
		[]string{"github_actions"},
		map[string]any{"deployment_artifacts": artifacts},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "delivery_command_families"), []string{
		"docker_compose",
		"helm",
		"kubectl",
		"terraform",
	}; len(got) != len(want) {
		t.Fatalf("delivery_paths[0].delivery_command_families length = %d, want %d; values=%#v", len(got), len(want), got)
	} else {
		for index, wantValue := range want {
			if got[index] != wantValue {
				t.Fatalf("delivery_paths[0].delivery_command_families[%d] = %q, want %q", index, got[index], wantValue)
			}
		}
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy-platform.yml as github_actions_workflow deploy-platform triggered by workflow_dispatch with 4 run command(s) using local paths charts/edge-api, deploy/prod.yaml, docker-compose.yaml, terraform/environments/prod and delivery families docker_compose, helm, kubectl, terraform (workflow_file, run_commands, workflow_triggers, delivery_command_families, delivery_local_paths)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}

func TestBuildRepositoryDeploymentOverviewIncludesWorkflowLocalDeliveryPaths(t *testing.T) {
	t.Parallel()

	artifacts := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-platform.yml",
			ArtifactType: "github_actions_workflow",
			Content: `name: deploy-platform
on:
  workflow_dispatch:
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: terraform -chdir=terraform/environments/prod apply -auto-approve
      - run: helm upgrade --install edge-api ./charts/edge-api
      - run: kubectl apply -f deploy/prod.yaml
      - run: ansible-playbook deploy/site.yml -i inventory/prod.ini --extra-vars @vars/prod.yml
      - run: docker compose -f deploy/docker-compose.yaml up -d --build
`,
		},
	})
	if artifacts == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	got := BuildRepositoryDeploymentOverview(
		[]string{"edge-api"},
		nil,
		[]string{"github_actions"},
		map[string]any{"deployment_artifacts": artifacts},
	)

	deliveryPaths, ok := got["delivery_paths"].([]map[string]any)
	if !ok {
		t.Fatalf("delivery_paths type = %T, want []map[string]any", got["delivery_paths"])
	}
	if len(deliveryPaths) != 1 {
		t.Fatalf("len(delivery_paths) = %d, want 1", len(deliveryPaths))
	}
	if got, want := StringSliceVal(deliveryPaths[0], "delivery_local_paths"), []string{
		"charts/edge-api",
		"deploy/docker-compose.yaml",
		"deploy/prod.yaml",
		"deploy/site.yml",
		"inventory/prod.ini",
		"terraform/environments/prod",
		"vars/prod.yml",
	}; len(got) != len(want) {
		t.Fatalf("delivery_paths[0].delivery_local_paths length = %d, want %d; values=%#v", len(got), len(want), got)
	} else {
		for index, wantValue := range want {
			if got[index] != wantValue {
				t.Fatalf("delivery_paths[0].delivery_local_paths[%d] = %q, want %q", index, got[index], wantValue)
			}
		}
	}

	topologyStory, ok := got["topology_story"].([]string)
	if !ok {
		t.Fatalf("topology_story type = %T, want []string", got["topology_story"])
	}
	if len(topologyStory) != 1 {
		t.Fatalf("len(topology_story) = %d, want 1", len(topologyStory))
	}
	if got, want := topologyStory[0], "Workflow delivery paths include .github/workflows/deploy-platform.yml as github_actions_workflow deploy-platform triggered by workflow_dispatch with 5 run command(s) using local paths charts/edge-api, deploy/docker-compose.yaml, deploy/prod.yaml, deploy/site.yml, inventory/prod.ini, terraform/environments/prod, vars/prod.yml and delivery families ansible, docker_compose, helm, kubectl, terraform (workflow_file, run_commands, workflow_triggers, delivery_command_families, delivery_local_paths)."; got != want {
		t.Fatalf("topology_story[0] = %q, want %q", got, want)
	}
}
