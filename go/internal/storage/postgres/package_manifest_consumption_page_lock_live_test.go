// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// pageLockProofPages is the number of 500-fact pages the #7663 lock proofs
// seed. It is well above the 20-page floor the issue asks for, so a lock that
// spans the whole scope is visibly different from one that spans one page.
const pageLockProofPages = 24

// TestPackageManifestConsumptionScopeRebuildReleasesScopeBetweenPagesLive is
// the #7663 lock-hold regression. It stalls the rebuild inside page 12 of a
// 24-page scope and, while it is stalled, proves three things: pages 1 to 11
// are already committed (so no transaction spans more than one page), a
// reducer-style fact insert for the same scope (whose foreign key takes FOR
// KEY SHARE on the ingestion_scopes row) completes, and an ingester-style
// scope-row update is still excluded.
func TestPackageManifestConsumptionScopeRebuildReleasesScopeBetweenPagesLive(t *testing.T) {
	ctx, database := openPackageManifestConsumptionPageProofDatabase(t)
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	seedPackageManifestConsumptionPageFacts(t, ctx, database, 0, pageLockProofPages*packageManifestConsumptionKeyBackfillBatchSize)

	const stalledPage = 12
	blocker := holdPackageManifestConsumptionPageKey(t, ctx, database, (stalledPage-1)*packageManifestConsumptionKeyBackfillBatchSize+7)
	done := make(chan error, 1)
	go func() { done <- BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}) }()
	waitForPackageManifestConsumptionKeyInsertWait(t, ctx, database)

	if got, want := countPackageManifestConsumptionKeys(t, ctx, database), (stalledPage-1)*packageManifestConsumptionKeyBackfillBatchSize; got != want {
		t.Errorf("committed sidecar keys while page %d is in flight = %d, want %d (each earlier page committed in its own transaction)", stalledPage, got, want)
	}
	if err := insertReducerStyleFact(ctx, database, "reducer-style-fact", time.Second); err != nil {
		t.Errorf("reducer-style fact insert for the scope while the rebuild is mid-scope: %v (want it to complete; the backfill must not block FOR KEY SHARE)", err)
	}
	if err := updateScopeRowLikeIngester(ctx, database, 200*time.Millisecond); !isPostgresLockNotAvailable(err) {
		t.Errorf("ingester-style scope-row update while a page is in flight = %v, want SQLSTATE 55P03 (the page must still exclude the sidecar writer)", err)
	}

	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release page blocker: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys() after releasing the page blocker: %v", err)
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionSidecarsMatchFacts(t, ctx, database)
}

// TestPackageManifestConsumptionScopeRebuildLockTimeoutLeavesScopeUnreadyLive
// proves the backfill gives up on a held scope row with SQLSTATE 55P03 within
// its lock_timeout instead of queueing indefinitely, and that the abort leaves
// no cursor advance, no readiness marker, and no partial sidecar state.
func TestPackageManifestConsumptionScopeRebuildLockTimeoutLeavesScopeUnreadyLive(t *testing.T) {
	ctx, database := openPackageManifestConsumptionPageProofDatabase(t)
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	seedPackageManifestConsumptionPageFacts(t, ctx, database, 0, 3)

	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin scope holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, `SELECT scope_id FROM ingestion_scopes WHERE scope_id = 'sidecar-live-scope' FOR UPDATE`); err != nil {
		t.Fatalf("hold scope row: %v", err)
	}

	err = runBackfillWithin(t, ctx, database, 15*time.Second, func() { _ = holder.Rollback() })
	if !isPostgresLockNotAvailable(err) {
		t.Fatalf("BackfillPackageManifestConsumptionKeys() behind a held scope row = %v, want SQLSTATE 55P03", err)
	}
	assertPackageManifestConsumptionBackfillCursor(t, ctx, database, "")
	assertPackageManifestConsumptionReady(t, ctx, database, false)
	if got := countPackageManifestConsumptionKeys(t, ctx, database); got != 0 {
		t.Fatalf("sidecar keys after a lock_timeout abort = %d, want 0", got)
	}

	if err := holder.Rollback(); err != nil {
		t.Fatalf("release scope holder: %v", err)
	}
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys() after the holder released: %v", err)
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionSidecarsMatchFacts(t, ctx, database)
}

