// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"

	factkeys "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/keys"
)

const (
	factKeyConcurrencyScope  = "keys-conc"
	factKeyConcurrencyOld    = "keys-conc-old"
	factKeyConcurrencyActive = "keys-conc-active"
)

// seedFactKeyConcurrency seeds one scope with a superseded generation and
// its active generation, each with facts and keys written by the production
// key writers, in a migrated schema whose per-fact foreign keys are dropped
// (the #7420 end state).
func seedFactKeyConcurrency(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	dropFactKeyForeignKeys(t, ctx, database)
	testfixtures.SeedScope(t, ctx, database, factKeyConcurrencyScope)
	testfixtures.SeedSupersededGeneration(t, ctx, database, factKeyConcurrencyScope, factKeyConcurrencyOld, time.Now().Add(-10*24*time.Hour))
	exec := SQLDB{DB: database}
	writeFactsWithKeys(t, ctx, exec, factKeyEnvelopes(factKeyConcurrencyScope, factKeyConcurrencyOld, 20))
	writeFactsWithKeys(t, ctx, exec, factKeyEnvelopes(factKeyConcurrencyScope, factKeyConcurrencyActive, 20))
	if _, err := database.ExecContext(ctx, `DELETE FROM package_manifest_consumption_key_dirty_scopes`); err != nil {
		t.Fatalf("clear dirty scope marks: %v", err)
	}
	return database, ctx
}

func factKeyCounts(t *testing.T, ctx context.Context, database *sql.DB, generationID string) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, len(factKeyForeignKeys))
	for table := range factKeyForeignKeys {
		counts[table] = factKeyRowsFor(t, ctx, database, table, factKeyConcurrencyScope, generationID)
	}
	return counts
}

// waitForLockWaiter polls until another backend of this database waits on a
// heavyweight lock, the proof that a concurrent statement really overlapped.
func waitForLockWaiter(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiters int
		if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&waiters); err != nil {
			t.Fatalf("poll lock waiters: %v", err)
		}
		if waiters > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no backend waited on a lock: the overlap did not happen")
}

