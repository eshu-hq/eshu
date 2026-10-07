// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// The live tests in this package run against a disposable PostgreSQL 18
// server. Point them at its administrative database:
//
//	ESHU_STATUS_SUMMARY_PROOF_DSN=postgres://user:pass@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE=1 \
//	go test ./internal/reducer/status/summary -run 'Live$' -count=1
//
// Every test creates its own database, applies the full schema bootstrap, and
// drops the database afterwards. With no DSN they skip; the
// live-postgres-readiness job sets the DSN and fails if an expected test did
// not run.

// fixtureScopes is the number of repository scopes the fixture spreads work
// across; every scope has one active generation.
const fixtureScopes = 24

const (
	// proofDSNEnv names the administrative DSN of the disposable server.
	proofDSNEnv = "ESHU_STATUS_SUMMARY_PROOF_DSN"
	// proofDisposableEnv must be 1 to allow creating and dropping databases.
	proofDisposableEnv = "ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE"
	// proofRequiredEnv turns an unset DSN from a skip into a failure. The
	// blocking reducer contention gate sets it, so a renamed DSN variable
	// cannot silently turn these proofs into skips in CI.
	proofRequiredEnv = "ESHU_REQUIRE_STATUS_SUMMARY_WRITER_PROOF"
)

// liveProofMissingDSNFails reports whether a live proof with no DSN must fail
// instead of skip: only when the required switch is exactly "1".
func liveProofMissingDSNFails(dsn, required string) bool {
	return strings.TrimSpace(dsn) == "" && strings.TrimSpace(required) == "1"
}

// openWriterDatabase returns a disposable database with every migration
// applied, migration 161 included.
func openWriterDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	if liveProofMissingDSNFails(os.Getenv(proofDSNEnv), os.Getenv(proofRequiredEnv)) {
		t.Fatalf("%s=1 but %s is unset: the status summary writer proof must run here", proofRequiredEnv, proofDSNEnv)
	}
	ctx, database := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv(proofDSNEnv),
		os.Getenv(proofDisposableEnv),
		5*time.Minute,
	)
	database.SetMaxOpenConns(16)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
		t.Fatalf("apply schema bootstrap: %v", err)
	}
	seedScopes(ctx, t, database)
	return ctx, database
}

// newLiveWriter returns the production-shaped writer: the storage package's
// active-work statement and digest on a transactional pool.
func newLiveWriter(database *sql.DB) *statussummary.Runner {
	return &statussummary.Runner{
		DB: postgres.SQLDB{DB: database},
		Statement: statussummary.Statement{
			ModelKey:     store.ModelActiveWorkSummary,
			SourceSHA256: postgres.ActiveWorkSummarySourceSHA256(),
			Compute:      postgres.ReadActiveWorkSummaryEntries,
		},
		Interval: statussummary.DefaultInterval,
	}
}

// seedScopes inserts fixtureScopes active repository scopes, each with one
// active generation, so no reducer row is hidden as stale.
func seedScopes(ctx context.Context, t *testing.T, database *sql.DB) {
	t.Helper()
	mustExec(ctx, t, database, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'scope-' || i, 'repository', 'git', 'scope-' || i, 'git', 'scope-' || i,
       now() - interval '3 hours', now() - interval '3 hours', 'active', 'gen-' || i, '{}'::jsonb
FROM generate_series(0, $1 - 1) AS i`, fixtureScopes)
	mustExec(ctx, t, database, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT 'gen-' || i, 'scope-' || i, 'snapshot', now() - interval '3 hours',
       now() - interval '3 hours', 'active', '{}'::jsonb
FROM generate_series(0, $1 - 1) AS i`, fixtureScopes)
}

