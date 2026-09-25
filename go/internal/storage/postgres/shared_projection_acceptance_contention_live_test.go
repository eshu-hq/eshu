// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// acceptanceDeadlockTrials and acceptanceDeadlockKeys size the #6679
// reversed-batch contention proof.
const (
	acceptanceDeadlockTrials = 20
	acceptanceDeadlockKeys   = 200
)

// TestSharedIntentAcceptanceWriterReversedBatchesDoNotDeadlockLive runs two
// production writers concurrently over the same 200 acceptance keys, one
// with its intents in reverse key order. Acceptance rows are locked in one
// global primary-key order (Go sort plus the statement's ORDER BY), so the
// overlapping batches queue behind each other instead of deadlocking
// (SQLSTATE 40P01), and the newer generation always ends accepted.
func TestSharedIntentAcceptanceWriterReversedBatchesDoNotDeadlockLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	writer := NewSharedIntentAcceptanceWriter(SQLDB{DB: fixture.db})

	for trial := 0; trial < acceptanceDeadlockTrials; trial++ {
		runID := fmt.Sprintf("deadlock-%d", trial)
		forward := deadlockTrialIntents(fixture, runID, fixture.genNew)
		reversed := deadlockTrialIntents(fixture, runID, fixture.genOld)
		slices.Reverse(reversed)

		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, batch := range [][]reducer.SharedProjectionIntentRow{forward, reversed} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = writer.UpsertIntents(t.Context(), batch)
			}()
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
				t.Fatalf("trial %d writer %d deadlocked: %v", trial, i, err)
			}
			if err != nil {
				t.Fatalf("trial %d writer %d: %v", trial, i, err)
			}
		}

		var notNew int
		if err := fixture.db.QueryRowContext(t.Context(), `
SELECT count(*) FROM shared_projection_acceptance
WHERE scope_id = $1 AND source_run_id = $2 AND generation_id <> $3`,
			fixture.scopeID, runID, fixture.genNew,
		).Scan(&notNew); err != nil {
			t.Fatalf("trial %d count: %v", trial, err)
		}
		if notNew != 0 {
			t.Fatalf("trial %d: %d acceptance rows not on %q", trial, notNew, fixture.genNew)
		}
	}
	t.Logf("%d/%d reversed-batch trials completed with no 40P01 and every key on G_new",
		acceptanceDeadlockTrials, acceptanceDeadlockTrials)
}

func deadlockTrialIntents(fixture acceptanceMonotonicFixture, runID, generationID string) []reducer.SharedProjectionIntentRow {
	now := time.Now().UTC().Truncate(time.Microsecond)
	intents := make([]reducer.SharedProjectionIntentRow, 0, acceptanceDeadlockKeys)
	for key := 0; key < acceptanceDeadlockKeys; key++ {
		intent := staleWriteIntent(generationID, now)
		intent.IntentID = fmt.Sprintf("%s-%s-%03d", runID, generationID, key)
		intent.ScopeID = fixture.scopeID
		intent.AcceptanceUnitID = fmt.Sprintf("repository:6679-%03d", key)
		intent.RepositoryID = intent.AcceptanceUnitID
		intent.SourceRunID = runID
		intents = append(intents, intent)
	}
	return intents
}

