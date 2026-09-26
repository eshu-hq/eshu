// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestSharedProjectionAcceptanceSameGenerationRetryKeepsHealedKeyLive pins the
// same-generation branch of the advance-only guard (#6679). The retry's
// statement snapshot predates the generation's scope_generations row, so the
// per-row key lookup yields NULL; the row already holds a filled key at that
// same generation (a concurrent writer committed both). The retry still
// applies, but it must not overwrite the healed key with NULL.
func TestSharedProjectionAcceptanceSameGenerationRetryKeepsHealedKeyLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	lateAt := generationIngestedAt(t, fixture, fixture.genNew).Add(-time.Second)

	for trial := 0; trial < 5; trial++ {
		runX := fmt.Sprintf("healed-%d-x", trial)
		runY := fmt.Sprintf("healed-%d-y", trial)
		lateGen := fmt.Sprintf("gen-6679-healed-%d", trial)
		stale := runInvisibleGenerationRetryTrial(t, fixture, runX, runY, lateGen, lateAt)
		gen, key, keyed := storedGenerationKey(t, fixture, runY)
		if gen != lateGen || len(stale) != 0 || !keyed || !key.Equal(lateAt) {
			t.Fatalf("trial %d: Y = %q, stale = %d, key = %v (valid=%v); want %q kept, 0 stale, healed key %v",
				trial, gen, len(stale), key, keyed, lateGen, lateAt)
		}
	}
}

// runInvisibleGenerationRetryTrial holds a batch [X, Y] blocked on X's row
// lock, then commits lateGen and a filled-key Y row at lateGen. The batch's
// snapshot cannot see lateGen, so its incoming key for Y is NULL when it
// resumes into the same-generation conflict on Y.
func runInvisibleGenerationRetryTrial(
	t *testing.T,
	fixture acceptanceMonotonicFixture,
	runX, runY, lateGen string,
	lateAt time.Time,
) []SharedProjectionAcceptance {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	at := time.Now().UTC()
	store := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db})
	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row(runX, fixture.genNew, at)}); err != nil {
		t.Fatalf("seed X: %v", err)
	}

	holder, err := fixture.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin L: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx,
		`UPDATE shared_projection_acceptance SET updated_at = now() WHERE scope_id = $1 AND source_run_id = $2`,
		fixture.scopeID, runX,
	); err != nil {
		t.Fatalf("L lock X: %v", err)
	}

	upserter, err := fixture.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin B: %v", err)
	}
	defer func() { _ = upserter.Rollback() }()
	var upserterPID int
	if err := upserter.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&upserterPID); err != nil {
		t.Fatalf("B pid: %v", err)
	}
	type upsertOutcome struct {
		stale []SharedProjectionAcceptance
		err   error
	}
	done := make(chan upsertOutcome, 1)
	go func() {
		stale, err := NewSharedProjectionAcceptanceStore(SQLTx{Tx: upserter}).UpsertReportingStale(ctx,
			[]SharedProjectionAcceptance{
				fixture.row(runX, fixture.genNew, at.Add(time.Second)),
				fixture.row(runY, lateGen, at.Add(time.Second)),
			})
		done <- upsertOutcome{stale: stale, err: err}
	}()
	neverDone := make(chan error) // B reports through done; a stuck wait times out via ctx
	waitForLockWait(t, ctx, fixture.db, upserterPID, neverDone)

	late, err := fixture.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin C: %v", err)
	}
	defer func() { _ = late.Rollback() }()
	if _, err := late.ExecContext(ctx, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'manual', $3, $3, 'pending')`,
		lateGen, fixture.scopeID, lateAt,
	); err != nil {
		t.Fatalf("C insert late generation: %v", err)
	}
	if err := NewSharedProjectionAcceptanceStore(SQLTx{Tx: late}).Upsert(ctx,
		[]SharedProjectionAcceptance{fixture.row(runY, lateGen, at)},
	); err != nil {
		t.Fatalf("C upsert Y: %v", err)
	}
	if err := late.Commit(); err != nil {
		t.Fatalf("commit C: %v", err)
	}

	if err := holder.Commit(); err != nil {
		t.Fatalf("commit L: %v", err)
	}
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("B upsert: %v", outcome.err)
	}
	if err := upserter.Commit(); err != nil {
		t.Fatalf("commit B: %v", err)
	}
	return outcome.stale
}