// seedWork replaces every work item with total reducer rows, of which live
// are outstanding (pending, claimed with a live lease, retrying, failed, or
// dead-lettered, so every section of the statement has rows) and the rest
// succeeded, plus pending shared projection intents when live > 0.
func seedWork(ctx context.Context, t *testing.T, database *sql.DB, total, live int) {
	t.Helper()
	mustExec(ctx, t, database, `DELETE FROM fact_work_items`)
	mustExec(ctx, t, database, `DELETE FROM shared_projection_intents`)
	mustExec(ctx, t, database, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
  conflict_domain, conflict_key, status, attempt_count, lease_owner, claim_until,
  failure_class, failure_message, payload, created_at, updated_at)
SELECT 'w-' || i, 'scope-' || (i % $2), 'gen-' || (i % $2), 'reducer',
       (ARRAY['workload_identity', 'deployment_mapping', 'code_calls'])[1 + i % 3],
       'repo',
       -- Each block of six live rows shares one conflict key: two pending rows
       -- fenced by the block's one claimed row (the live-lease unique index
       -- allows one claimed row per key), so the blockage section has rows.
       CASE WHEN i < $3 AND i % 6 IN (0, 1, 2) THEN 'block-' || (i / 6) ELSE 'key-' || i END,
       CASE WHEN i < $3 THEN (ARRAY['pending', 'pending', 'claimed', 'retrying', 'failed', 'dead_letter'])[1 + i % 6]
            ELSE 'succeeded' END,
       CASE WHEN i < $3 AND i % 6 >= 3 THEN 2 ELSE 0 END,
       CASE WHEN i < $3 AND i % 6 = 2 THEN 'seed-owner' END,
       CASE WHEN i < $3 AND i % 6 = 2 THEN now() + interval '10 minutes' END,
       CASE WHEN i < $3 AND i % 6 >= 3 THEN 'seed_failure' END,
       CASE WHEN i < $3 AND i % 6 >= 3 THEN 'seeded failure ' || i END,
       '{}'::jsonb,
       now() - make_interval(secs => 600 + i),
       now() - make_interval(secs => i % 300)
FROM generate_series(0, $1 - 1) AS i`, total, fixtureScopes, live)
	if live == 0 {
		return
	}
	mustExec(ctx, t, database, `
INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key,
  repository_id, source_run_id, generation_id, payload, created_at)
SELECT 'intent-' || i, (ARRAY['code_calls', 'repo_dependency'])[1 + i % 2], 'p-' || i,
       'repo-' || i, 'run', 'gen-' || (i % $1), '{}'::jsonb, now() - make_interval(mins => i)
FROM generate_series(0, 7) AS i`, fixtureScopes)
}

// assertRowEqualsLive reads the stored row and recomputes the live statement
// at the row's as_of in one REPEATABLE READ snapshot, and fails unless they
// are equal tuple for tuple. It returns the row.
func assertRowEqualsLive(ctx context.Context, t *testing.T, database *sql.DB) store.Row {
	t.Helper()
	row, live := readRowAndLive(ctx, t, database)
	if row.SchemaVersion != store.SchemaVersion || row.RowCount != len(row.Entries) ||
		row.SourceSHA256 != postgres.ActiveWorkSummarySourceSHA256() {
		t.Fatalf("stored row version/count/digest = %d/%d/%s, want %d/%d/%s", row.SchemaVersion, row.RowCount,
			row.SourceSHA256, store.SchemaVersion, len(row.Entries), postgres.ActiveWorkSummarySourceSHA256())
	}
	if !reflect.DeepEqual(row.Entries, live) {
		t.Fatalf("stored row (%d entries) differs from the live statement at as_of %s (%d entries):\nstored: %v\nlive:   %v",
			len(row.Entries), row.AsOf, len(live), row.Entries, live)
	}
	return row
}

// readRowAndLive returns the stored row and the live statement result at the
// row's as_of, both from one REPEATABLE READ snapshot.
func readRowAndLive(ctx context.Context, t *testing.T, database *sql.DB) (store.Row, []store.Entry) {
	t.Helper()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatalf("begin snapshot: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	queryer := txQueryer{tx}
	row, err := store.Read(ctx, queryer, store.ModelActiveWorkSummary)
	if err != nil {
		t.Fatalf("read stored row: %v", err)
	}
	live, err := postgres.ReadActiveWorkSummaryEntries(ctx, queryer, row.AsOf)
	if err != nil {
		t.Fatalf("run the live statement at %s: %v", row.AsOf, err)
	}
	return row, live
}

// storedAsOf returns the stored row's as_of.
func storedAsOf(ctx context.Context, t *testing.T, database *sql.DB) time.Time {
	t.Helper()
	var asOf time.Time
	if err := database.QueryRowContext(ctx,
		`SELECT as_of FROM status_summary_snapshots WHERE model_key = $1`, store.ModelActiveWorkSummary).Scan(&asOf); err != nil {
		t.Fatalf("read stored as_of: %v", err)
	}
	return asOf
}

// summaryMigrationSQL returns the embedded migration 161, so a test that
// drops the table reinstalls it from the shipped file.
func summaryMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, definition := range migrations.BootstrapDefinitions() {
		if strings.HasSuffix(definition.Path, "/161_status_summary_snapshots.sql") {
			return definition.SQL
		}
	}
	t.Fatal("migration 161_status_summary_snapshots.sql is not embedded")
	return ""
}

func mustExec(ctx context.Context, t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec: %v\n%s", err, query)
	}
}

// txQueryer adapts a *sql.Tx to db.Queryer.
type txQueryer struct{ tx *sql.Tx }

func (q txQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.tx.QueryContext(ctx, query, args...)
}
