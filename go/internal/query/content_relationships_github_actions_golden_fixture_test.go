// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestGitHubActionsGoldenFixtureDiscriminatesRunBlockUses pins the
// github_actions_workflows golden-corpus fixture's positive-vs-foil
// discrimination for the #5337 Detector 4 fix, feeding the fixture's real
// .github/workflows/ci.yml through buildContentRelationshipSet (the same
// query-time content-relationship builder the get_entity_context surface uses).
// The genuine step-level `uses: hashicorp/setup-terraform@v3` must produce the
// DEPENDS_ON action-repository edge, while the `uses: octocat/example-action@v1`
// line that lives inside a `run: |` block scalar must produce none. This is the
// fixture-tier proof that the golden repo staged in
// scripts/verify-golden-corpus-gate.sh exercises the detector end to end.
func TestGitHubActionsGoldenFixtureDiscriminatesRunBlockUses(t *testing.T) {
	t.Parallel()

	// Locate the fixture from the query package directory: repo-root-relative
	// tests/fixtures/ecosystems/github_actions_workflows/.github/workflows/ci.yml.
	workflowPath := filepath.Join(
		"..", "..", "..",
		"tests", "fixtures", "ecosystems", "github_actions_workflows",
		".github", "workflows", "ci.yml",
	)
	source, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read fixture workflow %q: %v", workflowPath, err)
	}

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "github_actions_workflows:.github/workflows/ci.yml",
		RepoID:       "github_actions_workflows",
		RelativePath: ".github/workflows/ci.yml",
		EntityType:   "File",
		EntityName:   "ci",
		Language:     "yaml",
		SourceCache:  string(source),
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	// POSITIVE: the genuine step-level third-party action reference is extracted.
	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "hashicorp/setup-terraform") {
		t.Fatalf("missing DEPENDS_ON hashicorp/setup-terraform action edge: %#v", relationships.outgoing)
	}
	// FOIL: the uses: line inside the run: block scalar must not be extracted.
	if relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "octocat/example-action") {
		t.Fatalf("run:-block uses: octocat/example-action must NOT become a DEPENDS_ON edge: %#v", relationships.outgoing)
	}
	// actions/checkout stays excluded by design.
	if relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "actions/checkout") {
		t.Fatalf("actions/checkout must be excluded from DEPENDS_ON: %#v", relationships.outgoing)
	}
	// Exactly one action-repository edge (the positive) — a closed bound that
	// fails if the run:-block foil is ever extracted as a second edge.
	if got, want := relationshipReasonCount(relationships, "DEPENDS_ON", "github_actions_action_repository"), 1; got != want {
		t.Fatalf("DEPENDS_ON action-repository edge count = %d, want %d: %#v", got, want, relationships.outgoing)
	}
}
