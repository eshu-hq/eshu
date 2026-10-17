// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// factKeyForeignKeyDrop pairs one key sidecar with the migration that drops
// its per-fact foreign key (#7420 PR2) and a statement that writes one key
// row for fact $1 of scope $2, generation $3, as the ingestion key writers do.
type factKeyForeignKeyDrop struct {
	table     string
	migration string
	insertKey string
}

var factKeyForeignKeyDrops = []factKeyForeignKeyDrop{
	{
		table:     "relationship_reference_candidate_keys",
		migration: "relationship_reference_candidate_keys_drop_fact_fk",
		insertKey: `INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key)
VALUES ($1, $2, $3, 'repo-fk', '|fk|')`,
	},
	{
		table:     "package_manifest_consumption_keys",
		migration: "package_manifest_consumption_keys_drop_fact_fk",
		insertKey: `INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
VALUES ($1, $2, $3, 'repo-fk', 'npm', 'pkg-fk')`,
	},
	{
		table:     "package_registry_identity_keys",
		migration: "package_registry_identity_keys_drop_fact_fk",
		insertKey: `INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id)
VALUES ($1, $2, $3, 'npm', 'pkg-fk', 'pid-fk')`,
	},
}

// factRecordsForeignKeys names every foreign key that references the
// fact_records table on the session's search_path.
func factRecordsForeignKeys(t *testing.T, ctx context.Context, database *sql.DB) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, `
SELECT conrelid::regclass::text || '.' || conname
FROM pg_constraint
WHERE contype = 'f' AND confrelid = to_regclass('fact_records')
ORDER BY 1`)
	if err != nil {
		t.Fatalf("list fact_records foreign keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan fact_records foreign key: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read fact_records foreign keys: %v", err)
	}
	return names
}

// restoreFactKeyForeignKey puts one per-fact foreign key back, as the
// pre-#7420 schema had it, so a test can run its drop migration again.
func restoreFactKeyForeignKey(t *testing.T, ctx context.Context, database *sql.DB, table string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT "+table+
		"_fact_id_fkey FOREIGN KEY (fact_id) REFERENCES fact_records(fact_id) ON DELETE CASCADE"); err != nil {
		t.Fatalf("restore %s foreign key: %v", table, err)
	}
}

