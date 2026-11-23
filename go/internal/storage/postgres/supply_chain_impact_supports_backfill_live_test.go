// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// supportsBackfillSeed writes findings through the real writer into both test
// scopes, then empties the supports table: the state a database is in the day
// the maintainer ships, with finding facts and no supports.
func supportsBackfillSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	writer := replaceSetWriter(db)
	for round, scoped := range []bool{false, true} {
		findings := []supplychaincore.SupplyChainImpactFinding{}
		for j := 0; j < 5; j++ {
			findings = append(findings, replaceSetFinding(fmt.Sprintf("repository:r_10880_bf_%d", j)))
		}
		write := replaceSetWrite(fmt.Sprintf("intent:10880:backfill-seed-%d", round), findings...)
		if scoped {
			write.ScopeID, write.GenerationID = replaceSetSecondScope, replaceSetSecondGeneration
		}
		if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
			t.Fatalf("seed write %d: %v", round, err)
		}
	}
	// A retracted finding in the first scope, and an orphan support an old writer
	// left behind for it.
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite("intent:10880:backfill-seed-retract",
		replaceSetFinding("repository:r_10880_bf_0"), replaceSetFinding("repository:r_10880_bf_1"),
		replaceSetFinding("repository:r_10880_bf_2"), replaceSetFinding("repository:r_10880_bf_3"))); err != nil {
		t.Fatalf("seed retract pass: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports`); err != nil {
		t.Fatalf("empty supports: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id,
    finding_id, has_payload_finding_id, priority_score, source_confidence, suppression_state, payload)
SELECT fact_id, 'orphan', scope_id, generation_id, 'orphan', 0, 0, 'reported', 'active', '{}'::jsonb
FROM fact_records WHERE is_tombstone = TRUE AND scope_id = $1 LIMIT 1`, replaceSetLiveScope); err != nil {
		t.Fatalf("plant orphan support: %v", err)
	}
}

func supportsBackfillRunToCompletion(t *testing.T, ctx context.Context, b postgres.SupplyChainImpactSupportsBackfill) (steps int) {
	t.Helper()
	for steps = 1; steps <= 500; steps++ {
		report, err := b.RunStep(ctx)
		if err != nil {
			t.Fatalf("backfill step %d: %v", steps, err)
		}
		if report.Complete {
			return steps
		}
	}
	t.Fatal("backfill did not complete in 500 steps")
	return steps
}

// TestSupplyChainImpactSupportsBackfillLive proves the marker-gated backfill on
// PostgreSQL: it fills the supports of facts written before the maintainer,
// removes an orphan an old writer left for a retracted fact, resumes across
// small chunks, withholds the readiness marker until a full verification finds
// nothing missing, restarts the sweep when verification does find a miss, and
// is a no-op once complete.
func TestSupplyChainImpactSupportsBackfillLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 2}

	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || ready {
		t.Fatalf("ready before the backfill = %v (err %v), want false", ready, err)
	}
	// One step is not enough to be ready, and leaves the marker unset.
	if report, err := backfill.RunStep(ctx); err != nil || report.Complete {
		t.Fatalf("first step = %+v (err %v), want incomplete", report, err)
	}
	if ready, _ := postgres.SupplyChainImpactSupportsReady(ctx, store); ready {
		t.Fatal("ready after one chunk: the marker must wait for the whole sweep and its verification")
	}
	supportsBackfillRunToCompletion(t, ctx, backfill)
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || !ready {
		t.Fatalf("ready after the backfill = %v (err %v), want true", ready, err)
	}
	for _, pair := range [][2]string{
		{replaceSetLiveScope, replaceSetLiveGeneration},
		{replaceSetSecondScope, replaceSetSecondGeneration},
	} {
		supportsLiveAssertMirror(t, ctx, db, pair[0], pair[1], "after the backfill "+pair[0])
	}
	if got := len(supportsLiveSupportIDs(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration)); got != 4 {
		t.Fatalf("supports of the first scope = %d, want 4 (the retracted finding's orphan removed)", got)
	}

	// Complete is final: another step writes nothing.
	before := supportsLiveXminSum(t, ctx, db)
	if report, err := backfill.RunStep(ctx); err != nil || !report.Complete || report.Written != 0 {
		t.Fatalf("step after completion = %+v (err %v), want complete with no writes", report, err)
	}
	if after := supportsLiveXminSum(t, ctx, db); after != before {
		t.Fatalf("a completed backfill rewrote supports (xmin sum %d to %d)", before, after)
	}
}

// TestSupplyChainImpactSupportsBackfillRestartsOnAMissLive proves the marker
// is never set over a gap: a support that goes missing after the sweep passed
// its pair (an old-binary writer in a rolling upgrade) makes the verification
// restart the sweep, and the next sweep fills it before the marker is set.
func TestSupplyChainImpactSupportsBackfillRestartsOnAMissLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 1000}

	// With a large chunk each pair finishes in one step; stop before verification.
	for i := 0; i < 2; i++ {
		if report, err := backfill.RunStep(ctx); err != nil || report.Complete || report.Verified {
			t.Fatalf("pair step %d = %+v (err %v), want incomplete and unverified", i, report, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, replaceSetLiveScope); err != nil {
		t.Fatalf("drop supports after the sweep passed them: %v", err)
	}
	report, err := backfill.RunStep(ctx)
	if err != nil {
		t.Fatalf("verification step: %v", err)
	}
	if !report.Verified || report.Missing == 0 || report.Complete {
		t.Fatalf("verification = %+v, want verified with missing rows and no completion", report)
	}
	if ready, _ := postgres.SupplyChainImpactSupportsReady(ctx, store); ready {
		t.Fatal("ready over a gap")
	}
	supportsBackfillRunToCompletion(t, ctx, backfill)
	supportsLiveAssertMirror(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "after the restarted sweep")
	if ready, _ := postgres.SupplyChainImpactSupportsReady(ctx, store); !ready {
		t.Fatal("not ready after the restarted sweep completed")
	}
}

// TestSupplyChainImpactSupportsBackfillRacesWritersLive runs the backfill with
// one-row chunks while real writers replace the finding sets of both scopes. The
// backfill takes each pair's conflict-domain lock, so it cannot resurrect a
// support a writer's retraction just removed or miss one a writer just wrote:
// after both finish the supports equal the live facts, with no deadlock.
func TestSupplyChainImpactSupportsBackfillRacesWritersLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	writer := replaceSetWriter(db)
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 1}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			report, err := backfill.RunStep(ctx)
			if err != nil {
				errs <- fmt.Errorf("backfill step: %w", err)
				return
			}
			if report.Complete {
				return
			}
		}
		errs <- errors.New("backfill did not complete")
	}()
	for round := 0; round < 6; round++ {
		for _, scoped := range []bool{false, true} {
			findings := []supplychaincore.SupplyChainImpactFinding{}
			for j := 0; j <= round%4; j++ {
				findings = append(findings, replaceSetFinding(fmt.Sprintf("repository:r_10880_race_bf_%d", j)))
			}
			write := replaceSetWrite(fmt.Sprintf("intent:10880:bf-race-%d-%v", round, scoped), findings...)
			if scoped {
				write.ScopeID, write.GenerationID = replaceSetSecondScope, replaceSetSecondGeneration
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
					var classified interface{ FailureClass() string }
					if !errors.As(err, &classified) || classified.FailureClass() != supplychaincore.SupplyChainImpactWriteSupersededFailureClass {
						errs <- err
					}
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("race: %v", err)
	}
	supportsBackfillRunToCompletion(t, ctx, backfill)
	for _, pair := range [][2]string{
		{replaceSetLiveScope, replaceSetLiveGeneration},
		{replaceSetSecondScope, replaceSetSecondGeneration},
	} {
		supportsLiveAssertMirror(t, ctx, db, pair[0], pair[1], "after the race "+pair[0])
	}
	var orphans int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_finding_supports s
LEFT JOIN fact_records f ON f.fact_id = s.fact_id AND NOT f.is_tombstone WHERE f.fact_id IS NULL`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("supports without a live fact = %d (err %v), want 0", orphans, err)
	}
}

