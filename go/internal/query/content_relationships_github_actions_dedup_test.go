// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

// TestGitHubActionsSourceRelationshipsDedupesRepeatedRefsWithinSource is the
// #5377 dedup-retention guard. The metadata relationship path was deleted, but
// the SourceCache path alone still emits duplicate (type, target, reason)
// tuples whenever the same reference appears more than once in a file: here the
// same action (docker/build-push-action@v5) is used in two steps and the same
// repository is checked out in two jobs. buildOutgoingGitHubActionsRelationships
// must still collapse each duplicate to exactly one edge. This test fails if the
// (type|target|reason) `seen` dedup is stripped along with the metadata path.
//
// It lives beside the source-path tests and reuses relationshipReasonCount and
// relationshipHasTarget from content_relationships_github_actions_source_test.go.
func TestGitHubActionsSourceRelationshipsDedupesRepeatedRefsWithinSource(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:     "gha-dedup",
		RepoID:       "repo-1",
		RelativePath: ".github/workflows/build.yaml",
		EntityType:   "File",
		EntityName:   "build",
		Language:     "yaml",
		SourceCache: `jobs:
  build-amd64:
    steps:
      - uses: actions/checkout@v4
        with:
          repository: myorg/shared-config
      - uses: docker/build-push-action@v5
  build-arm64:
    steps:
      - uses: actions/checkout@v4
        with:
          repository: myorg/shared-config
      - uses: docker/build-push-action@v5
`,
	}, nil)
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if got := relationshipReasonCount(relationships, "DEPENDS_ON", "github_actions_action_repository"); got != 1 {
		t.Fatalf("DEPENDS_ON github_actions_action_repository count = %d, want 1 (docker/build-push-action@v5 used in two steps): %#v", got, relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DEPENDS_ON", "github_actions_action_repository", "docker/build-push-action") {
		t.Fatalf("missing DEPENDS_ON docker/build-push-action edge: %#v", relationships.outgoing)
	}
	if got := relationshipReasonCount(relationships, "DISCOVERS_CONFIG_IN", "github_actions_checkout_repository"); got != 1 {
		t.Fatalf("DISCOVERS_CONFIG_IN github_actions_checkout_repository count = %d, want 1 (myorg/shared-config checked out in two jobs): %#v", got, relationships.outgoing)
	}
	if !relationshipHasTarget(relationships, "DISCOVERS_CONFIG_IN", "github_actions_checkout_repository", "myorg/shared-config") {
		t.Fatalf("missing DISCOVERS_CONFIG_IN myorg/shared-config edge: %#v", relationships.outgoing)
	}
	if len(relationships.outgoing) != 2 {
		t.Fatalf("len(relationships.outgoing) = %d, want 2 deduped edges: %#v", len(relationships.outgoing), relationships.outgoing)
	}
}
