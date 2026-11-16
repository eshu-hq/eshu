// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fallback

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// nonGitHubActionsJobsYAML is a YAML file whose top-level shape (jobs → steps →
// uses) collides with a GitHub Actions workflow but which is not one — an
// internal CI config, a templated example, or a GitLab-style file. Before the
// path gate, githubActionsSourceRelationships decoded and walked this for every
// content entity, fabricating a github_actions_action_repository edge.
const nonGitHubActionsJobsYAML = `jobs:
  build:
    steps:
      - uses: some-org/some-action@v1
`

// relationshipHasTargetPort reports whether the built outgoing set holds an
// edge with the given type, reason, and target name. It mirrors the query
// root's relationshipHasTarget helper for this leaf's port-shaped set.
func relationshipHasTargetPort(set querycontract.ContentRelationshipSet, relationshipType, reason, target string) bool {
	for _, relationship := range set.Outgoing {
		if relationship["type"] == relationshipType && relationship["reason"] == reason && relationship["target_name"] == target {
			return true
		}
	}
	return false
}

// TestGitHubActionsSourceRelationshipsRejectsNonGitHubPath is the codex P1
// false-positive regression proof (issue #5337, PR #5379). A non-GitHub YAML
// whose content has a jobs/steps/uses shape must produce ZERO relationships
// when it does not live at a GitHub Actions artifact path, so it cannot
// fabricate github_actions_* edges (and, via
// buildOutgoingContentRelationships' first-classifier-wins short-circuit,
// cannot block later relationship classifiers from handling the entity).
func TestGitHubActionsSourceRelationshipsRejectsNonGitHubPath(t *testing.T) {
	t.Parallel()

	set, err := Builder{}.BuildContentRelationships(context.Background(), nil, querycontract.EntityContent{
		EntityID:     "not-a-workflow",
		RepoID:       "repo-1",
		RelativePath: "config/ci.yml",
		EntityType:   "File",
		EntityName:   "ci",
		Language:     "yaml",
		SourceCache:  nonGitHubActionsJobsYAML,
	}, nil)
	if err != nil {
		t.Fatalf("BuildContentRelationships() error = %v, want nil", err)
	}

	if len(set.Outgoing) != 0 {
		t.Fatalf("len(set.Outgoing) = %d, want 0 (config/ci.yml is not a GitHub Actions artifact path): %#v", len(set.Outgoing), set.Outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsRejectsGitLabCIPath is a second negative:
// a .gitlab-ci.yml with the same jobs/steps/uses shape is not a GitHub Actions
// artifact and must produce no github_actions_* edges.
func TestGitHubActionsSourceRelationshipsRejectsGitLabCIPath(t *testing.T) {
	t.Parallel()

	set, err := Builder{}.BuildContentRelationships(context.Background(), nil, querycontract.EntityContent{
		EntityID:     "gitlab-ci",
		RepoID:       "repo-1",
		RelativePath: ".gitlab-ci.yml",
		EntityType:   "File",
		EntityName:   "gitlab-ci",
		Language:     "yaml",
		SourceCache:  nonGitHubActionsJobsYAML,
	}, nil)
	if err != nil {
		t.Fatalf("BuildContentRelationships() error = %v, want nil", err)
	}

	if len(set.Outgoing) != 0 {
		t.Fatalf("len(set.Outgoing) = %d, want 0 (.gitlab-ci.yml is not a GitHub Actions artifact path): %#v", len(set.Outgoing), set.Outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsAcceptsWorkflowPath proves the exact same
// jobs/steps/uses content DOES still surface its DEPENDS_ON edge when it lives
// at a real workflow path — the path gate rejects only non-artifacts, not the
// signal.
func TestGitHubActionsSourceRelationshipsAcceptsWorkflowPath(t *testing.T) {
	t.Parallel()

	set, err := Builder{}.BuildContentRelationships(context.Background(), nil, querycontract.EntityContent{
		EntityID:     "real-workflow",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/ci.yml",
		EntityType:   "File",
		EntityName:   "ci",
		Language:     "yaml",
		SourceCache:  nonGitHubActionsJobsYAML,
	}, nil)
	if err != nil {
		t.Fatalf("BuildContentRelationships() error = %v, want nil", err)
	}

	if !relationshipHasTargetPort(set, "DEPENDS_ON", "github_actions_action_repository", "some-org/some-action") {
		t.Fatalf("missing DEPENDS_ON some-org/some-action for .github/workflows/ci.yml: %#v", set.Outgoing)
	}
}

// TestGitHubActionsSourceRelationshipsAcceptsCompositeActionPath proves a
// composite action metadata file (basename action.yml, outside
// .github/workflows) passes the path gate and still surfaces its step
// dependency through the runs.steps walk.
func TestGitHubActionsSourceRelationshipsAcceptsCompositeActionPath(t *testing.T) {
	t.Parallel()

	set, err := Builder{}.BuildContentRelationships(context.Background(), nil, querycontract.EntityContent{
		EntityID:     "composite-action",
		RepoID:       "repo-1",
		RelativePath: "my-action/action.yml",
		EntityType:   "File",
		EntityName:   "action",
		Language:     "yaml",
		SourceCache: `name: My action
runs:
  using: composite
  steps:
    - uses: some-org/x@v1
`,
	}, nil)
	if err != nil {
		t.Fatalf("BuildContentRelationships() error = %v, want nil", err)
	}

	if !relationshipHasTargetPort(set, "DEPENDS_ON", "github_actions_action_repository", "some-org/x") {
		t.Fatalf("missing composite-action DEPENDS_ON some-org/x for my-action/action.yml: %#v", set.Outgoing)
	}
}

func TestGitHubActionsSourceRelationshipsRejectsInexactWorkflowPaths(t *testing.T) {
	t.Parallel()

	for _, relativePath := range []string{
		".github/workflows/team/ci.yml",
		"examples/.github/workflows/ci.yml",
		".github/workflows/ci.json",
		".github/workflows/.yml",
		".github/workflows/.yaml",
		".github/workflows.yml",
	} {
		relativePath := relativePath
		t.Run(relativePath, func(t *testing.T) {
			t.Parallel()
			if isGitHubActionsArtifactPath(querycontract.EntityContent{RelativePath: relativePath}) {
				t.Fatalf("isGitHubActionsArtifactPath(%q) = true, want false", relativePath)
			}
		})
	}
}

func TestGitHubActionsSourceRelationshipsMultiJobOrderIsRepeatable(t *testing.T) {
	t.Parallel()

	entity := querycontract.EntityContent{
		RelativePath: ".github/workflows/ci.yml",
		SourceCache: `jobs:
  zeta:
    steps:
      - uses: z-org/z-action@v1
  alpha:
    steps:
      - uses: a-org/a-action@v1
  middle:
    steps:
      - uses: m-org/m-action@v1
`,
	}
	want := githubActionsSourceRelationships(entity)
	for run := 0; run < 25; run++ {
		if got := githubActionsSourceRelationships(entity); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d relationships = %#v, want stable %#v", run, got, want)
		}
	}
	if got, wantTarget := want[0].targetName, "a-org/a-action"; got != wantTarget {
		t.Fatalf("first target = %q, want deterministic job-name order %q", got, wantTarget)
	}
}