// TestSupplyChainImpactSupportsBackfillWaitsForTheConflictDomainLockLive holds
// the writer's conflict-domain lock for the first pair in an open transaction,
// as a writer mid-pass does, and requires the backfill step to wait for it. A
// step that skipped the lock could write a support for a fact the writer is
// about to retract.
func TestSupplyChainImpactSupportsBackfillWaitsForTheConflictDomainLockLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	supportsBackfillSeed(t, ctx, db)
	var scope, generation string
	if err := db.QueryRowContext(ctx, `
SELECT scope_id, generation_id FROM fact_records
WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND NOT is_tombstone
ORDER BY scope_id, generation_id, fact_id LIMIT 1`).Scan(&scope, &generation); err != nil {
		t.Fatalf("first pair: %v", err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			_ = holder.Rollback()
		}
	}
	t.Cleanup(release)
	if _, err := holder.ExecContext(ctx, supplychaincore.SupplyChainImpactConflictDomainLockQuery,
		supplychaincore.SupplyChainImpactConflictDomainKey(scope, generation)); err != nil {
		t.Fatalf("hold the lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 100}.RunStep(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the backfill step finished (err=%v) while a writer held the pair's conflict-domain lock", err)
	case <-time.After(750 * time.Millisecond):
	}
	if waiting := replaceSetAdvisoryWaiters(t, ctx, db); waiting != 1 {
		t.Fatalf("ungranted advisory locks = %d, want 1 (the backfill waiting on the pair's lock)", waiting)
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("backfill step after the lock was released: %v", err)
	}
}
