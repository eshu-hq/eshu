// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// acceptanceMonotonicRequiredEnv turns a missing DSN into a failure. The
// reducer contention gate, which enrolls these proofs, sets it to "1".
const acceptanceMonotonicRequiredEnv = "ESHU_REQUIRE_ACCEPTANCE_MONOTONIC_PROOF"

// acceptanceMonotonicTrials is the per-variant repetition count for the
// concurrent out-of-order writer proof (#6679).
const acceptanceMonotonicTrials = 50

// acceptanceMonotonicFixture is an isolated schema holding one scope with an
// older and a newer generation ordered by (observed_at, generation_id).
type acceptanceMonotonicFixture struct {
	db      *sql.DB
	scopeID string
	genOld  string
	genNew  string
}

// openAcceptanceMonotonicFixture bootstraps an isolated schema on
// ESHU_POSTGRES_TEST_DSN (or ESHU_POSTGRES_DSN) and seeds G_old < G_new for one scope.
func openAcceptanceMonotonicFixture(t *testing.T) acceptanceMonotonicFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		// The reducer contention gate sets the require flag so a renamed DSN
		// variable fails the lane instead of skipping these proofs there.
		if os.Getenv(acceptanceMonotonicRequiredEnv) == "1" {
			t.Fatalf("%s=1 but neither ESHU_POSTGRES_TEST_DSN nor ESHU_POSTGRES_DSN is set", acceptanceMonotonicRequiredEnv)
		}
		t.Skip("set ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN to run the #6679 acceptance monotonic proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	schema := fmt.Sprintf("eshu_6679_acceptance_%d", time.Now().UnixNano())
	adminDB := openActiveOCIWarningIndexProofDB(t, dsn)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
	})

	database, err := sql.Open("pgx", activeOCIWarningIndexSchemaDSN(t, dsn, schema))
	if err != nil {
		t.Fatalf("open proof database: %v", err)
	}
	database.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	fixture := acceptanceMonotonicFixture{
		db:      database,
		scopeID: "scope-6679",
		genOld:  "gen-6679-old",
		genNew:  "gen-6679-new",
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	mustExecAcceptanceFixture(t, database, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind,
   partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)`,
		fixture.scopeID, now)
	for generationID, observedAt := range map[string]time.Time{
		fixture.genOld: now.Add(-time.Hour),
		fixture.genNew: now,
	} {
		mustExecAcceptanceFixture(t, database, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'manual', $3, $3, 'pending')`,
			generationID, fixture.scopeID, observedAt)
	}
	return fixture
}

func mustExecAcceptanceFixture(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("exec fixture SQL: %v", err)
	}
}

func (f acceptanceMonotonicFixture) row(sourceRunID, generationID string, at time.Time) SharedProjectionAcceptance {
	return SharedProjectionAcceptance{
		ScopeID:          f.scopeID,
		AcceptanceUnitID: "repository:6679",
		SourceRunID:      sourceRunID,
		GenerationID:     generationID,
		AcceptedAt:       at,
		UpdatedAt:        at,
	}
}

func (f acceptanceMonotonicFixture) accepted(t *testing.T, sourceRunID string) string {
	t.Helper()
	generationID, found, err := NewSharedProjectionAcceptanceStore(SQLDB{DB: f.db}).
		Lookup(t.Context(), f.scopeID, "repository:6679", sourceRunID)
	if err != nil {
		t.Fatalf("Lookup(%s) error = %v", sourceRunID, err)
	}
	if !found {
		t.Fatalf("Lookup(%s) found no acceptance row", sourceRunID)
	}
	return generationID
}

// TestSharedProjectionAcceptanceSequentialStaleWriteLive proves a late write
// carrying an older generation cannot move the accepted generation backwards,
// while a same-generation retry still refreshes the row timestamps.
func TestSharedProjectionAcceptanceSequentialStaleWriteLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	store := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db})
	ctx := t.Context()
	base := time.Now().UTC().Truncate(time.Microsecond)

	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row("run-seq", fixture.genNew, base)}); err != nil {
		t.Fatalf("Upsert(G_new) error = %v", err)
	}
	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row("run-seq", fixture.genOld, base.Add(time.Minute))}); err != nil {
		t.Fatalf("Upsert(G_old) error = %v", err)
	}
	if got := fixture.accepted(t, "run-seq"); got != fixture.genNew {
		t.Fatalf("accepted generation after stale write = %q, want %q (acceptance moved backwards)", got, fixture.genNew)
	}

	retryAt := base.Add(2 * time.Minute)
	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row("run-seq", fixture.genNew, retryAt)}); err != nil {
		t.Fatalf("Upsert(G_new retry) error = %v", err)
	}
	var updatedAt time.Time
	if err := fixture.db.QueryRowContext(ctx,
		`SELECT updated_at FROM shared_projection_acceptance WHERE source_run_id = 'run-seq'`,
	).Scan(&updatedAt); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if !updatedAt.Equal(retryAt) {
		t.Fatalf("same-generation retry updated_at = %s, want refreshed %s", updatedAt, retryAt)
	}

	// Forward progress stays allowed: G_old then G_new advances.
	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row("run-fwd", fixture.genOld, base)}); err != nil {
		t.Fatalf("Upsert(G_old fwd) error = %v", err)
	}
	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row("run-fwd", fixture.genNew, base)}); err != nil {
		t.Fatalf("Upsert(G_new fwd) error = %v", err)
	}
	if got := fixture.accepted(t, "run-fwd"); got != fixture.genNew {
		t.Fatalf("accepted generation after forward write = %q, want %q", got, fixture.genNew)
	}
}

