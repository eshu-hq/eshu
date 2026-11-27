// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Test11619ResolvedSetDigestStableAcrossInputOrder proves the
// deployment-evidence fence digest is a pure function of the resolved edge
// set: a rerun that assigns a different slice order to the same identities
// computes the same digest, so the rerun stays quiet.
func Test11619ResolvedSetDigestStableAcrossInputOrder(t *testing.T) {
	t.Parallel()

	edges := []relationships.ResolvedRelationship{
		{SourceRepoID: "repo-a", TargetRepoID: "repo-b", RelationshipType: relationships.RelRunsOn},
		{SourceRepoID: "repo-a", TargetRepoID: "repo-c", RelationshipType: relationships.RelDependsOn},
		{SourceEntityID: "entity-x", TargetRepoID: "repo-b", RelationshipType: relationships.RelRunsOn},
	}
	shuffled := []relationships.ResolvedRelationship{edges[2], edges[0], edges[1]}

	first := resolvedSetDigest("generation-11619", edges)
	second := resolvedSetDigest("generation-11619", shuffled)
	if first == "" {
		t.Fatal("resolvedSetDigest() = empty, want a stable non-empty fence value")
	}
	if first != second {
		t.Fatalf("resolvedSetDigest() changed across input order: %q vs %q", first, second)
	}
}

// Test11619ResolvedSetDigestChangesWhenSetChanges proves an added or removed
// resolved edge moves the fence so the changed rerun reopens.
func Test11619ResolvedSetDigestChangesWhenSetChanges(t *testing.T) {
	t.Parallel()

	base := []relationships.ResolvedRelationship{
		{SourceRepoID: "repo-a", TargetRepoID: "repo-b", RelationshipType: relationships.RelRunsOn},
	}
	extended := append(append([]relationships.ResolvedRelationship(nil), base...),
		relationships.ResolvedRelationship{
			SourceRepoID: "repo-a", TargetRepoID: "repo-c", RelationshipType: relationships.RelRunsOn,
		})

	if got, want := resolvedSetDigest("generation-11619", base), resolvedSetDigest("generation-11619", extended); got == want {
		t.Fatalf("resolvedSetDigest() = %q for both sets, want the extended set to differ", got)
	}
}

// Test11619ResolvedSetDigestEmptySetIsStable proves the empty resolved set
// (the evidence-tombstone pass) still fences on a stable value: the first
// empty pass replays once by fail-open, and the repeat stays quiet.
func Test11619ResolvedSetDigestEmptySetIsStable(t *testing.T) {
	t.Parallel()

	if got, want := resolvedSetDigest("generation-11619", nil), resolvedSetDigest("generation-11619", []relationships.ResolvedRelationship{}); got != want {
		t.Fatalf("resolvedSetDigest() empty-set mismatch: %q vs %q", got, want)
	}
	if got := resolvedSetDigest("generation-11619", nil); got == "" {
		t.Fatal("resolvedSetDigest() = empty for the empty set, want a stable fence value")
	}
}
