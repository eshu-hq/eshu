package query

import "testing"

func TestBuildRepositoryWorkflowArtifactsClassifiesDeliveryCommandFamilies(t *testing.T) {
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
      - name: Terraform
        run: terraform -chdir=terraform/environments/prod apply -auto-approve
      - name: Terragrunt
        run: terragrunt run-all apply --terragrunt-non-interactive
      - name: Helm
        run: helm upgrade --install edge-api ./charts/edge-api
      - name: Kubectl
        run: kubectl apply -f deploy/prod.yaml
      - name: Ansible
        run: ansible-playbook deploy/site.yml -i inventory/prod.ini
      - name: Compose
        run: docker compose -f docker-compose.yaml up -d --build
      - name: Docker
        run: docker build -t ghcr.io/example/edge-api:latest .
      - name: Argo CD
        run: argocd app sync edge-api
`,
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

	row := rows[0]
	if got, want := StringSliceVal(row, "delivery_command_families"), []string{
		"ansible",
		"argocd",
		"docker",
		"docker_compose",
		"helm",
		"kubectl",
		"terraform",
		"terragrunt",
	}; len(got) != len(want) {
		t.Fatalf("delivery_command_families length = %d, want %d; values=%#v", len(got), len(want), got)
	} else {
		for index, wantValue := range want {
			if got[index] != wantValue {
				t.Fatalf("delivery_command_families[%d] = %q, want %q", index, got[index], wantValue)
			}
		}
	}
	if got, want := StringSliceVal(row, "signals"), []string{
		"workflow_file",
		"run_commands",
		"workflow_triggers",
		"delivery_command_families",
	}; len(got) < len(want) {
		t.Fatalf("signals = %#v, want at least %#v", got, want)
	}
}

func TestBuildRepositoryWorkflowArtifactsDoesNotPromoteActionRepositoriesAsDeliveryCommands(t *testing.T) {
	t.Parallel()

	artifacts := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/setup-terraform.yml",
			ArtifactType: "github_actions_workflow",
			Content: `name: setup-terraform
on:
  workflow_dispatch:
jobs:
  setup:
    runs-on: ubuntu-latest
    steps:
      - uses: hashicorp/setup-terraform@v3
`,
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

	row := rows[0]
	if got := StringSliceVal(row, "action_repositories"); len(got) != 1 || got[0] != "hashicorp/setup-terraform" {
		t.Fatalf("action_repositories = %#v, want hashicorp/setup-terraform", got)
	}
	if got := StringSliceVal(row, "delivery_command_families"); len(got) != 0 {
		t.Fatalf("delivery_command_families = %#v, want none", got)
	}
}

func TestBuildRepositoryWorkflowArtifactsExtractsWorkflowLocalDeliveryPaths(t *testing.T) {
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
      - run: terragrunt run-all apply --terragrunt-working-dir infra/live/prod
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

	rows, ok := artifacts["workflow_artifacts"].([]map[string]any)
	if !ok {
		t.Fatalf("workflow_artifacts type = %T, want []map[string]any", artifacts["workflow_artifacts"])
	}
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "delivery_local_paths"), []string{
		"charts/edge-api",
		"deploy/docker-compose.yaml",
		"deploy/prod.yaml",
		"deploy/site.yml",
		"infra/live/prod",
		"inventory/prod.ini",
		"terraform/environments/prod",
		"vars/prod.yml",
	}; len(got) != len(want) {
		t.Fatalf("delivery_local_paths length = %d, want %d; values=%#v", len(got), len(want), got)
	} else {
		for index, wantValue := range want {
			if got[index] != wantValue {
				t.Fatalf("delivery_local_paths[%d] = %q, want %q", index, got[index], wantValue)
			}
		}
	}
}