// TestSharedProjectionAcceptanceOrdersByIngestedAtLive pins the ordering
// key: a generation observed earlier but ingested later is the one the
// projector activates (#6686), so it must win over one observed later but
// ingested earlier.
func TestSharedProjectionAcceptanceOrdersByIngestedAtLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	ctx := t.Context()
	base := time.Now().UTC().Truncate(time.Millisecond)
	const observedLateIngestedEarly = "gen-6679-observed-late"
	const observedEarlyIngestedLate = "gen-6679-ingested-late"
	for generationID, times := range map[string][2]time.Time{
		observedLateIngestedEarly: {base.Add(time.Hour), base.Add(-time.Hour)},
		observedEarlyIngestedLate: {base.Add(-time.Hour), base.Add(time.Hour)},
	} {
		mustExecAcceptanceFixture(t, fixture.db, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'manual', $3, $4, 'pending')`,
			generationID, fixture.scopeID, times[0], times[1])
	}

	store := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db})
	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row("run-order", observedEarlyIngestedLate, base)}); err != nil {
		t.Fatalf("Upsert(ingested-late) error = %v", err)
	}
	stale, err := store.UpsertReportingStale(ctx, []SharedProjectionAcceptance{fixture.row("run-order", observedLateIngestedEarly, base)})
	if err != nil {
		t.Fatalf("Upsert(observed-late) error = %v", err)
	}
	if got := fixture.accepted(t, "run-order"); got != observedEarlyIngestedLate || len(stale) != 1 {
		t.Fatalf("accepted = %q, stale = %d; want %q (later ingested_at wins) and 1 stale row",
			got, len(stale), observedEarlyIngestedLate)
	}
}

// TestSharedProjectionAcceptanceGenerationKeyBackfillLive applies the
// shipped 126 backfill to rows carrying a NULL ordering key (as a pre-#6679
// binary writes them) while another transaction holds one row locked: the
// backfill must fill every unlocked row, skip the locked one without waiting,
// and fill it on a rerun once released. A NULL-key row must still advance.
func TestSharedProjectionAcceptanceGenerationKeyBackfillLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var backfillSQL string
	for _, definition := range BootstrapDefinitions() {
		if strings.HasSuffix(definition.Path, "/126_shared_projection_acceptance_generation_key_backfill.sql") {
			backfillSQL = definition.SQL
		}
	}
	if backfillSQL == "" {
		t.Fatal("migration 126 backfill is not in BootstrapDefinitions()")
	}

	now := time.Now().UTC()
	for key := 0; key < 5; key++ {
		mustExecAcceptanceFixture(t, fixture.db, `
INSERT INTO shared_projection_acceptance
  (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at, generation_ingested_at)
VALUES ($1, $2, 'run-backfill', $3, $4, $4, NULL)`,
			fixture.scopeID, fmt.Sprintf("unit-%d", key), fixture.genOld, now)
	}

	holder, err := fixture.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx,
		`SELECT 1 FROM shared_projection_acceptance WHERE acceptance_unit_id = 'unit-0' FOR UPDATE`,
	); err != nil {
		t.Fatalf("lock unit-0: %v", err)
	}

	started := time.Now()
	if _, err := fixture.db.ExecContext(ctx, backfillSQL); err != nil {
		t.Fatalf("backfill with a held row: %v", err)
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Fatalf("backfill took %s with a held row; SKIP LOCKED must not wait", waited)
	}
	if got := backfillNullKeys(t, fixture); !slices.Equal(got, []string{"unit-0"}) {
		t.Fatalf("NULL keys after backfill = %v, want only the locked unit-0", got)
	}

	if err := holder.Rollback(); err != nil {
		t.Fatalf("release holder: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, backfillSQL); err != nil {
		t.Fatalf("backfill rerun: %v", err)
	}
	if got := backfillNullKeys(t, fixture); len(got) != 0 {
		t.Fatalf("NULL keys after rerun = %v, want none", got)
	}

	// A row still carrying a NULL key (skipped, or written by an old binary)
	// sorts older than any incoming generation and is advanced.
	mustExecAcceptanceFixture(t, fixture.db, `
UPDATE shared_projection_acceptance SET generation_id = $1, generation_ingested_at = NULL
WHERE acceptance_unit_id = 'unit-1'`, fixture.genNew)
	stale, err := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db}).UpsertReportingStale(ctx,
		[]SharedProjectionAcceptance{{
			ScopeID: fixture.scopeID, AcceptanceUnitID: "unit-1", SourceRunID: "run-backfill",
			GenerationID: fixture.genOld, AcceptedAt: now, UpdatedAt: now,
		}})
	if err != nil || len(stale) != 0 {
		t.Fatalf("NULL-key row upsert: stale = %d, err = %v; want advanced", len(stale), err)
	}
}

func backfillNullKeys(t *testing.T, fixture acceptanceMonotonicFixture) []string {
	t.Helper()
	rows, err := fixture.db.QueryContext(t.Context(), `
SELECT acceptance_unit_id FROM shared_projection_acceptance
WHERE source_run_id = 'run-backfill' AND generation_ingested_at IS NULL
ORDER BY acceptance_unit_id`)
	if err != nil {
		t.Fatalf("query NULL keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var units []string
	for rows.Next() {
		var unit string
		if err := rows.Scan(&unit); err != nil {
			t.Fatalf("scan NULL key: %v", err)
		}
		units = append(units, unit)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate NULL keys: %v", err)
	}
	return units
}

// TestSharedIntentAcceptanceWriterStaleWriteCounterLive drives the production
// writer against real Postgres: G_new applies, a late G_old is skipped and
// counted once, and a same-generation G_new retry applies without counting.
func TestSharedIntentAcceptanceWriterStaleWriteCounterLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	instruments, reader := newStaleWriteInstruments(t)
	writer := NewSharedIntentAcceptanceWriterWithInstruments(SQLDB{DB: fixture.db}, instruments)
	ctx := t.Context()
	base := time.Now().UTC().Truncate(time.Microsecond)

	steps := []struct {
		name       string
		generation string
		at         time.Time
		wantStale  int64
	}{
		{name: "G_new applies", generation: fixture.genNew, at: base, wantStale: 0},
		{name: "late G_old is skipped", generation: fixture.genOld, at: base.Add(time.Minute), wantStale: 1},
		{name: "same-generation retry applies", generation: fixture.genNew, at: base.Add(2 * time.Minute), wantStale: 1},
	}
	for _, step := range steps {
		intent := staleWriteIntent(step.generation, step.at)
		intent.ScopeID = fixture.scopeID
		if err := writer.UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{intent}); err != nil {
			t.Fatalf("%s: UpsertIntents() error = %v", step.name, err)
		}
		if got := staleWritesByDomain(t, reader)[reducer.DomainCodeCalls]; got != step.wantStale {
			t.Fatalf("%s: %s{domain=%s} = %d, want %d", step.name, staleWritesMetric, reducer.DomainCodeCalls, got, step.wantStale)
		}
		if got := fixture.accepted(t, "run-6679"); got != fixture.genNew {
			t.Fatalf("%s: accepted generation = %q, want %q", step.name, got, fixture.genNew)
		}
	}
}
