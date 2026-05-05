package query

import (
	"context"
	"testing"
)

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesExplicitRefsFromSource(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-1",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `name: Deploy
jobs:
  deploy:
    uses: myorg/deployment-helm/.github/workflows/deploy.yaml@main
  local:
    uses: ./.github/workflows/local.yaml
  checkout:
    steps:
      - uses: actions/checkout@v4
        with:
          repository: myorg/deployment-kustomize
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 3 {
		t.Fatalf("len(relationships.outgoing) = %d, want 3", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "myorg/deployment-helm"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "github_actions_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}

	second := relationships.outgoing[1]
	if got, want := second["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], ".github/workflows/local.yaml"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_local_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}

	third := relationships.outgoing[2]
	if got, want := third["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[2][type] = %#v, want %#v", got, want)
	}
	if got, want := third["target_name"], "myorg/deployment-kustomize"; got != want {
		t.Fatalf("relationships.outgoing[2][target_name] = %#v, want %#v", got, want)
	}
	if got, want := third["reason"], "github_actions_checkout_repository"; got != want {
		t.Fatalf("relationships.outgoing[2][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesExplicitRefsFromMetadata(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-2",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/release.yaml",
		EntityType:   "File",
		EntityName:   "release",
		Metadata: map[string]any{
			"workflow_refs": []any{
				"myorg/deployment-helm/.github/workflows/release.yaml@main",
				"./.github/workflows/internal.yaml@main",
			},
			"checkout_repository": "myorg/deployment-kustomize",
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 3 {
		t.Fatalf("len(relationships.outgoing) = %d, want 3", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "myorg/deployment-helm"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "github_actions_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}

	second := relationships.outgoing[1]
	if got, want := second["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], ".github/workflows/internal.yaml"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_local_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}

	third := relationships.outgoing[2]
	if got, want := third["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[2][type] = %#v, want %#v", got, want)
	}
	if got, want := third["target_name"], "myorg/deployment-kustomize"; got != want {
		t.Fatalf("relationships.outgoing[2][target_name] = %#v, want %#v", got, want)
	}
	if got, want := third["reason"], "github_actions_checkout_repository"; got != want {
		t.Fatalf("relationships.outgoing[2][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesAutomationRepository(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-3",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/pr-command-dispatch.yml",
		EntityType:   "File",
		EntityName:   "pr-command-dispatch",
		Language:     "yaml",
		SourceCache: `jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      automation-repo: 'example-org/shared-automation'
      automation-repo-ref: 'refs/tags/v2'
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2", len(relationships.outgoing))
	}

	second := relationships.outgoing[1]
	if got, want := second["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], "example-org/shared-automation"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_workflow_input_repository"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesWorkflowInputRepository(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-4",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/pr-command-dispatch.yml",
		EntityType:   "File",
		EntityName:   "pr-command-dispatch",
		Language:     "yaml",
		SourceCache: `jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      workflow_input_repository: 'example-org/shared-automation'
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2", len(relationships.outgoing))
	}

	second := relationships.outgoing[1]
	if got, want := second["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], "example-org/shared-automation"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_workflow_input_repository"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesWorkflowInputRepositoriesListFromSource(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-4c",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/pr-command-dispatch.yml",
		EntityType:   "File",
		EntityName:   "pr-command-dispatch",
		Language:     "yaml",
		SourceCache: `jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      workflow_input_repositories:
        - example-org/shared-automation
        - example-org/automation-fallback
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 3 {
		t.Fatalf("len(relationships.outgoing) = %d, want 3", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "example-org/shared-automation"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}

	second := relationships.outgoing[1]
	if got, want := second["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], "example-org/shared-automation"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_workflow_input_repository"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}

	third := relationships.outgoing[2]
	if got, want := third["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[2][type] = %#v, want %#v", got, want)
	}
	if got, want := third["target_name"], "example-org/automation-fallback"; got != want {
		t.Fatalf("relationships.outgoing[2][target_name] = %#v, want %#v", got, want)
	}
	if got, want := third["reason"], "github_actions_workflow_input_repository"; got != want {
		t.Fatalf("relationships.outgoing[2][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesWorkflowInputRepositoriesListMetadata(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-4b",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/pr-command-dispatch.yml",
		EntityType:   "File",
		EntityName:   "pr-command-dispatch",
		Metadata: map[string]any{
			"workflow_input_repositories": []any{
				"example-org/shared-automation",
				"example-org/automation-fallback",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "example-org/shared-automation"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "github_actions_workflow_input_repository"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}

	second := relationships.outgoing[1]
	if got, want := second["type"], "DISCOVERS_CONFIG_IN"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], "example-org/automation-fallback"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_workflow_input_repository"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesActionRepositories(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-5",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/update-providers.yml",
		EntityType:   "File",
		EntityName:   "update-providers",
		Language:     "yaml",
		SourceCache: `jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
      - uses: peter-evans/create-pull-request@v5
      - uses: ./.github/actions/local-helper
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DEPENDS_ON"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], "hashicorp/setup-terraform"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "github_actions_action_repository"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}

	second := relationships.outgoing[1]
	if got, want := second["type"], "DEPENDS_ON"; got != want {
		t.Fatalf("relationships.outgoing[1][type] = %#v, want %#v", got, want)
	}
	if got, want := second["target_name"], "peter-evans/create-pull-request"; got != want {
		t.Fatalf("relationships.outgoing[1][target_name] = %#v, want %#v", got, want)
	}
	if got, want := second["reason"], "github_actions_action_repository"; got != want {
		t.Fatalf("relationships.outgoing[1][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesLocalReusableWorkflowPathFromSource(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-6",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `jobs:
  reusable:
    uses: ./.github/workflows/release.yaml
`,
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], ".github/workflows/release.yaml"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "github_actions_local_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}
}

func TestBuildContentRelationshipSetGitHubActionsWorkflowPromotesLocalReusableWorkflowPathFromMetadata(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-workflow-7",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Metadata: map[string]any{
			"workflow_refs": []any{
				"./.github/workflows/release.yaml@main",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	first := relationships.outgoing[0]
	if got, want := first["type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationships.outgoing[0][type] = %#v, want %#v", got, want)
	}
	if got, want := first["target_name"], ".github/workflows/release.yaml"; got != want {
		t.Fatalf("relationships.outgoing[0][target_name] = %#v, want %#v", got, want)
	}
	if got, want := first["reason"], "github_actions_local_reusable_workflow_ref"; got != want {
		t.Fatalf("relationships.outgoing[0][reason] = %#v, want %#v", got, want)
	}
}

func TestGitHubActionsActionRepositoryRef(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "step action", in: "hashicorp/setup-terraform@v3", want: "hashicorp/setup-terraform"},
		{name: "checkout stays explicit", in: "actions/checkout@v4", want: ""},
		{name: "local action ignored", in: "./.github/actions/local-helper", want: ""},
		{name: "reusable workflow ignored", in: "myorg/deployment-helm/.github/workflows/deploy.yaml@main", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := githubActionsActionRepositoryRef(tc.in); got != tc.want {
				t.Fatalf("githubActionsActionRepositoryRef(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