// TestSharedProjectionAcceptanceConcurrentOutOfOrderLive holds the first
// writer's upsert uncommitted, proves the second writer's conflicting upsert
// blocks on the row lock, then commits the first writer and lets the second
// proceed. Under READ COMMITTED the second writer's ON CONFLICT DO UPDATE
// WHERE is evaluated against the just-committed row version, so every
// variant must end on G_new regardless of which generation holds the lock.
func TestSharedProjectionAcceptanceConcurrentOutOfOrderLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)

	type variant struct {
		name        string
		preseed     bool
		firstIsNew  bool
		description string
	}
	variants := []variant{
		{name: "absent/new-holds-lock", firstIsNew: true, description: "G_old commits after G_new (speculative insert conflict)"},
		{name: "absent/old-holds-lock", firstIsNew: false, description: "G_new commits after G_old (speculative insert conflict)"},
		{name: "preseeded/new-holds-lock", preseed: true, firstIsNew: true, description: "G_old commits after G_new (update conflict)"},
		{name: "preseeded/old-holds-lock", preseed: true, firstIsNew: false, description: "G_new commits after G_old (update conflict)"},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			for trial := 0; trial < acceptanceMonotonicTrials; trial++ {
				runID := fmt.Sprintf("run-%s-%d", strings.ReplaceAll(v.name, "/", "-"), trial)
				if v.preseed {
					seedAt := time.Now().UTC()
					if err := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db}).Upsert(
						t.Context(), []SharedProjectionAcceptance{fixture.row(runID, fixture.genOld, seedAt)},
					); err != nil {
						t.Fatalf("trial %d preseed: %v", trial, err)
					}
				}
				firstGen, secondGen := fixture.genOld, fixture.genNew
				if v.firstIsNew {
					firstGen, secondGen = fixture.genNew, fixture.genOld
				}
				runLockWaitTrial(t, fixture, runID, firstGen, secondGen)
				if got := fixture.accepted(t, runID); got != fixture.genNew {
					t.Fatalf("trial %d (%s): accepted = %q, want %q", trial, v.description, got, fixture.genNew)
				}
			}
			t.Logf("%s: %d/%d trials ended on G_new", v.name, acceptanceMonotonicTrials, acceptanceMonotonicTrials)
		})
	}
}

// runLockWaitTrial runs one in-flight lock-wait interleaving: the first
// writer upserts without committing, the second writer's upsert is observed
// waiting on a lock, then the first commits and the second completes.
func runLockWaitTrial(t *testing.T, fixture acceptanceMonotonicFixture, runID, firstGen, secondGen string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	at := time.Now().UTC()

	first, err := fixture.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin first: %v", err)
	}
	defer func() { _ = first.Rollback() }()
	if err := NewSharedProjectionAcceptanceStore(SQLTx{Tx: first}).Upsert(
		ctx, []SharedProjectionAcceptance{fixture.row(runID, firstGen, at)},
	); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := fixture.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin second: %v", err)
	}
	defer func() { _ = second.Rollback() }()
	var secondPID int
	if err := second.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		t.Fatalf("second pid: %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- NewSharedProjectionAcceptanceStore(SQLTx{Tx: second}).Upsert(
			ctx, []SharedProjectionAcceptance{fixture.row(runID, secondGen, at.Add(time.Second))},
		)
	}()

	waitForLockWait(t, ctx, fixture.db, secondPID, secondDone)
	if err := first.Commit(); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if err := second.Commit(); err != nil {
		t.Fatalf("commit second: %v", err)
	}
}

