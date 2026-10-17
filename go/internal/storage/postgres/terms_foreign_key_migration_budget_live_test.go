// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// termsForeignKeyDropDefinition is migration 191 (#7427 PR3) as the tracked
// migrator applies it.
func termsForeignKeyDropDefinition(t *testing.T) Definition {
	t.Helper()
	for _, candidate := range BootstrapDefinitions() {
		if candidate.Name == "eshu_search_index_terms_drop_generation_fk" {
			return candidate
		}
	}
	t.Fatal("no bootstrap definition named eshu_search_index_terms_drop_generation_fk")
	return Definition{}
}

// countTermsGenerationForeignKeys counts the keys from the terms parent or
// any leaf to scope_generations.
func countTermsGenerationForeignKeys(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_constraint AS constraint_row
WHERE constraint_row.contype = 'f'
  AND constraint_row.confrelid = to_regclass('scope_generations')
  AND (constraint_row.conrelid = to_regclass('eshu_search_index_terms')
       OR constraint_row.conrelid IN (
           SELECT inhrelid FROM pg_inherits WHERE inhparent = to_regclass('eshu_search_index_terms')))`).Scan(&count); err != nil {
		t.Fatalf("count terms generation foreign keys: %v", err)
	}
	return count
}

// migrationLedgerHas reports whether the tracked migrator recorded path.
func migrationLedgerHas(t *testing.T, ctx context.Context, database *sql.DB, path string) bool {
	t.Helper()
	var recorded bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM eshu_schema_migrations WHERE path = $1)`,
		path).Scan(&recorded); err != nil {
		t.Fatalf("read migration ledger: %v", err)
	}
	return recorded
}

// TestTermsForeignKeyDropFailsCleanlyWhenItsLockStaysBusyLive is migration
// 191's budget exhaustion path, through the tracked migrator with a short
// lock_timeout and retry budget. A holder keeps one of the two tables the
// drop needs for longer than the whole budget: the terms parent (the drop's
// NOWAIT fails at once) or scope_generations (the drop's wait reaches
// lock_timeout). Each time the run fails with a 55P03 error that names the
// migration and the budget, logs the lock_wait warning, leaves all 65 keys in
// place (no partial drop), leaves no relation lock on its connection, and
// records nothing in the ledger, so a later run after the holder ends applies
// the migration.
func TestTermsForeignKeyDropFailsCleanlyWhenItsLockStaysBusyLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	database.SetMaxOpenConns(4)
	definition := termsForeignKeyDropDefinition(t)

	for _, held := range []struct {
		name, lock string
		names      string
	}{
		// NOWAIT on the parent: refused at once, naming the relation.
		{
			"terms parent held", "LOCK TABLE eshu_search_index_terms IN ACCESS SHARE MODE",
			`could not obtain lock on relation "eshu_search_index_terms"`,
		},
		// The bounded wait for scope_generations reaches lock_timeout.
		{
			"scope_generations held", "LOCK TABLE scope_generations IN ROW SHARE MODE",
			"canceling statement due to lock timeout",
		},
	} {
		t.Run(held.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `ALTER TABLE eshu_search_index_terms ADD CONSTRAINT eshu_search_index_terms_generation_id_fkey
FOREIGN KEY (generation_id) REFERENCES scope_generations(generation_id) ON DELETE CASCADE`); err != nil {
				t.Fatalf("restore the terms foreign key: %v", err)
			}
			if _, err := database.ExecContext(ctx, `DELETE FROM eshu_schema_migrations WHERE path = $1`, definition.Path); err != nil {
				t.Fatalf("forget %s in the ledger: %v", definition.Path, err)
			}
			holder, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin holder: %v", err)
			}
			defer func() { _ = holder.Rollback() }()
			if _, err := holder.ExecContext(ctx, held.lock); err != nil {
				t.Fatalf("holder: %v", err)
			}

			conn, err := database.Conn(ctx)
			if err != nil {
				t.Fatalf("open migrator connection: %v", err)
			}
			defer func() { _ = conn.Close() }()
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			executor := schemaConnectionExecutor{database: SQLDB{DB: database}, conn: conn, logger: logger}
			started := time.Now()
			err = executor.applyTrackedDefinitions(ctx, []Definition{definition}, schemaStatementBounds{
				lockTimeout:     200 * time.Millisecond,
				lockRetryBudget: 1500 * time.Millisecond,
			}, logger)
			elapsed := time.Since(started)
			if err == nil {
				t.Fatal("migration 191 with its lock held for the whole budget succeeded, want a clean failure")
			}
			for _, want := range []string{"apply " + definition.Name, "lock retry budget", definition.Path, held.names, "55P03"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("bootstrap error %q does not name %q", err, want)
				}
			}
			if !coordination.IsLockNotAvailable(err) {
				t.Errorf("bootstrap error lost its 55P03 classification: %v", err)
			}
			if elapsed > 10*time.Second {
				t.Errorf("exhausted bootstrap took %s, want it bounded by the 1.5 s budget plus one backoff", elapsed)
			}
			if !strings.Contains(logs.String(), "event_name=bootstrap.postgres.migration.lock_wait") {
				t.Errorf("migrator logged no lock_wait warning before failing:\n%s", logs.String())
			}
			if migrationLedgerHas(t, ctx, database, definition.Path) {
				t.Error("failed migration 191 was recorded in the ledger; the next run would skip it")
			}
			if got := countTermsGenerationForeignKeys(t, ctx, database); got != 65 {
				t.Errorf("terms foreign keys after the failed run = %d, want all 65 (no partial drop)", got)
			}
			var leftLocks int
			if err := conn.QueryRowContext(ctx, `
SELECT count(*) FROM pg_locks AS lock_row
WHERE lock_row.pid = pg_backend_pid() AND lock_row.locktype = 'relation'
  AND (lock_row.relation IN (to_regclass('eshu_search_index_terms'), to_regclass('scope_generations'))
       OR lock_row.relation IN (SELECT inhrelid FROM pg_inherits WHERE inhparent = to_regclass('eshu_search_index_terms')))`).Scan(&leftLocks); err != nil {
				t.Fatalf("read the migrator connection's locks: %v", err)
			}
			if leftLocks != 0 {
				t.Errorf("the failed run left %d relation locks on its connection, want none", leftLocks)
			}
			t.Logf("operator-visible error after %s: %v", elapsed.Round(time.Millisecond), err)

			if err := holder.Commit(); err != nil {
				t.Fatalf("holder commit: %v", err)
			}
			if err := executor.applyTrackedDefinitions(ctx, []Definition{definition}, schemaStatementBounds{
				lockTimeout:     5 * time.Second,
				lockRetryBudget: 30 * time.Second,
			}, logger); err != nil {
				t.Fatalf("migration 191 after the holder ended: %v", err)
			}
			if got := countTermsGenerationForeignKeys(t, ctx, database); got != 0 {
				t.Errorf("terms foreign keys after the later run = %d, want none", got)
			}
			if !migrationLedgerHas(t, ctx, database, definition.Path) {
				t.Error("the later run did not record migration 191 in the ledger")
			}
		})
	}
}
