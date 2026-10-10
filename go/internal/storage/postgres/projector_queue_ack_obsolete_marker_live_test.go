// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// seedAckMarkerScope claims gen-am2 and then late-arrives gen-am1, an older
// pending generation with pending work, returning the open database, the
// queue, and gen-am2's claim for Ack.
func seedAckMarkerScope(t *testing.T, dsn string) (*sql.DB, ProjectorQueue, projector.ScopeGenerationWork) {
	t.Helper()
	database := openClaimDeadlockProofDB(t, dsn, 2)
	at := time.Now().UTC().Truncate(time.Second)
	seedClaimMaintenanceScopes(t, database, "scope-am")
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-am", "gen-am2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	ctx := context.Background()
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	queue.Now = func() time.Time { return at }
	work, ok, err := queue.Claim(ctx)
	if err != nil || !ok || work.Generation.GenerationID != "gen-am2" {
		t.Fatalf("Claim() = (%q, %v, %v), want gen-am2", work.Generation.GenerationID, ok, err)
	}
	// An older generation committed while gen-am2 was being projected.
	seedFenceProofWork(t, database, fenceProofWork{
		"scope-am", "gen-am1", "pending", "pending", 0,
		at.Add(-2 * time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	return database, queue, work
}

// TestProjectorAckSeesLockTimeMarkerTruth proves Ack's obsolete-generation
// supersede observes the write marker at lock time, not from the statement
// snapshot (#7820).
//
//   - marker_commits_after_ack_snapshot: Ack blocks on gen-am1's work row
//     inside the obsolete supersede; the marker commits while it waits
//     (provably after the statement's snapshot, because the test observes
//     the blocked lock attempt first) and the holder releases the row.
//     The Ack must publish gen-am2 and spare the marked gen-am1.
//   - inflight_marker_skips_without_waiting: the marker transaction stays
//     open on gen-am1's generation row while gen-am2 Acks. The Ack must
//     succeed at once and leave gen-am1 alone instead of timing out on the
//     foreign-key row check behind the in-flight write.
func TestProjectorAckSeesLockTimeMarkerTruth(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)

	t.Run("marker_commits_after_ack_snapshot", func(t *testing.T) {
		database, queue, work := seedAckMarkerScope(t, dsn)
		queue.AckScopeLockTimeout = maxProjectorAckLockTimeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		holderDB := openSessionOnProofSchema(ctx, t, database, dsn)
		holder, err := holderDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin holder: %v", err)
		}
		defer func() { _ = holder.Rollback() }()
		var held string
		if err := holder.QueryRowContext(ctx,
			`SELECT work_item_id FROM fact_work_items WHERE generation_id = 'gen-am1' FOR UPDATE`).Scan(&held); err != nil {
			t.Fatalf("hold the gen-am1 work row: %v", err)
		}
		if _, err := holder.ExecContext(ctx,
			`UPDATE scope_generations SET projection_write_started_at = now() WHERE generation_id = 'gen-am1'`); err != nil {
			t.Fatalf("stage the marker uncommitted: %v", err)
		}
		var holderXID string
		if err := holder.QueryRowContext(ctx, `SELECT pg_current_xact_id()`).Scan(&holderXID); err != nil {
			t.Fatalf("read holder xid: %v", err)
		}
		ackDone := make(chan error, 1)
		go func() {
			ackDone <- queue.Ack(ctx, work, runtime.Result{})
		}()
		// Wait until the Ack settles: either it blocks on the held work
		// row (the pre-fix UPDATE locks the row) or it finishes (the
		// fixed lock step skips the held generation and never touches
		// the row). Either way the obsolete statement took its snapshot
		// before the marker below commits.
		var ackErr error
		ackFinished := false
		deadline := time.Now().Add(10 * time.Second)
		for settled := false; !settled && time.Now().Before(deadline); {
			select {
			case ackErr = <-ackDone:
				ackFinished = true
				settled = true
			default:
			}
			if !settled {
				// A backend blocked on a row with an uncommitted version
				// waits on the holder's transaction id (locktype
				// transactionid, NULL relation), not on a tuple lock, so
				// scope the poll to the holder xid. That observes exactly
				// "the Ack is blocked behind our holder" and is immune to
				// unrelated backends waiting anywhere else.
				var waiting int
				if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks
WHERE NOT granted
  AND locktype = 'transactionid'
  AND transactionid::text = $1`, holderXID).Scan(&waiting); err != nil {
					t.Fatalf("poll pg_locks: %v", err)
				}
				settled = waiting > 0
			}
			if !settled {
				time.Sleep(10 * time.Millisecond)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("Ack neither blocked nor finished within 10s")
		}
		if err := holder.Commit(); err != nil {
			t.Fatalf("commit the marker: %v", err)
		}
		if !ackFinished {
			ackErr = <-ackDone
		}
		if ackErr != nil {
			t.Fatalf("Ack(gen-am2) = %v, want success", ackErr)
		}
		if got := generationState(t, database, "gen-am2"); got != "active" {
			t.Fatalf("gen-am2 generation = %q, want active", got)
		}
		if status, class, _ := workState(t, database, "scope-am", "gen-am1"); status != "pending" || class != "" {
			t.Fatalf("gen-am1 work = (%s, %q), want pending with no supersede class", status, class)
		}
		if got := generationState(t, database, "gen-am1"); got != "pending" {
			t.Fatalf("gen-am1 generation = %q, want pending", got)
		}
		var marked bool
		if err := database.QueryRowContext(ctx,
			`SELECT projection_write_started_at IS NOT NULL FROM scope_generations WHERE generation_id = 'gen-am1'`).Scan(&marked); err != nil {
			t.Fatalf("read gen-am1 marker: %v", err)
		}
		if !marked {
			t.Fatal("gen-am1 marker is not set, the test staged nothing")
		}
	})

	t.Run("inflight_marker_skips_without_waiting", func(t *testing.T) {
		database, queue, work := seedAckMarkerScope(t, dsn)
		queue.AckScopeLockTimeout = 100 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		holderDB := openSessionOnProofSchema(ctx, t, database, dsn)
		holder, err := holderDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin holder: %v", err)
		}
		defer func() { _ = holder.Rollback() }()
		if _, err := holder.ExecContext(ctx,
			`UPDATE scope_generations SET projection_write_started_at = now() WHERE generation_id = 'gen-am1'`); err != nil {
			t.Fatalf("hold the marker uncommitted: %v", err)
		}
		if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
			if errors.Is(err, failure.ErrWorkAckDeferred) {
				t.Fatalf("Ack(gen-am2) deferred on the in-flight marker, want success without waiting: %v", err)
			}
			t.Fatalf("Ack(gen-am2) = %v, want success", err)
		}
		if status, class, _ := workState(t, database, "scope-am", "gen-am1"); status != "pending" || class != "" {
			t.Fatalf("gen-am1 work = (%s, %q), want pending with no supersede class", status, class)
		}
		if got := generationState(t, database, "gen-am1"); got != "pending" {
			t.Fatalf("gen-am1 generation = %q, want pending", got)
		}
	})
}

// ackMarkerRaceIterations is the #7820 ruling's N for marker-commit vs Ack.
const ackMarkerRaceIterations = 300

// TestProjectorAckMarkerCommitRace is the statistical half of the #7820
// exclusion proof: a marker commit and gen-ar2's Ack race 300 times with
// either side delayed by a seeded random 0-400us. The commit-after-snapshot
// alignment cannot be choreographed deterministically (the obsolete
// statement's lock attempts land microseconds after its snapshot), so the
// race covers it by volume: no trial may retire a marked generation, and
// no trial may error. gen-ar1 is late-arrived after each trial's claim so
// the claim sweep never engages it.
func TestProjectorAckMarkerCommitRace(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 4)
	at := time.Now().UTC().Truncate(time.Second)
	seedClaimMaintenanceScopes(t, database, "scope-ar")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	markerDB := openSessionOnProofSchema(ctx, t, database, dsn)
	ackDB := openSessionOnProofSchema(ctx, t, database, dsn)
	ackQueue := NewProjectorQueue(SQLDB{DB: ackDB}, "acker", time.Minute)
	ackQueue.Now = func() time.Time { return at }
	claimQueue := NewProjectorQueue(SQLDB{DB: database}, "acker", time.Minute)
	claimQueue.Now = func() time.Time { return at }

	seedFenceProofWork(t, database, fenceProofWork{
		"scope-ar", "gen-ar2", "pending", "pending", 0,
		at.Add(-time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
	})
	rng := rand.New(rand.NewSource(7820))
	violations := 0
	ackErrs := 0
	for i := 0; i < ackMarkerRaceIterations; i++ {
		for _, stmt := range []string{
			`UPDATE scope_generations SET status = 'pending', activated_at = NULL, superseded_at = NULL
WHERE generation_id = 'gen-ar2'`,
			`UPDATE fact_work_items SET status = 'pending', lease_owner = NULL, claim_until = NULL,
    next_attempt_at = NULL, failure_class = NULL,
    failure_message = NULL, failure_details = NULL
WHERE generation_id = 'gen-ar2'`,
			`DELETE FROM fact_work_items WHERE generation_id = 'gen-ar1'`,
			`DELETE FROM scope_generations WHERE generation_id = 'gen-ar1'`,
		} {
			if _, err := database.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("iteration %d: reset: %v", i, err)
			}
		}
		work, ok, err := claimQueue.Claim(ctx)
		if err != nil || !ok || work.Generation.GenerationID != "gen-ar2" {
			t.Fatalf("iteration %d: Claim() = (%q, %v, %v), want gen-ar2",
				i, work.Generation.GenerationID, ok, err)
		}
		seedFenceProofWork(t, database, fenceProofWork{
			"scope-ar", "gen-ar1", "pending", "pending", 0,
			at.Add(-2 * time.Hour), at.Add(-time.Hour), at.Add(-time.Hour),
		})
		mode := i % 3 // 0 simultaneous, 1 marker delayed, 2 ack delayed
		delay := time.Duration(rng.Intn(400)) * time.Microsecond
		start := make(chan struct{})
		var wg sync.WaitGroup
		var markerErr, ackErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if mode == 1 {
				time.Sleep(delay)
			}
			_, markerErr = markerDB.ExecContext(ctx, `
UPDATE scope_generations SET projection_write_started_at = now()
WHERE generation_id = 'gen-ar1' AND status IN ('pending', 'failed')`)
		}()
		go func() {
			defer wg.Done()
			<-start
			if mode == 2 {
				time.Sleep(delay)
			}
			ackErr = ackQueue.Ack(ctx, work, runtime.Result{})
		}()
		close(start)
		wg.Wait()
		if markerErr != nil {
			t.Fatalf("iteration %d: marker = %v", i, markerErr)
		}
		if ackErr != nil {
			ackErrs++
			t.Logf("iteration %d: Ack = %v", i, ackErr)
		}
		var workStatus string
		var marked bool
		if err := database.QueryRowContext(ctx, `
SELECT w.status, g.projection_write_started_at IS NOT NULL
FROM fact_work_items AS w
JOIN scope_generations AS g ON g.generation_id = w.generation_id
WHERE w.generation_id = 'gen-ar1'`).Scan(&workStatus, &marked); err != nil {
			t.Fatalf("iteration %d: read final: %v", i, err)
		}
		if workStatus == "superseded" && marked {
			violations++
		}
	}
	t.Logf("ack/marker race iterations=%d violations=%d ack errors=%d", ackMarkerRaceIterations, violations, ackErrs)
	if violations != 0 {
		t.Errorf("retired a marked generation in %d of %d trials", violations, ackMarkerRaceIterations)
	}
	if ackErrs != 0 {
		t.Errorf("Ack errored in %d of %d trials", ackErrs, ackMarkerRaceIterations)
	}
}
