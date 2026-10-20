// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"testing"
)

// TestRetirementFenceThenPurgeLive proves the operator trigger's store
// sequence against real Postgres: FenceRepository marks and fences one
// seeded repository, then PurgeRetired purges exactly that fenced
// repository to complete. The purge live tests seed their tombstone with
// the standalone mark, so this is the only live proof of the designed
// mark-then-fence-then-purge order the trigger (#8602) invokes.
func TestRetirementFenceThenPurgeLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_trigger_seq",
		"set ESHU_POSTGRES_DSN to run the retirement trigger-sequence Postgres proof")
	partition := "PARTITION:trigger-live"
	scopeIDs := seedPurgeLiveScope(t, ctx, database, partition)

	fencer := NewRetirementFencer(SQLDB{DB: database}, SQLDB{DB: database})
	fenced, err := fencer.FenceRepository(ctx, FenceRequest{
		Retirements: []FenceRetirement{{
			RetirementID: "rr_trigger_live",
			RepoID:       partition,
			ScopeID:      scopeIDs[0],
		}},
		ReasonCode:         RetirementReasonOperatorRetired,
		ReasonHash:         "sha256:live-reason",
		ActorClass:         "operator",
		ActorIDHash:        "sha256:live-actor",
		IdempotencyKeyHash: "sha256:live-key",
	})
	if err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}
	if len(fenced.RepoIDs) != 1 || fenced.RepoIDs[0] != partition {
		t.Fatalf("fenced repos = %v, want [%s]", fenced.RepoIDs, partition)
	}

	store := NewRetirementStore(SQLDB{DB: database})
	open, err := store.GetOpen(ctx, partition)
	if err != nil {
		t.Fatalf("GetOpen after fence: %v", err)
	}
	if open == nil {
		t.Fatal("no open tombstone after the fence")
	}
	if open.State != RetirementStatePending || open.Phase != RetirementPhaseFenced {
		t.Fatalf("tombstone = %s/%s, want pending/fenced", open.State, open.Phase)
	}

	purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
	purged, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: fenced.RepoIDs})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(purged.Blocked) != 0 {
		t.Fatalf("blocked = %+v, want none on terminal generations", purged.Blocked)
	}
	if len(purged.Completed) != 1 || purged.Completed[0] != partition {
		t.Fatalf("completed = %v, want [%s]", purged.Completed, partition)
	}

	complete, err := store.GetOpen(ctx, partition)
	if err != nil {
		t.Fatalf("GetOpen after purge: %v", err)
	}
	if complete == nil || complete.State != RetirementStateComplete {
		t.Fatalf("tombstone after purge = %+v, want complete", complete)
	}
}

// TestRetirementTriggerSequenceRefusesUnfencedLive proves the trigger's
// second acceptance refusal against real Postgres: purging a repository
// the fence never marked fails with ErrRetirementNoOpenTombstone and
// writes nothing.
func TestRetirementTriggerSequenceRefusesUnfencedLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_trigger_unfenced",
		"set ESHU_POSTGRES_DSN to run the retirement unfenced-refusal Postgres proof")
	purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
	_, err := purger.PurgeRetired(ctx,
		PurgeRequest{RepoIDs: []string{"PARTITION:trigger-never-fenced"}})
	if err == nil {
		t.Fatal("PurgeRetired on an unfenced repository succeeded, want ErrRetirementNoOpenTombstone")
	}
	if !errors.Is(err, ErrRetirementNoOpenTombstone) {
		t.Fatalf("error = %v, want ErrRetirementNoOpenTombstone", err)
	}
}
