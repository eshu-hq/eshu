// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import "testing"

// TestResolvedRelationshipIDRepoLevelCollision is the #8299 regression: two
// repo-level rows of one type in one generation must hold distinct ids, and
// each id must be a pure function of the unique-index identity so a re-run
// that shifts slice ordinals cannot reuse another identity's id.
func TestResolvedRelationshipIDRepoLevelCollision(t *testing.T) {
	t.Parallel()

	rowA := ResolvedRelationship{
		SourceRepoID:     "repo-a",
		TargetRepoID:     "repo-b",
		RelationshipType: RelDeploysFrom,
	}
	rowB := ResolvedRelationship{
		SourceRepoID:     "repo-c",
		TargetRepoID:     "repo-d",
		RelationshipType: RelDeploysFrom,
	}

	// Different identities: ids must differ.
	idA := ResolvedRelationshipID("gen-1", rowA)
	idB := ResolvedRelationshipID("gen-1", rowB)
	if idA == "" || idB == "" {
		t.Fatal("ResolvedRelationshipID() = empty, want stable id")
	}
	if idA == idB {
		t.Fatalf("collision: distinct repo identities share resolved_id %q", idA)
	}

	// Same identity recomputed (as a re-run with shifted ordinals would):
	// the id must be stable because there is no ordinal input.
	if again := ResolvedRelationshipID("gen-1", rowA); again != idA {
		t.Fatalf("identity drift: rowA = %q then %q, want stable id", idA, again)
	}
	if again := ResolvedRelationshipID("gen-1", rowB); again != idB {
		t.Fatalf("identity drift: rowB = %q then %q, want stable id", idB, again)
	}
}

// BenchmarkResolvedRelationshipID bounds the per-row digest cost of the
// #8299 identity digest (referenced by docs/internal/evidence/
// 8299-resolved-id-no-regression.md).
func BenchmarkResolvedRelationshipID(b *testing.B) {
	row := ResolvedRelationship{
		SourceRepoID:     "repo-a",
		TargetRepoID:     "repo-b",
		RelationshipType: RelDeploysFrom,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ResolvedRelationshipID("gen-1", row)
	}
}

// TestResolvedRelationshipIDFallsBackToRepoIDs pins the #8299 digest inputs:
// an empty entity id falls back to the repo id, matching the COALESCE in
// resolved_relationships_identity_idx. Entity-level rows keep the entity id.
func TestResolvedRelationshipIDFallsBackToRepoIDs(t *testing.T) {
	t.Parallel()

	repoLevel := ResolvedRelationship{
		SourceRepoID:     "repo-a",
		TargetRepoID:     "repo-b",
		RelationshipType: RelDeploysFrom,
	}
	entityLevel := ResolvedRelationship{
		SourceRepoID:     "repo-a",
		TargetRepoID:     "repo-b",
		SourceEntityID:   "repo-a",
		TargetEntityID:   "repo-b",
		RelationshipType: RelDeploysFrom,
	}
	otherRepo := ResolvedRelationship{
		SourceRepoID:     "repo-c",
		TargetRepoID:     "repo-d",
		RelationshipType: RelDeploysFrom,
	}

	repoID := ResolvedRelationshipID("gen-1", repoLevel)
	entityID := ResolvedRelationshipID("gen-1", entityLevel)
	if repoID != entityID {
		t.Fatalf("repo fallback mismatch: repo-level %q != entity-level %q for the same identity", repoID, entityID)
	}
	if other := ResolvedRelationshipID("gen-1", otherRepo); other == repoID {
		t.Fatalf("collision: distinct repo identities share resolved_id %q", repoID)
	}
}
