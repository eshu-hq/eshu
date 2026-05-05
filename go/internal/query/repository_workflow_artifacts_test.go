package query

import (
	"context"
	"database/sql/driver"
	"testing"
)

func TestLoadRepositoryWorkflowArtifactsFallsBackToGetFileContent(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", ".github/workflows/deploy-gated.yml", "abc123", "",
					"hash-workflow", int64(16), "yaml", "github_actions_workflow",
				},
			},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-1", ".github/workflows/deploy-gated.yml", "abc123", `name: deploy-gated

on:
  workflow_dispatch:

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - name: Verify worker
        run: echo "verifying service-worker-jobs"
  deploy:
    needs: verify
    if: ${{ github.ref == 'refs/heads/main' }}
    runs-on: ubuntu-latest
    steps:
      - name: Deploy gated worker
        if: ${{ inputs.deploy_enabled == 'true' }}
        run: echo "deploying gated service-worker-jobs"
`,
					"hash-workflow", int64(16), "yaml", "github_actions_workflow",
				},
			},
		},
	})

	reader := NewContentReader(db)
	got, err := loadRepositoryWorkflowArtifacts(
		context.Background(),
		reader,
		"repo-1",
		nil,
	)
	if err != nil {
		t.Fatalf("loadRepositoryWorkflowArtifacts() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("loadRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := row["workflow_name"], "deploy-gated"; got != want {
		t.Fatalf("workflow_artifacts[0].workflow_name = %#v, want %#v", got, want)
	}
	if got, want := row["command_count"], 2; got != want {
		t.Fatalf("workflow_artifacts[0].command_count = %#v, want %#v", got, want)
	}
	gatingConditions := StringSliceVal(row, "gating_conditions")
	if len(gatingConditions) != 2 {
		t.Fatalf("len(workflow_artifacts[0].gating_conditions) = %d, want 2", len(gatingConditions))
	}
	if gatingConditions[0] != "job deploy if ${{ github.ref == 'refs/heads/main' }}" {
		t.Fatalf("workflow_artifacts[0].gating_conditions[0] = %q, want job condition", gatingConditions[0])
	}
	needsDependencies := StringSliceVal(row, "needs_dependencies")
	if len(needsDependencies) != 1 || needsDependencies[0] != "deploy<-verify" {
		t.Fatalf("workflow_artifacts[0].needs_dependencies = %#v, want [deploy<-verify]", needsDependencies)
	}
}

func TestBuildRepositoryWorkflowArtifactsIncludesWorkflowInputRepositories(t *testing.T) {
	t.Parallel()

	got := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/dispatch.yaml",
			ArtifactType: "github_actions_workflow",
			Content: `name: Dispatch
jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      workflow_input_repository: example-org/shared-automation
      automation-repo: example-org/automation-fallback
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "reusable_workflow_repositories"), []string{"example-org/shared-automation"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("workflow_artifacts[0].reusable_workflow_repositories = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "workflow_input_repositories"), []string{"example-org/automation-fallback", "example-org/shared-automation"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].workflow_input_repositories = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryWorkflowArtifactsIncludesWorkflowInputRepositoriesListForm(t *testing.T) {
	t.Parallel()

	got := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/dispatch.yaml",
			ArtifactType: "github_actions_workflow",
			Content: `name: Dispatch
jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      workflow_input_repositories:
        - example-org/shared-automation
        - example-org/automation-fallback
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "workflow_input_repositories"), []string{"example-org/automation-fallback", "example-org/shared-automation"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].workflow_input_repositories = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryWorkflowArtifactsIncludesCheckoutRepositories(t *testing.T) {
	t.Parallel()

	got := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy.yaml",
			ArtifactType: "github_actions_workflow",
			Content: `name: Deploy
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          repository: example-org/deployment-kustomize
      - uses: actions/checkout@v4
        with:
          repository: example-org/deployment-helm
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "checkout_repositories"), []string{"example-org/deployment-helm", "example-org/deployment-kustomize"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].checkout_repositories = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryWorkflowArtifactsIncludesActionRepositories(t *testing.T) {
	t.Parallel()

	got := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/update-providers.yml",
			ArtifactType: "github_actions_workflow",
			Content: `name: Update providers
jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - uses: peter-evans/create-pull-request@v5
      - uses: ./.github/actions/local-helper
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "action_repositories"), []string{"hashicorp/setup-terraform", "peter-evans/create-pull-request"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].action_repositories = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "signals"), []string{"workflow_file", "action_repositories"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].signals = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryWorkflowArtifactsIncludesWorkflowTriggerAndMatrixMetadata(t *testing.T) {
	t.Parallel()

	got := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-matrix.yaml",
			ArtifactType: "github_actions_workflow",
			Content: `name: Deploy Matrix
on:
  workflow_dispatch:
    inputs:
      deploy_enabled:
        required: true
  push:
    branches:
      - main
jobs:
  deploy:
    strategy:
      matrix:
        runtime: [node18, node20]
        region: [us-east-1, us-west-2]
    runs-on: ubuntu-latest
    steps:
      - run: echo "deploy"
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "trigger_events"), []string{"push", "workflow_dispatch"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].trigger_events = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "workflow_inputs"), []string{"deploy_enabled"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("workflow_artifacts[0].workflow_inputs = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "matrix_keys"), []string{"region", "runtime"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].matrix_keys = %#v, want %#v", got, want)
	}
	if got, want := row["matrix_combination_count"], 4; got != want {
		t.Fatalf("workflow_artifacts[0].matrix_combination_count = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryWorkflowArtifactsIncludesWorkflowGovernanceMetadata(t *testing.T) {
	t.Parallel()

	got := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-governed.yaml",
			ArtifactType: "github_actions_workflow",
			Content: `name: Deploy Governed
on:
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
concurrency:
  group: deploy-${{ github.ref }}
  cancel-in-progress: true
jobs:
  deploy:
    permissions:
      deployments: write
    concurrency: deploy-production
    environment:
      name: production
      url: https://deployments.example.com
    timeout-minutes: 30
    runs-on: ubuntu-latest
    steps:
      - run: echo "deploy"
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "permission_scopes"), []string{"contents:read", "deployments:write", "id-token:write"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("workflow_artifacts[0].permission_scopes = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "concurrency_groups"), []string{"deploy-${{ github.ref }}", "deploy-production"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].concurrency_groups = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "environments"), []string{"production"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("workflow_artifacts[0].environments = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "job_timeout_minutes"), []string{"deploy:30"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("workflow_artifacts[0].job_timeout_minutes = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "signals"), []string{
		"workflow_file",
		"run_commands",
		"workflow_triggers",
		"workflow_permissions",
		"workflow_concurrency",
		"workflow_environments",
		"workflow_timeouts",
	}; len(got) != len(want) ||
		got[0] != want[0] ||
		got[1] != want[1] ||
		got[2] != want[2] ||
		got[3] != want[3] ||
		got[4] != want[4] ||
		got[5] != want[5] ||
		got[6] != want[6] {
		t.Fatalf("workflow_artifacts[0].signals = %#v, want %#v", got, want)
	}
}

func TestBuildRepositoryWorkflowArtifactsIncludesLocalReusableWorkflowPaths(t *testing.T) {
	t.Parallel()

	got := buildRepositoryWorkflowArtifacts([]FileContent{
		{
			RelativePath: ".github/workflows/deploy-local.yaml",
			ArtifactType: "github_actions_workflow",
			Content: `name: Deploy Local
jobs:
  local-release:
    uses: ./.github/workflows/release.yaml
  local-verify:
    uses: ./.github/workflows/verify.yaml@main
`,
		},
	})
	if got == nil {
		t.Fatal("buildRepositoryWorkflowArtifacts() = nil, want workflow_artifacts")
	}

	rows := mapSliceValue(got, "workflow_artifacts")
	if len(rows) != 1 {
		t.Fatalf("len(workflow_artifacts) = %d, want 1", len(rows))
	}

	row := rows[0]
	if got, want := StringSliceVal(row, "local_reusable_workflow_paths"), []string{".github/workflows/release.yaml", ".github/workflows/verify.yaml"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].local_reusable_workflow_paths = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(row, "signals"), []string{"workflow_file", "local_reusable_workflow_refs"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("workflow_artifacts[0].signals = %#v, want %#v", got, want)
	}
}