// waitForLockWait polls pg_stat_activity until the given backend is waiting
// on a heavyweight lock, proving the second writer is blocked behind the
// first writer's uncommitted row.
func waitForLockWait(t *testing.T, ctx context.Context, database *sql.DB, pid int, done <-chan error) {
	t.Helper()
	for {
		select {
		case err := <-done:
			t.Fatalf("second writer finished before blocking on the row lock (err=%v)", err)
		default:
		}
		var waiting bool
		if err := database.QueryRowContext(ctx,
			`SELECT COALESCE(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid = $1`, pid,
		).Scan(&waiting); err != nil {
			t.Fatalf("poll lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("second writer never blocked on a lock: %v", ctx.Err())
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// TestSharedProjectionAcceptancePostSnapshotStoredGenerationLive is the
// review F1 interleaving (#6679): a multi-row upsert B blocks on its first
// key X while transaction C commits a generation and an acceptance row for
// B's second key Y, both after B's statement snapshot. When B resumes and
// conflicts on Y, the guard must compare against C's committed row itself,
// not against a scope_generations lookup pinned to B's older snapshot.
//
//   - newer-incoming: C commits an OLDER generation on Y; B carries the newer
//     one and must advance Y (a snapshot-bound guard wrongly keeps C's older
//     generation and reports B's write as stale).
//   - older-incoming (mirror): C commits a NEWER generation on Y; B carries
//     the older one and must be rejected as stale.
func TestSharedProjectionAcceptancePostSnapshotStoredGenerationLive(t *testing.T) {
	fixture := openAcceptanceMonotonicFixture(t)
	ctx := t.Context()

	var genNewIngestedAt time.Time
	if err := fixture.db.QueryRowContext(ctx,
		`SELECT ingested_at FROM scope_generations WHERE generation_id = $1`, fixture.genNew,
	).Scan(&genNewIngestedAt); err != nil {
		t.Fatalf("read G_new ingested_at: %v", err)
	}

	cases := []struct {
		name          string
		incomingGen   string
		lateOffset    time.Duration
		wantYIncoming bool
	}{
		{name: "newer-incoming", incomingGen: fixture.genNew, lateOffset: -time.Second, wantYIncoming: true},
		{name: "older-incoming", incomingGen: fixture.genOld, lateOffset: time.Hour, wantYIncoming: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for trial := 0; trial < 10; trial++ {
				runX := fmt.Sprintf("postsnap-%s-%d-x", tc.name, trial)
				runY := fmt.Sprintf("postsnap-%s-%d-y", tc.name, trial)
				lateGen := fmt.Sprintf("gen-6679-late-%s-%d", tc.name, trial)
				lateAt := genNewIngestedAt.Add(tc.lateOffset)
				stale := runPostSnapshotTrial(t, fixture, runX, runY, tc.incomingGen, lateGen, lateAt, false)

				gotY := fixture.accepted(t, runY)
				if tc.wantYIncoming {
					if gotY != tc.incomingGen || len(stale) != 0 {
						t.Fatalf("trial %d: Y = %q, stale = %d rows; want Y = %q and no stale rows "+
							"(a post-snapshot older generation must not block the newer write)",
							trial, gotY, len(stale), tc.incomingGen)
					}
					continue
				}
				if gotY != lateGen || len(stale) != 1 || stale[0].SourceRunID != runY {
					t.Fatalf("trial %d: Y = %q, stale = %+v; want Y = %q and stale = {Y}",
						trial, gotY, stale, lateGen)
				}
			}
			t.Logf("%s: held in all %d trials", tc.name, 10)
		})
	}
}

// runPostSnapshotTrial seeds X at the incoming generation, holds X in
// transaction L, starts B's production two-row upsert [X, Y] and waits until
// it blocks on X, then commits transaction C (a late generation plus Y at that
// generation) before releasing L. With legacyNullKey, C writes Y the way a
// pre-#6679 binary does, with no generation_ingested_at. It returns B's stale
// set.
func runPostSnapshotTrial(
	t *testing.T,
	fixture acceptanceMonotonicFixture,
	runX, runY, incomingGen, lateGen string,
	lateAt time.Time,
	legacyNullKey bool,
) []SharedProjectionAcceptance {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	at := time.Now().UTC()
	store := NewSharedProjectionAcceptanceStore(SQLDB{DB: fixture.db})
	if err := store.Upsert(ctx, []SharedProjectionAcceptance{fixture.row(runX, incomingGen, at)}); err != nil {
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
				fixture.row(runX, incomingGen, at.Add(time.Second)),
				fixture.row(runY, incomingGen, at.Add(time.Second)),
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
	if legacyNullKey {
		if _, err := late.ExecContext(ctx, `
INSERT INTO shared_projection_acceptance
  (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
VALUES ($1, 'repository:6679', $2, $3, $4, $4)`,
			fixture.scopeID, runY, lateGen, at,
		); err != nil {
			t.Fatalf("C legacy insert Y: %v", err)
		}
	} else if err := NewSharedProjectionAcceptanceStore(SQLTx{Tx: late}).Upsert(ctx,
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
