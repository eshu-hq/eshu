// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestRelationshipStoreRepoLevelOrdinalShift is the #8299 storage RED
// regression: two repo-level rows of one type in one generation, re-run with
// shifted ordinals. On real Postgres the second insert fails on the
// resolved_id primary key (ON CONFLICT is on the identity index, not the PK);
// the in-memory fake models that collision as crossed ids overwriting the
// other identity's slot, so the test asserts per-identity id stability.
func TestRelationshipStoreRepoLevelOrdinalShift(t *testing.T) {
	t.Parallel()

	database := newRelationshipTestDB()
	store := NewRelationshipStore(database)
	ctx := context.Background()

	rowA := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-a",
		TargetRepoID:     "repo-b",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.9,
		ResolutionSource: relationships.ResolutionSourceInferred,
		Details:          map[string]any{},
	}
	rowB := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-c",
		TargetRepoID:     "repo-d",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.9,
		ResolutionSource: relationships.ResolutionSourceInferred,
		Details:          map[string]any{},
	}

	if _, err := store.UpsertResolved(ctx, "gen-1", []relationships.ResolvedRelationship{rowA, rowB}); err != nil {
		t.Fatalf("UpsertResolved first run: %v", err)
	}
	first := snapshotResolvedByIdentity(t, database)

	// Re-run with shifted ordinals: same identities, opposite slice order.
	if _, err := store.UpsertResolved(ctx, "gen-1", []relationships.ResolvedRelationship{rowB, rowA}); err != nil {
		t.Fatalf("UpsertResolved re-run: re-run must not produce a primary key conflict for existing identities: %v", err)
	}
	second := snapshotResolvedByIdentity(t, database)

	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("resolved rows first=%d second=%d, want 2 and 2", len(first), len(second))
	}
	for identity, firstID := range first {
		secondID, ok := second[identity]
		if !ok {
			t.Fatalf("identity %q lost on re-run", identity)
		}
		if firstID != secondID {
			t.Fatalf("identity %q id drifted: first %q, re-run %q (ordinal shift reuses another identity's id)", identity, firstID, secondID)
		}
	}
}

func snapshotResolvedByIdentity(t *testing.T, database *relationshipTestDB) map[string]string {
	t.Helper()

	out := make(map[string]string, len(database.resolved))
	for id, rec := range database.resolved {
		key := rec.generationID + "|" + rec.sourceRepoID + "|" + rec.targetRepoID + "|" + rec.relType +
			"|" + rec.sourceEntityID + "|" + rec.targetEntityID
		if prev, dup := out[key]; dup {
			t.Fatalf("duplicate identity %q holds ids %q and %q", key, prev, id)
		}
		out[key] = id
	}
	return out
}