func deadlockCount(t *testing.T, ctx context.Context, database *sql.DB) int64 {
	t.Helper()
	var deadlocks int64
	if err := database.QueryRowContext(ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`).Scan(&deadlocks); err != nil {
		t.Fatalf("read deadlock count: %v", err)
	}
	return deadlocks
}

// TestRetentionFactKeyDeletesBesideKeyWriterLive is #7420 condition (e),
// first half: the retention key deletes for the superseded generation hold
// their row locks while an ingestion-shaped writer (facts, then the three
// production key writers, under a 2 s lock_timeout) commits keys for the
// same scope's active generation. The writer never waits, both commits keep
// their rows, a rolled-back attempt changes nothing, and a retry after the
// commit deletes nothing twice.
func TestRetentionFactKeyDeletesBesideKeyWriterLive(t *testing.T) {
	database, ctx := seedFactKeyConcurrency(t)
	oldBefore := factKeyCounts(t, ctx, database, factKeyConcurrencyOld)
	activeBefore := factKeyCounts(t, ctx, database, factKeyConcurrencyActive)
	run := func(commit bool) map[string]int64 {
		t.Helper()
		tx, err := SQLDB{DB: database}.Begin(ctx)
		if err != nil {
			t.Fatalf("begin retention: %v", err)
		}
		result := GenerationRetentionResult{RowsPruned: map[string]int64{}}
		if err := factkeys.DeleteGenerations(ctx, tx, []string{factKeyConcurrencyScope}, []string{factKeyConcurrencyOld}, result.RowsPruned); err != nil {
			t.Fatalf("DeleteGenerations(): %v", err)
		}
		if commit {
			if err := tx.Commit(); err != nil {
				t.Fatalf("commit retention: %v", err)
			}
		} else if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback retention: %v", err)
		}
		return result.RowsPruned
	}

	// A rolled-back attempt changes nothing.
	if got := run(false); got["relationship_reference_candidate_keys"] != oldBefore["relationship_reference_candidate_keys"] {
		t.Fatalf("rolled-back attempt deleted %v, want %v", got, oldBefore)
	}
	if got := factKeyCounts(t, ctx, database, factKeyConcurrencyOld); got["package_manifest_consumption_keys"] != oldBefore["package_manifest_consumption_keys"] {
		t.Fatalf("keys after rollback = %v, want %v", got, oldBefore)
	}

	retention, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin retention: %v", err)
	}
	defer func() { _ = retention.Rollback() }()
	result := GenerationRetentionResult{RowsPruned: map[string]int64{}}
	if err := factkeys.DeleteGenerations(ctx, retention, []string{factKeyConcurrencyScope}, []string{factKeyConcurrencyOld}, result.RowsPruned); err != nil {
		t.Fatalf("DeleteGenerations(): %v", err)
	}

	writer, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin writer: %v", err)
	}
	if _, err := writer.ExecContext(ctx, "SET LOCAL lock_timeout = '2s'"); err != nil {
		t.Fatalf("set writer lock_timeout: %v", err)
	}
	fresh := factKeyEnvelopes(factKeyConcurrencyScope, factKeyConcurrencyActive, 5)
	for i := range fresh {
		fresh[i].FactID += "-writer"
	}
	writeFactsWithKeys(t, ctx, writer, fresh)
	if err := writer.Commit(); err != nil {
		t.Fatalf("writer commit while retention holds its key deletes: %v", err)
	}
	if err := retention.Commit(); err != nil {
		t.Fatalf("retention commit: %v", err)
	}

	for table, rows := range oldBefore {
		if result.RowsPruned[table] != rows {
			t.Errorf("RowsPruned[%s] = %d, want %d", table, result.RowsPruned[table], rows)
		}
	}
	activeAfter := factKeyCounts(t, ctx, database, factKeyConcurrencyActive)
	for table, rows := range factKeyCounts(t, ctx, database, factKeyConcurrencyOld) {
		if rows != 0 {
			t.Errorf("superseded %s rows = %d, want 0", table, rows)
		}
		if activeAfter[table] != activeBefore[table]+activeBefore[table]/4 {
			t.Errorf("active %s rows = %d, want %d kept plus the writer's %d", table, activeAfter[table], activeBefore[table], activeBefore[table]/4)
		}
	}
	// A retry after the commit deletes nothing twice.
	for table, rows := range run(true) {
		if rows != 0 {
			t.Errorf("retry deleted %d %s rows, want 0", rows, table)
		}
	}
}

// TestRetentionFactKeyDeletesBesideBackfillRebuildLive is #7420 condition
// (e), second half, against the production package key scope rebuild, which
// clears manifest keys then registry keys by scope. In the retention shape
// (scope row locked first, keys, then the generation delete) the rebuild
// waits on the scope lock and rebuilds the remaining generation after the
// commit. With no outer scope lock at all, the retention order
// (relationship, manifest, registry) matches the rebuild's manifest-then-
// registry order, so the rebuild waits on retention's manifest rows and
// neither side deadlocks. The seeded RED reverses the retention order
// (registry keys before manifest keys) and must deadlock, which proves the
// interleave really reaches the rows both writers lock.
func TestRetentionFactKeyDeletesBesideBackfillRebuildLive(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scopeLock bool
	}{
		{"retention-shape-with-scope-lock", true},
		{"child-order-only-no-scope-lock", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, ctx := seedFactKeyConcurrency(t)
			activeBefore := factKeyCounts(t, ctx, database, factKeyConcurrencyActive)
			deadlocksBefore := deadlockCount(t, ctx, database)
			retention, err := SQLDB{DB: database}.Begin(ctx)
			if err != nil {
				t.Fatalf("begin retention: %v", err)
			}
			defer func() { _ = retention.Rollback() }()
			if tc.scopeLock {
				execRetention(t, ctx, retention, `SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR UPDATE`, factKeyConcurrencyScope)
			}
			for _, keys := range factkeys.Deletes()[:2] {
				execRetention(t, ctx, retention, keys.Generation, factKeyConcurrencyScope, factKeyConcurrencyOld)
			}
			rebuildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			rebuilt := make(chan error, 1)
			go func() {
				rebuilt <- rebuildPackageManifestConsumptionKeyScope(rebuildCtx, SQLDB{DB: database}, factKeyConcurrencyScope, false)
			}()
			waitForLockWaiter(t, ctx, database)
			execRetention(t, ctx, retention, factkeys.Deletes()[2].Generation, factKeyConcurrencyScope, factKeyConcurrencyOld)
			if tc.scopeLock {
				execRetention(t, ctx, retention, deleteScopeGenerationsForRetentionQuery, []string{factKeyConcurrencyOld})
			}
			if err := retention.Commit(); err != nil {
				t.Fatalf("retention commit: %v", err)
			}
			if err := <-rebuilt; err != nil {
				t.Fatalf("rebuild beside retention: %v", err)
			}
			if got := deadlockCount(t, ctx, database); got != deadlocksBefore {
				t.Fatalf("deadlocks = %d, want %d", got, deadlocksBefore)
			}
			if !tc.scopeLock {
				return
			}
			assertNoOrphanFactKeys(t, ctx, database, "after retention beside the rebuild")
			for table, rows := range factKeyCounts(t, ctx, database, factKeyConcurrencyActive) {
				if rows != activeBefore[table] {
					t.Errorf("active %s rows = %d, want %d rebuilt", table, rows, activeBefore[table])
				}
			}
		})
	}
}

func execRetention(t *testing.T, ctx context.Context, tx db.Transaction, statement string, args ...any) {
	t.Helper()
	if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
		t.Fatalf("retention statement: %v", err)
	}
}

// TestRetentionFactKeyReversedOrderDeadlocksLive is the seeded RED for the
// lock order above: deleting registry keys before manifest keys, with no
// outer scope lock, deadlocks against the rebuild's manifest-then-registry
// scope clear. PostgreSQL aborts one side with SQLSTATE 40P01.
func TestRetentionFactKeyReversedOrderDeadlocksLive(t *testing.T) {
	database, ctx := seedFactKeyConcurrency(t)
	retention, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin retention: %v", err)
	}
	defer func() { _ = retention.Rollback() }()
	execRetention(t, ctx, retention, factkeys.Deletes()[2].Generation, factKeyConcurrencyScope, factKeyConcurrencyOld)
	rebuildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rebuilt := make(chan error, 1)
	go func() {
		rebuilt <- rebuildPackageManifestConsumptionKeyScope(rebuildCtx, SQLDB{DB: database}, factKeyConcurrencyScope, false)
	}()
	waitForLockWaiter(t, ctx, database)
	_, retentionErr := retention.ExecContext(ctx, factkeys.Deletes()[1].Generation, factKeyConcurrencyScope, factKeyConcurrencyOld)
	if retentionErr != nil {
		_ = retention.Rollback()
	} else {
		_ = retention.Commit()
	}
	rebuildErr := <-rebuilt
	deadlocked := func(err error) bool { return err != nil && strings.Contains(err.Error(), "40P01") }
	if !deadlocked(retentionErr) && !deadlocked(rebuildErr) {
		t.Fatalf("reversed order: retention error %v, rebuild error %v; want one side aborted with SQLSTATE 40P01", retentionErr, rebuildErr)
	}
	t.Logf("reversed order deadlocked as expected: retention error %v, rebuild error %v", retentionErr, rebuildErr)
}