// waitForMigrationLockWait polls until backend pid waits on a heavyweight lock.
func waitForMigrationLockWait(t *testing.T, ctx context.Context, database *sql.DB, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := database.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND NOT granted)`, pid).Scan(&waiting); err != nil {
			t.Fatalf("poll backend %d lock wait: %v", pid, err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d never waited on a lock: the overlap did not happen", pid)
}

// migrationConn opens one connection with the migration runner's 5 s
// lock_timeout and returns it with its backend pid.
func migrationConn(t *testing.T, ctx context.Context, database *sql.DB) (*sql.Conn, int) {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open migration connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.ExecContext(ctx, "SET lock_timeout = '5s'"); err != nil {
		t.Fatalf("set migration lock_timeout: %v", err)
	}
	var pid int
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("read migration backend pid: %v", err)
	}
	return conn, pid
}

// TestBootstrapDropsEveryFactRecordsForeignKeyLive is the #7420 PR2 end
// state: after the full bootstrap no foreign key references fact_records, so
// no fact delete fires a per-row referential trigger. Retention's explicit
// key deletes (facts/keys) are what keep the key sidecars clean.
func TestBootstrapDropsEveryFactRecordsForeignKeyLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	if got := factRecordsForeignKeys(t, ctx, database); len(got) != 0 {
		t.Fatalf("foreign keys referencing fact_records after bootstrap = %v, want none", got)
	}
}

// TestFactKeyForeignKeyDropMigrationsAreDeadlockFreeLive runs each drop
// migration against the two lock orders production uses on a fact and its
// key sidecar. Ingestion writes the fact, then the key; retention and the
// package key rebuild touch the key table, then fact_records. A plain
// ALTER TABLE ... DROP CONSTRAINT locks the key table, then waits for
// fact_records while it holds that lock, and deadlocks the ingestion order
// (SQLSTATE 40P01). The migration must wait only while it holds nothing:
// against the ingestion order it waits and then drops the key; against the
// key-first order it fails at once with 55P03, which the runner retries.
// A re-run after the drop takes no table lock at all, because the local
// supervisor re-applies every definition at each start.
func TestFactKeyForeignKeyDropMigrationsAreDeadlockFreeLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	database.SetMaxOpenConns(8)
	const scope = "fk-drop"
	testfixtures.SeedScope(t, ctx, database, scope)
	generation := scope + "-active"
	for index, drop := range factKeyForeignKeyDrops {
		t.Run(drop.table, func(t *testing.T) {
			migration := MigrationSQL(drop.migration)
			factID := "fk-drop-fact-" + drop.table
			restoreFactKeyForeignKey(t, ctx, database, drop.table)

			// Ingestion order: fact, then key, while the migration runs.
			writer, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin ingestion-order writer: %v", err)
			}
			defer func() { _ = writer.Rollback() }()
			if _, err := writer.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content', $1, 'git', $1, now(), now(), '{}'::jsonb)`, factID, scope, generation); err != nil {
				t.Fatalf("writer inserts fact: %v", err)
			}
			conn, pid := migrationConn(t, ctx, database)
			migrated := make(chan error, 1)
			go func() {
				_, err := conn.ExecContext(ctx, migration)
				migrated <- err
			}()
			waitForMigrationLockWait(t, ctx, database, pid)
			if _, err := writer.ExecContext(ctx, drop.insertKey, factID, scope, generation); err != nil {
				t.Fatalf("ingestion-order writer inserts key while the drop waits: %v", err)
			}
			if err := writer.Commit(); err != nil {
				t.Fatalf("ingestion-order writer commit: %v", err)
			}
			if err := <-migrated; err != nil {
				t.Fatalf("drop migration beside the ingestion-order writer: %v", err)
			}
			if got := factRecordsForeignKeys(t, ctx, database); len(got) != 0 {
				t.Fatalf("foreign keys after the drop = %v, want none", got)
			}

			// Key-first order: the key table is held, fact_records is not.
			restoreFactKeyForeignKey(t, ctx, database, drop.table)
			holder, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin key-first holder: %v", err)
			}
			defer func() { _ = holder.Rollback() }()
			if _, err := holder.ExecContext(ctx, "DELETE FROM "+drop.table+" WHERE scope_id = $1", scope); err != nil {
				t.Fatalf("key-first holder deletes keys: %v", err)
			}
			started := time.Now()
			_, err = conn.ExecContext(ctx, migration)
			elapsed := time.Since(started)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "55P03" || !coordination.IsLockNotAvailable(err) {
				t.Fatalf("drop migration beside a key-first holder: err = %v, want SQLSTATE 55P03 for the runner's retry", err)
			}
			if elapsed > 2*time.Second {
				t.Fatalf("drop migration waited %s beside a key-first holder, want an immediate 55P03", elapsed)
			}
			if _, err := holder.ExecContext(ctx, "SELECT count(*) FROM fact_records WHERE scope_id = $1", scope); err != nil {
				t.Fatalf("key-first holder reads facts after the failed drop: %v", err)
			}
			if err := holder.Commit(); err != nil {
				t.Fatalf("key-first holder commit: %v", err)
			}
			if got := factRecordsForeignKeys(t, ctx, database); len(got) != 1 {
				t.Fatalf("foreign keys after the refused drop = %v, want the one restored key", got)
			}
			if _, err := conn.ExecContext(ctx, migration); err != nil {
				t.Fatalf("retried drop migration after the holder committed: %v", err)
			}

			// No-op re-run: zero table locks.
			assertDropMigrationTakesNoTableLock(t, ctx, database, migration)
			if index == len(factKeyForeignKeyDrops)-1 {
				if got := factRecordsForeignKeys(t, ctx, database); len(got) != 0 {
					t.Fatalf("foreign keys after every drop = %v, want none", got)
				}
			}
		})
	}
}

// assertDropMigrationTakesNoTableLock runs migration in a transaction and
// fails if it took any lock on a non-catalog relation.
func assertDropMigrationTakesNoTableLock(t *testing.T, ctx context.Context, database *sql.DB, migration string) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin no-op re-run: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		t.Fatalf("no-op re-run of the drop migration: %v", err)
	}
	var locked []string
	rows, err := tx.QueryContext(ctx, `
SELECT relation::regclass::text || ':' || mode
FROM pg_locks AS lock
JOIN pg_class AS class ON class.oid = lock.relation
JOIN pg_namespace AS namespace ON namespace.oid = class.relnamespace
WHERE lock.pid = pg_backend_pid() AND lock.locktype = 'relation'
  AND namespace.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY 1`)
	if err != nil {
		t.Fatalf("read no-op re-run locks: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan no-op re-run lock: %v", err)
		}
		locked = append(locked, name)
	}
	if len(locked) != 0 {
		t.Fatalf("no-op re-run of the drop migration locked %v, want no table lock", locked)
	}
}
