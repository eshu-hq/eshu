// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that a relationship generation which stops being an orphan between the census and its batch is skipped as ineligible with every row intact (a scope generation row appears, the generation row turns active, a scope pointer names it), that the production relationship activation blocks behind the batch's generation row lock instead of racing the deletes and re-creates the generation only after the batch commits, that a repeated activation race never leaves an active generation row without the children its own statement kept, and that the claim-fenced activation cannot revive an id that has no scope generation
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// lockWaitUntil polls until a backend of this database is waiting on a lock
// while running a statement that matches likePattern, or the timeout passes. It
// returns an error instead of failing the test so a hook running on the sweep's
// goroutine can use it.
func (f *liveFixture) lockWaitUntil(likePattern string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		var waiting int
		if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE $1`, likePattern).Scan(&waiting); err != nil {
			return false, fmt.Errorf("poll lock waits: %w", err)
		}
		if waiting > 0 {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitForLockWait is lockWaitUntil for the test goroutine: a polling error
// fails the test.
func (f *liveFixture) waitForLockWait(t *testing.T, likePattern string, timeout time.Duration) bool {
	t.Helper()
	waiting, err := f.lockWaitUntil(likePattern, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return waiting
}

// generationStatus reads a generation row's status and created_at, with
// present false when the row does not exist.
func (f *liveFixture) generationStatus(t *testing.T, generationID string) (status string, createdAt time.Time, present bool) {
	t.Helper()
	err := f.db.QueryRowContext(f.ctx, `SELECT status, created_at FROM relationship_generations WHERE generation_id = $1`, generationID).
		Scan(&status, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false
	}
	if err != nil {
		t.Fatalf("read generation row %s: %v", generationID, err)
	}
	return status, createdAt, true
}

// seededCreatedAt is the created_at the fixture gives every generation row: an
// activation that updated the existing row keeps it, one that re-created the
// row after a sweep stamps its own.
var seededCreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// TestStateChangeAfterTheCensusIsIneligibleLive proves the batch re-checks Rule
// A in its own snapshot rather than trusting the census. The hook changes the
// generation after the census listed it (the first batch statement has run), by
// each of the three ways that keeps it alive. The batch must report it as
// ineligible and delete nothing. A classification that dropped one of the
// predicates would claim it and count it swept, so the per-reason counters are
// part of the proof, not only the surviving rows.
func TestStateChangeAfterTheCensusIsIneligibleLive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(f *liveFixture, t *testing.T)
	}{
		{"a scope generation row appears", func(f *liveFixture, t *testing.T) {
			f.scopeGeneration(t, "scope-x", "gen-x", "pending")
		}},
		{"the generation row turns active", func(f *liveFixture, t *testing.T) {
			f.exec(t, `UPDATE relationship_generations SET status = 'active', activated_at = now() WHERE generation_id = 'gen-x'`)
		}},
		{"a scope pointer names it", func(f *liveFixture, t *testing.T) {
			f.pointer(t, "scope-x", "gen-x")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openLiveFixture(t)
			f.scope(t, "scope-x")
			f.generation(t, "scope-x", "gen-x", "superseded")
			f.evidence(t, "gen-x", 3)
			f.candidates(t, "gen-x", 2)

			changed := false
			opts := relationshipretention.Options{Deletes: true, RowCap: 1 << 40}
			relationshipretention.SetAfterStatement(&opts, func(stage string) error {
				if stage == relationshipretention.StageSetLockTimeout && !changed {
					changed = true
					tc.change(f, t)
				}
				return nil
			})
			result, err := relationshipretention.Sweep(f.ctx, f.database, opts)
			if err != nil {
				t.Fatalf("Sweep: %v", err)
			}
			if !changed {
				t.Fatal("the hook never ran: the census listed nothing, so no batch opened")
			}
			if got := result.Census.Generations; len(got) != 1 || got[0] != "gen-x" {
				t.Fatalf("census listed %v, want [gen-x] before the change", got)
			}
			if result.Skipped[relationshipretention.SkipIneligible] != 1 || result.GenerationsSwept != 0 || result.Batches != 0 {
				t.Fatalf("pass = swept %d, %d batches, skipped %v; want the generation skipped as ineligible and nothing swept",
					result.GenerationsSwept, result.Batches, result.Skipped)
			}
			if got := f.of(t, "gen-x"); got != (counts{3, 2, 1, 0}) {
				t.Fatalf("rows after the pass = %+v, want every row intact {3 2 1 0}", got)
			}
		})
	}
}

// TestActivationBlocksBehindTheBatchRowLockLive runs the production unclaimed
// relationship generation activation while a batch holds the generation row. The
// batch locked the row at classification and has already deleted the evidence;
// the activation arrives, and must wait for the row, not slip an update in
// between the deletes. Once the batch commits the row is gone, so the
// activation's upsert re-creates it: the end state is an active generation row
// with none of the children the batch removed, never an active row that lost
// part of its children.
//
// That re-creation is only reachable for an id with no scope generation (the
// claim-fenced activation cannot do it, see the next test) and is recorded here
// as the observed outcome.
func TestActivationBlocksBehindTheBatchRowLockLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-a")
	f.generation(t, "scope-a", "gen-a", "superseded")
	f.evidence(t, "gen-a", 3)
	f.candidates(t, "gen-a", 2)
	store := postgres.NewRelationshipStore(f.database)

	var activation sync.WaitGroup
	var activationErr error
	blocked := false
	started := false
	opts := relationshipretention.Options{Deletes: true, RowCap: 1 << 40}
	relationshipretention.SetAfterStatement(&opts, func(stage string) error {
		if stage != relationshipretention.StageDeleteEvidence || started {
			return nil
		}
		started = true
		activation.Add(1)
		go func() {
			defer activation.Done()
			activationErr = store.ActivateResolutionGeneration(f.ctx, "gen-a", "scope-a")
		}()
		// The batch is still open and holds the generation row. Wait until the
		// activation is queued behind it, then let the batch carry on.
		blocked = f.waitForLockWait(t, "%INSERT INTO relationship_generations%", 10*time.Second)
		return nil
	})
	result, err := relationshipretention.Sweep(f.ctx, f.database, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	activation.Wait()
	if !blocked {
		t.Fatal("the activation never waited on the batch's generation row: its lock does not span the deletes")
	}
	if activationErr != nil {
		t.Fatalf("ActivateResolutionGeneration: %v", activationErr)
	}
	if result.GenerationsSwept != 1 || result.RowsSwept[relationshipretention.EvidenceTable] != 3 {
		t.Fatalf("pass swept %d generations, %v rows; want the batch to commit whole", result.GenerationsSwept, result.RowsSwept)
	}
	status, createdAt, present := f.generationStatus(t, "gen-a")
	if !present || status != "active" || createdAt.Equal(seededCreatedAt) {
		t.Fatalf("generation row = (%q, %s, present %v), want a row the activation re-created after the batch", status, createdAt, present)
	}
	if got := f.of(t, "gen-a"); got.evidence != 0 || got.candidates != 0 {
		t.Fatalf("rows after = %+v, want the batch's deletes whole (no evidence, no candidates)", got)
	}
}

// TestActivationRaceKeepsEveryOutcomeConsistentLive repeats the unclaimed
// activation against a sweep of the same generation. Whatever the interleaving,
// the generation ends active, and an activation that updated the existing row
// (the row still carries the seeded created_at) leaves every child row intact;
// only a row the activation re-created after the sweep may be missing them, and
// then it is missing all of them. It logs how often each outcome occurred.
func TestActivationRaceKeepsEveryOutcomeConsistentLive(t *testing.T) {
	f := openLiveFixture(t)
	store := postgres.NewRelationshipStore(f.database)
	const rounds = 60
	var updatedExisting, recreated int
	for i := 0; i < rounds; i++ {
		scope, generation := fmt.Sprintf("scope-r%02d", i), fmt.Sprintf("gen-r%02d", i)
		f.scope(t, scope)
		f.generation(t, scope, generation, "superseded")
		f.evidence(t, generation, 3)
		f.candidates(t, generation, 2)

		start := make(chan struct{})
		var wg sync.WaitGroup
		var sweepErr, activateErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, sweepErr = relationshipretention.Sweep(f.ctx, f.database, relationshipretention.Options{Deletes: true, RowCap: 1 << 40})
		}()
		stagger := time.Duration(i%12) * time.Millisecond // walks the activation across the sweep's statements
		go func() {
			defer wg.Done()
			<-start
			time.Sleep(stagger)
			activateErr = store.ActivateResolutionGeneration(f.ctx, generation, scope)
		}()
		close(start)
		wg.Wait()
		if sweepErr != nil || activateErr != nil {
			t.Fatalf("round %d: sweep error %v, activation error %v", i, sweepErr, activateErr)
		}

		status, createdAt, present := f.generationStatus(t, generation)
		got := f.of(t, generation)
		switch {
		case !present || status != "active":
			t.Fatalf("round %d: generation row = (%q, present %v), want active", i, status, present)
		case createdAt.Equal(seededCreatedAt):
			updatedExisting++
			if got != (counts{3, 2, 1, 0}) {
				t.Fatalf("round %d: the activation updated the existing row but the children are %+v, want {3 2 1 0}", i, got)
			}
		default:
			recreated++
			if got.evidence != 0 || got.candidates != 0 {
				t.Fatalf("round %d: the activation re-created the row but the children are %+v, want none", i, got)
			}
		}
	}
	t.Logf("activation race over %d rounds: updated the existing row %d, re-created after the sweep %d", rounds, updatedExisting, recreated)
}

// TestFencedActivationCannotReviveAnOrphanIDLive pins why the re-creation above
// stays out of production: the claim-fenced activation needs a live claimed work
// item for the exact (scope, generation), and a work item cannot exist for a
// generation without a scope generation row. So for an orphan id the fenced
// activation inserts and updates nothing, and the sweep still removes it.
func TestFencedActivationCannotReviveAnOrphanIDLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-f")
	f.generation(t, "scope-f", "gen-f", "superseded")
	f.evidence(t, "gen-f", 3)
	f.candidates(t, "gen-f", 2)

	_, err := f.db.ExecContext(f.ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ('work-f', 'scope-f', 'gen-f', 'reducer', 'deployment_mapping', 'claimed', now(), now())`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("work item for a generation with no scope generation: error %v, want a foreign key violation (23503)", err)
	}

	store := postgres.NewRelationshipStore(f.database)
	err = store.ActivateResolutionGenerationForClaim(f.ctx, "gen-f", "scope-f", "work-f", time.Now())
	if !errors.Is(err, reducercontract.ErrExecutionClaimRejected) {
		t.Fatalf("fenced activation of an orphan id: error %v, want ErrExecutionClaimRejected", err)
	}
	status, createdAt, present := f.generationStatus(t, "gen-f")
	if !present || status != "superseded" || !createdAt.Equal(seededCreatedAt) {
		t.Fatalf("generation row after the rejected activation = (%q, %s, present %v), want it untouched", status, createdAt, present)
	}
	if result := f.sweep(t); result.GenerationsSwept != 1 {
		t.Fatalf("pass swept %d generations, want the orphan swept", result.GenerationsSwept)
	}
	if got := f.of(t, "gen-f"); got != (counts{0, 0, 0, 0}) {
		t.Fatalf("rows after the sweep = %+v, want none", got)
	}
}
