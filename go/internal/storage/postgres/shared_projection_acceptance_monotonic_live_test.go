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
// ESHU_POSTGRES_TEST_DSN and seeds G_old < G_new for one scope.
func openAcceptanceMonotonicFixture(t *testing.T) acceptanceMonotonicFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the #6679 acceptance monotonic proof")
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