// TestPackageManifestConsumptionScopeRebuildResumesAfterPageFailureAndCancelLive
// proves a failing page and a canceled pass roll back only the in-flight
// page, and that rerunning converges on the sidecar rows derived directly
// from the facts.
func TestPackageManifestConsumptionScopeRebuildResumesAfterPageFailureAndCancelLive(t *testing.T) {
	ctx, database := openPackageManifestConsumptionPageProofDatabase(t)
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	const pages = 10
	seedPackageManifestConsumptionPageFacts(t, ctx, database, 0, pages*packageManifestConsumptionKeyBackfillBatchSize)
	seedPackageManifestConsumptionRegistryFacts(t, ctx, database, 3)

	// A page that cannot take its key-row lock fails with 55P03; the pages
	// before it stay committed and the scope stays unready.
	failing := holdPackageManifestConsumptionPageKey(t, ctx, database, 5*packageManifestConsumptionKeyBackfillBatchSize+11)
	err := runBackfillWithin(t, ctx, database, 20*time.Second, func() { _ = failing.Rollback() })
	if !isPostgresLockNotAvailable(err) {
		t.Fatalf("BackfillPackageManifestConsumptionKeys() with page 6 blocked = %v, want SQLSTATE 55P03", err)
	}
	if got, want := countPackageManifestConsumptionKeys(t, ctx, database), 5*packageManifestConsumptionKeyBackfillBatchSize; got != want {
		t.Fatalf("committed sidecar keys after page 6 failed = %d, want %d (only the failing page rolls back)", got, want)
	}
	assertPackageManifestConsumptionBackfillCursor(t, ctx, database, "")
	assertPackageManifestConsumptionReady(t, ctx, database, false)
	if err := failing.Rollback(); err != nil {
		t.Fatalf("release failing-page blocker: %v", err)
	}

	// A canceled pass rolls back only the page it was in.
	canceling := holdPackageManifestConsumptionPageKey(t, ctx, database, 7*packageManifestConsumptionKeyBackfillBatchSize+3)
	passCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- BackfillPackageManifestConsumptionKeys(passCtx, SQLDB{DB: database}) }()
	waitForPackageManifestConsumptionKeyInsertWait(t, ctx, database)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("BackfillPackageManifestConsumptionKeys() after cancel = nil, want an error")
	}
	if got, want := countPackageManifestConsumptionKeys(t, ctx, database), 7*packageManifestConsumptionKeyBackfillBatchSize; got != want {
		t.Fatalf("committed sidecar keys after cancel in page 8 = %d, want %d", got, want)
	}
	assertPackageManifestConsumptionReady(t, ctx, database, false)
	if err := canceling.Rollback(); err != nil {
		t.Fatalf("release cancel blocker: %v", err)
	}

	for pass := 0; pass < 3; pass++ {
		if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
			t.Fatalf("rerun pass %d: %v", pass, err)
		}
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionSidecarsMatchFacts(t, ctx, database)
}

func openPackageManifestConsumptionPageProofDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	return ctx, database
}

// runBackfillWithin runs one pass and fails the test when it does not return
// within bound. release unblocks the pass so the goroutine can finish before
// the disposable database is dropped.
func runBackfillWithin(t *testing.T, ctx context.Context, database *sql.DB, bound time.Duration, release func()) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}) }()
	select {
	case err := <-done:
		return err
	case <-time.After(bound):
		release()
		err := <-done
		t.Fatalf("BackfillPackageManifestConsumptionKeys() did not return within %s while blocked (returned %v after release)", bound, err)
		return nil
	}
}

func packageManifestConsumptionPageFactID(index int) string {
	return fmt.Sprintf("page-fact-%06d", index)
}

func packageManifestConsumptionPagePackageName(index int) string {
	return fmt.Sprintf("package-%06d", index)
}

// seedPackageManifestConsumptionPageFacts bulk-inserts count manifest
// dependency facts without the sidecar-writer GUC, so the dirty trigger marks
// the scope exactly as an old writer would.
func seedPackageManifestConsumptionPageFacts(t *testing.T, ctx context.Context, database *sql.DB, from, count int) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'page-fact-' || lpad(i::text, 6, '0'), 'sidecar-live-scope', 'sidecar-live-generation',
       'content_entity', 'page-fact-' || lpad(i::text, 6, '0'), 'git', 'page-fact-' || lpad(i::text, 6, '0'),
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
           'repo_id', 'repository:sidecar-live',
           'entity_type', 'Variable',
           'entity_name', 'package-' || lpad(i::text, 6, '0'),
           'entity_metadata', jsonb_build_object('config_kind', 'dependency', 'package_manager', 'npm'))
