// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

// relationshipReasonCount counts outgoing relationships in the set that carry
// the given (type, reason) pair, so signal-preservation assertions do not
// depend on ordering across the merged source+metadata builders.
func relationshipReasonCount(relationships contentRelationshipSet, relationshipType, reason string) int {
	count := 0
	for _, relationship := range relationships.outgoing {
		if relationship["type"] == relationshipType && relationship["reason"] == reason {
			count++
		}
	}
	return count
}

// relationshipHasTarget reports whether the set contains an outgoing
// relationship matching (type, reason, target).
func relationshipHasTarget(relationships contentRelationshipSet, relationshipType, reason, target string) bool {
	for _, relationship := range relationships.outgoing {
		if relationship["type"] == relationshipType &&
			relationship["reason"] == reason &&
			relationship["target_name"] == target {
			return true
		}
	}
	return false
}

// TestGitHubActionsSourceRelationshipsIgnoresUsesInsideRunBlock is the issue
// #5337 Detector 4 false-positive regression proof: the deleted raw-text line
// scanner treated any physical line beginning with "uses:" as a real step key,
// so a `uses:` line that appears inside a `run: |` block scalar (here a heredoc
// writing an example workflow file) fabricated a DEPENDS_ON edge. The
// structured YAML decode sees the block scalar as an opaque string, so no edge
// is produced.
func TestGitHubActionsSourceRelationshipsIgnoresUsesInsideRunBlock(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-run-block",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/generate.yaml",
		EntityType:   "File",
		EntityName:   "generate",
		Language:     "yaml",
		SourceCache: `jobs:
  generate:
    runs-on: ubuntu-latest
    steps:
      - name: Write an example workflow file
        run: |
          cat <<'EOF' > example-workflow.yaml
          jobs:
            example:
              steps:
                - uses: octocat/example-action@v1
                - uses: actions/checkout@v4
                  with:
                    repository: octocat/some-config
          EOF
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 0 {
		t.Fatalf("len(relationships.outgoing) = %d, want 0 (all uses:/repository: lines are inside a run: block scalar): %#v", len(relationships.outgoing), relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsPreservesStepActionDependency proves the
// DEPENDS_ON signal survives the scanner-to-structured migration: a real
// step-level `uses:` action reference still produces a DEPENDS_ON edge, while
// actions/checkout@ and local (./…) action references stay excluded.
func TestGitHubActionsSourceRelationshipsPreservesStepActionDependency(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-actions",
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
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if got, want := relationshipReasonCount(relationships, "DEPENDS_ON", "github_actions_action_repository"), 2; got != want {
		t.Fatalf("DEPENDS_ON action-repository edge count = %d, want %d: %#v", got, want, relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "hashicorp/setup-terraform") {
		t.Fatalf("missing DEPENDS_ON hashicorp/setup-terraform: %#v", relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "peter-evans/create-pull-request") {
		t.Fatalf("missing DEPENDS_ON peter-evans/create-pull-request: %#v", relationships.outgoing)
	}
	// actions/checkout@ and the local ./ action must not become DEPENDS_ON edges.
	if relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "actions/checkout") {
		t.Fatalf("actions/checkout must be excluded from DEPENDS_ON: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsPreservesRemoteReusableWorkflow proves
// the DEPLOYS_FROM signal for a job-level remote reusable workflow ref
// survives.
func TestGitHubActionsSourceRelationshipsPreservesRemoteReusableWorkflow(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-reusable",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `jobs:
  deploy:
    uses: myorg/deployment-helm/.github/workflows/deploy.yaml@main
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DEPLOYS_FROM", "github_actions_reusable_workflow_ref", "myorg/deployment-helm") {
		t.Fatalf("missing DEPLOYS_FROM myorg/deployment-helm: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsPreservesLocalReusableWorkflow proves the
// DEPLOYS_FROM signal for a job-level local (./.github/workflows/…) reusable
// workflow path survives.
func TestGitHubActionsSourceRelationshipsPreservesLocalReusableWorkflow(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-local-reusable",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `jobs:
  reusable:
    uses: ./.github/workflows/release.yaml
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DEPLOYS_FROM", "github_actions_local_reusable_workflow_ref", ".github/workflows/release.yaml") {
		t.Fatalf("missing DEPLOYS_FROM .github/workflows/release.yaml: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsPreservesCheckoutRepository proves the
// DISCOVERS_CONFIG_IN signal for an actions/checkout step with an explicit
// with.repository survives.
func TestGitHubActionsSourceRelationshipsPreservesCheckoutRepository(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-checkout",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `jobs:
  checkout:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          repository: myorg/deployment-kustomize
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DISCOVERS_CONFIG_IN", "github_actions_checkout_repository", "myorg/deployment-kustomize") {
		t.Fatalf("missing DISCOVERS_CONFIG_IN checkout myorg/deployment-kustomize: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsPreservesStepLevelWorkflowInputRepository
// is the parity-trap proof: the old scanner picked up a workflow-input
// repository key from a deeper-indented step-level `with:`, not only a
// job-level `with:`. The structured walk must scan step-level `with:` too or
// this real DISCOVERS_CONFIG_IN signal is lost.
func TestGitHubActionsSourceRelationshipsPreservesStepLevelWorkflowInputRepository(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-step-with",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/pr-command-dispatch.yml",
		EntityType:   "File",
		EntityName:   "pr-command-dispatch",
		Language:     "yaml",
		SourceCache: `jobs:
  dispatch:
    runs-on: ubuntu-latest
    steps:
      - name: Dispatch
        uses: some-org/dispatcher@v1
        with:
          workflow_input_repository: example-org/shared-automation
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DISCOVERS_CONFIG_IN", "github_actions_workflow_input_repository", "example-org/shared-automation") {
		t.Fatalf("missing step-level with: DISCOVERS_CONFIG_IN example-org/shared-automation: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsPreservesJobLevelWorkflowInputRepository
// proves the job-level `with:` and job-map input-repository keys still surface
// a DISCOVERS_CONFIG_IN edge alongside the reusable-workflow DEPLOYS_FROM.
func TestGitHubActionsSourceRelationshipsPreservesJobLevelWorkflowInputRepository(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-job-with",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/pr-command-dispatch.yml",
		EntityType:   "File",
		EntityName:   "pr-command-dispatch",
		Language:     "yaml",
		SourceCache: `jobs:
  dispatch-command:
    uses: example-org/shared-automation/.github/workflows/node-api-command-processing.yml@v2
    with:
      automation-repo: example-org/shared-automation
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DEPLOYS_FROM", "github_actions_reusable_workflow_ref", "example-org/shared-automation") {
		t.Fatalf("missing DEPLOYS_FROM example-org/shared-automation: %#v", relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DISCOVERS_CONFIG_IN", "github_actions_workflow_input_repository", "example-org/shared-automation") {
		t.Fatalf("missing job with: DISCOVERS_CONFIG_IN example-org/shared-automation: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsWalksCompositeActionSteps proves a
// composite action file (top-level runs.steps, no jobs) still surfaces its
// step action dependencies. workflowArtifactDetails never modeled this shape,
// so a jobs-only structured walk would have silently regressed composite
// actions the old substring gate let through.
func TestGitHubActionsSourceRelationshipsWalksCompositeActionSteps(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-composite",
		RepoID:       "repo-1",
		RelativePath: ".github/actions/build/action.yml",
		EntityType:   "File",
		EntityName:   "build",
		Language:     "yaml",
		SourceCache: `name: Build
runs:
  using: composite
  steps:
    - uses: actions/checkout@v4
      with:
        repository: myorg/shared-config
    - uses: hashicorp/setup-terraform@v3
    - run: echo build
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "hashicorp/setup-terraform") {
		t.Fatalf("missing composite DEPENDS_ON hashicorp/setup-terraform: %#v", relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DISCOVERS_CONFIG_IN", "github_actions_checkout_repository", "myorg/shared-config") {
		t.Fatalf("missing composite DISCOVERS_CONFIG_IN myorg/shared-config: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsSkipsExpressionRefs proves a `${{ … }}`
// expression as a uses: value (unresolvable to a concrete repository here) is
// skipped rather than parsed into a garbage target.
func TestGitHubActionsSourceRelationshipsSkipsExpressionRefs(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-expression",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ${{ matrix.action }}/build@v1
      - uses: hashicorp/setup-terraform@v3
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if got, want := relationshipReasonCount(relationships, "DEPENDS_ON", "github_actions_action_repository"), 1; got != want {
		t.Fatalf("DEPENDS_ON count = %d, want %d (the ${{ }} ref must be skipped): %#v", got, want, relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "hashicorp/setup-terraform") {
		t.Fatalf("missing DEPENDS_ON hashicorp/setup-terraform: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsWalksMultipleDocuments proves refs are
// collected across every YAML document in a multi-document SourceCache.
func TestGitHubActionsSourceRelationshipsWalksMultipleDocuments(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-multidoc",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `jobs:
  a:
    steps:
      - uses: hashicorp/setup-terraform@v3
---
jobs:
  b:
    steps:
      - uses: peter-evans/create-pull-request@v5
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "hashicorp/setup-terraform") {
		t.Fatalf("missing document-1 DEPENDS_ON hashicorp/setup-terraform: %#v", relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "peter-evans/create-pull-request") {
		t.Fatalf("missing document-2 DEPENDS_ON peter-evans/create-pull-request: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsResolvesYAMLAnchors proves yaml.v3 anchor
// and alias expansion works through the structured decode, so a step reused via
// an alias still surfaces its dependency.
func TestGitHubActionsSourceRelationshipsResolvesYAMLAnchors(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-anchor",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache: `jobs:
  a:
    steps:
      - &checkout
        uses: hashicorp/setup-terraform@v3
  b:
    steps:
      - *checkout
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "hashicorp/setup-terraform") {
		t.Fatalf("missing anchor/alias DEPENDS_ON hashicorp/setup-terraform: %#v", relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsMalformedYAMLYieldsNoEdges proves
// malformed YAML returns no relationships (and does not panic): an unparseable
// workflow cannot run, so it declares no real dependency, and there is
// deliberately no fallback to line scanning.
func TestGitHubActionsSourceRelationshipsMalformedYAMLYieldsNoEdges(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-malformed",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/deploy.yaml",
		EntityType:   "File",
		EntityName:   "deploy",
		Language:     "yaml",
		SourceCache:  "jobs:\n  build:\n    steps:\n      - uses: hashicorp/setup-terraform@v3\n    invalid: [unterminated\n",
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 0 {
		t.Fatalf("len(relationships.outgoing) = %d, want 0 for malformed YAML (no line-scan fallback): %#v", len(relationships.outgoing), relationships.outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsProseWithCheckoutSubstringYieldsNoEdges
// documents the intentional behavior change from deleting the substring-gated
// line scanner: prose or docs content that merely mentions actions/checkout@
// (but does not decode to a workflow/action structure) now correctly yields no
// relationships instead of a text-derived guess.
func TestGitHubActionsSourceRelationshipsProseWithCheckoutSubstringYieldsNoEdges(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-prose",
		RepoID:       "repo-1",
		RelativePath: "docs/ci.md",
		EntityType:   "File",
		EntityName:   "ci",
		Language:     "markdown",
		SourceCache:  "To check out code, add a step using actions/checkout@v4 in your workflow.\n",
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 0 {
		t.Fatalf("len(relationships.outgoing) = %d, want 0 for prose mentioning actions/checkout@: %#v", len(relationships.outgoing), relationships.outgoing)
	}
}
