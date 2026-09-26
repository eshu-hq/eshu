// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
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

// setLegacyNullKey rewrites one acceptance row the way a pre-#6679 binary
// leaves it: at generationID with no generation_ingested_at.
func setLegacyNullKey(t *testing.T, fixture acceptanceMonotonicFixture, sourceRunID, generationID string) {
	t.Helper()
	now := time.Now().UTC()
	mustExecAcceptanceFixture(t, fixture.db, `
INSERT INTO shared_projection_acceptance
  (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at, generation_ingested_at)
VALUES ($1, 'repository:6679', $2, $3, $4, $4, NULL)
ON CONFLICT (scope_id, acceptance_unit_id, source_run_id) DO UPDATE
SET generation_id = EXCLUDED.generation_id, generation_ingested_at = NULL`,
		fixture.scopeID, sourceRunID, generationID, now)
}

// storedGenerationKey reads a row's generation and ordering key; valid is
// false when the key is NULL.
func storedGenerationKey(t *testing.T, fixture acceptanceMonotonicFixture, sourceRunID string) (string, time.Time, bool) {
	t.Helper()
	var generationID string
	var key sql.NullTime
	if err := fixture.db.QueryRowContext(t.Context(), `
SELECT generation_id, generation_ingested_at FROM shared_projection_acceptance
WHERE scope_id = $1 AND acceptance_unit_id = 'repository:6679' AND source_run_id = $2`,
		fixture.scopeID, sourceRunID,
	).Scan(&generationID, &key); err != nil {
		t.Fatalf("read %s: %v", sourceRunID, err)
	}
	return generationID, key.Time, key.Valid
}

func generationIngestedAt(t *testing.T, fixture acceptanceMonotonicFixture, generationID string) time.Time {
	t.Helper()
	var ingestedAt time.Time
	if err := fixture.db.QueryRowContext(t.Context(),
		`SELECT ingested_at FROM scope_generations WHERE generation_id = $1`, generationID,
	).Scan(&ingestedAt); err != nil {
		t.Fatalf("read %s ingested_at: %v", generationID, err)
	}
	return ingestedAt
}

// TestSharedProjectionAcceptanceLegacyNullKeyRejectsStaleLive covers rows a
// pre-#6679 binary wrote (no generation_ingested_at; migration 125 adds the
// column without a backfill). The guard must resolve the stored generation's
// key from scope_generations instead of treating NULL as "always older", so a
// late older write still cannot roll the row back; a same-generation retry
// applies and fills the key.
func TestSharedProjectionAcceptanceLegacyNullKeyRejectsStaleLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	store := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db})
	ctx := t.Context()
	now := time.Now().UTC()

	t.Run("older-incoming", func(t *testing.T) {
		setLegacyNullKey(t, fixture, "legacy-stale", fixture.genNew)
		stale, err := store.UpsertReportingStale(ctx, []SharedProjectionAcceptance{fixture.row("legacy-stale", fixture.genOld, now)})
		if err != nil {
			t.Fatalf("UpsertReportingStale() error = %v", err)
		}
		gen, _, keyed := storedGenerationKey(t, fixture, "legacy-stale")
		if gen != fixture.genNew || len(stale) != 1 {
			t.Fatalf("legacy row: generation = %q, stale = %d; want %q kept and 1 stale write "+
				"(a NULL stored key must not let an older generation win)", gen, len(stale), fixture.genNew)
		}
		if keyed {
			t.Fatal("a rejected stale write must leave the legacy row untouched, key still NULL")
		}
	})

	t.Run("same-generation-fills-key", func(t *testing.T) {
		setLegacyNullKey(t, fixture, "legacy-same", fixture.genNew)
		stale, err := store.UpsertReportingStale(ctx, []SharedProjectionAcceptance{fixture.row("legacy-same", fixture.genNew, now)})
		if err != nil {
			t.Fatalf("UpsertReportingStale() error = %v", err)
		}
		gen, key, keyed := storedGenerationKey(t, fixture, "legacy-same")
		want := generationIngestedAt(t, fixture, fixture.genNew)
		if gen != fixture.genNew || len(stale) != 0 || !keyed || !key.Equal(want) {
			t.Fatalf("same-generation retry: generation = %q, stale = %d, key = %v (valid=%v); want %q, 0 stale, key %v",
				gen, len(stale), key, keyed, fixture.genNew, want)
		}
	})
}

// TestSharedProjectionAcceptanceLegacyNullKeyAdvancesLive proves a legacy row
// still moves forward: a newer generation replaces it and fills the key.
func TestSharedProjectionAcceptanceLegacyNullKeyAdvancesLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	setLegacyNullKey(t, fixture, "legacy-advance", fixture.genOld)

	stale, err := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db}).UpsertReportingStale(t.Context(),
		[]SharedProjectionAcceptance{fixture.row("legacy-advance", fixture.genNew, time.Now().UTC())})
	if err != nil {
		t.Fatalf("UpsertReportingStale() error = %v", err)
	}
	gen, key, keyed := storedGenerationKey(t, fixture, "legacy-advance")
	want := generationIngestedAt(t, fixture, fixture.genNew)
	if gen != fixture.genNew || len(stale) != 0 || !keyed || !key.Equal(want) {
		t.Fatalf("legacy advance: generation = %q, stale = %d, key = %v (valid=%v); want %q, 0 stale, key %v",
			gen, len(stale), key, keyed, fixture.genNew, want)
	}
}

// TestSharedProjectionAcceptanceLegacyNullKeyInvisibleGenerationAdvancesLive
// is the fallback case: an old-binary writer commits a legacy (NULL-key) row
// at a generation ingested after B's statement snapshot, while B is blocked
// on an earlier key. B's lookup of that generation finds nothing; without the
// '-infinity' fallback the row comparison is NULL and B's newer write would be
// dropped. B must advance, as it would have before #6679.
func TestSharedProjectionAcceptanceLegacyNullKeyInvisibleGenerationAdvancesLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	lateAt := generationIngestedAt(t, fixture, fixture.genNew).Add(-time.Second)

	for trial := 0; trial < 5; trial++ {
		runX := fmt.Sprintf("invisible-%d-x", trial)
		runY := fmt.Sprintf("invisible-%d-y", trial)
		lateGen := fmt.Sprintf("gen-6679-invisible-%d", trial)
		stale := runPostSnapshotTrial(t, fixture, runX, runY, fixture.genNew, lateGen, lateAt, true)
		gen, key, keyed := storedGenerationKey(t, fixture, runY)
		want := generationIngestedAt(t, fixture, fixture.genNew)
		if gen != fixture.genNew || len(stale) != 0 || !keyed || !key.Equal(want) {
			t.Fatalf("trial %d: Y = %q, stale = %d, key = %v (valid=%v); want %q advanced with key %v",
				trial, gen, len(stale), key, keyed, fixture.genNew, want)
		}
	}
	t.Logf("legacy row at a post-snapshot generation advanced to G_new in all %d trials", 5)
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