FROM generate_series($1::int, $2::int) AS i`, from, from+count-1); err != nil {
		t.Fatalf("seed %d page facts: %v", count, err)
	}
}

func seedPackageManifestConsumptionRegistryFacts(t *testing.T, ctx context.Context, database *sql.DB, count int) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'zz-registry-' || i, 'sidecar-live-scope', 'sidecar-live-generation',
       'package_registry.package', 'zz-registry-' || i, 'npm_registry', 'zz-registry-' || i,
       clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object(
           'package_id', 'pkg:npm/registry-' || i, 'ecosystem', 'npm',
           'raw_name', 'Registry-' || i, 'normalized_name', 'registry-' || i)
FROM generate_series(1, $1::int) AS i`, count); err != nil {
		t.Fatalf("seed registry facts: %v", err)
	}
}

// holdPackageManifestConsumptionPageKey inserts, without committing, the
// sidecar key the rebuild will derive for the fact at index. The rebuild's
// INSERT ... ON CONFLICT for that key then waits on this transaction, which
// stalls the rebuild inside the page holding that fact.
func holdPackageManifestConsumptionPageKey(t *testing.T, ctx context.Context, database *sql.DB, index int) *sql.Tx {
	t.Helper()
	keys := packageManifestConsumptionKeyRows(packageManifestConsumptionPageEnvelopes(index))
	if len(keys) == 0 {
		t.Fatalf("no sidecar key derives from page fact %d", index)
	}
	key := keys[0]
	// A repair test starts from a built sidecar; drop the committed copy so the
	// uncommitted one below is the only row the rebuild can collide with.
	if _, err := database.ExecContext(ctx, `DELETE FROM package_manifest_consumption_keys WHERE fact_id = $1`, key.FactID); err != nil {
		t.Fatalf("clear committed page blocker key: %v", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin page blocker: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `
INSERT INTO package_manifest_consumption_keys
    (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
VALUES ($1, $2, $3, $4, $5, $6)`, key.FactID, key.ScopeID, key.GenerationID, key.RepositoryID, key.Ecosystem, key.PackageName); err != nil {
		t.Fatalf("insert page blocker key: %v", err)
	}
	return tx
}

func waitForPackageManifestConsumptionKeyInsertWait(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query LIKE '%INSERT INTO package_manifest_consumption_keys%'`).Scan(&waiting); err != nil {
			t.Fatalf("poll blocked sidecar insert: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("rebuild never blocked on the held page key")
}

// insertReducerStyleFact inserts a non-manifest fact for the scope, as a
// reducer writer does; its foreign keys take FOR KEY SHARE on the scope row.
func insertReducerStyleFact(ctx context.Context, database *sql.DB, factID string, lockTimeout time.Duration) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, fmt.Sprintf("%dms", lockTimeout.Milliseconds())); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'sidecar-live-scope', 'sidecar-live-generation', 'reducer_probe', $1,
        'reducer', $1, clock_timestamp(), clock_timestamp(), FALSE, '{}'::jsonb)`, factID); err != nil {
		return err
	}
	return tx.Commit()
}

// updateScopeRowLikeIngester runs the ingester's own upsertIngestionScopeQuery,
// the statement that takes the scope row lock before the ingester writes facts
// and sidecar keys under the sidecar-writer GUC.
func updateScopeRowLikeIngester(ctx context.Context, database *sql.DB, lockTimeout time.Duration) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`, fmt.Sprintf("%dms", lockTimeout.Milliseconds())); err != nil {
		return err
	}
	if err := upsertSidecarLiveScopeLikeIngester(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertSidecarLiveScopeLikeIngester(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC()
	_, err := tx.ExecContext(ctx, upsertIngestionScopeQuery,
		"sidecar-live-scope", "repository", "git", "repository:sidecar-live", nil, "git",
		"repository:sidecar-live", now, now, "active", "sidecar-live-generation", "{}")
	return err
}

func countPackageManifestConsumptionKeys(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var got int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM package_manifest_consumption_keys`).Scan(&got); err != nil {
		t.Fatalf("count sidecar keys: %v", err)
	}
	return got
}

func assertPackageManifestConsumptionBackfillCursor(t *testing.T, ctx context.Context, database *sql.DB, want string) {
	t.Helper()
	got, err := loadPackageManifestConsumptionKeyBackfillCursor(ctx, SQLDB{DB: database})
	if err != nil {
		t.Fatalf("load backfill cursor: %v", err)
	}
	if got != want {
		t.Fatalf("backfill cursor = %q, want %q (an aborted scope must not advance the cursor)", got, want)
	}
}
