// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// openBootstrapWaitTestDB returns a handle on the empty disposable database
// named by ESHU_POSTGRES_RECOVERY_TEST_DSN, or skips.
func openBootstrapWaitTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_RECOVERY_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_RECOVERY_TEST_DSN to an empty disposable PostgreSQL database")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// holdFor runs setup on a dedicated session, holds it for hold, then runs
// release and closes the session. Errors surface through the returned
// channel so the test can fail instead of hanging on a silent holder.
func holdFor(ctx context.Context, database *sql.DB, hold time.Duration, setup, release []string) <-chan error {
	done := make(chan error, 1)
	go func() {
		conn, err := database.Conn(ctx)
		if err != nil {
			done <- fmt.Errorf("holder session: %w", err)
			return
		}
		defer func() { _ = conn.Close() }()
		for _, stmt := range setup {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				done <- fmt.Errorf("holder %q: %w", stmt, err)
				return
			}
		}
		time.Sleep(hold)
		for _, stmt := range release {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				done <- fmt.Errorf("holder release %q: %w", stmt, err)
				return
			}
		}
		done <- nil
	}()
	return done
}

// TestBootstrapWaitsForOwnershipHeldLongerThanLockTimeoutLive pins #6956
// cause 1: another bootstrapper owns the schema advisory lock for longer
// than the 5 s statement lock timeout. Before the fix the second
// bootstrapper failed with "acquire schema bootstrap ownership: context
// deadline exceeded"; it must instead wait for the owner and then apply.
func TestBootstrapWaitsForOwnershipHeldLongerThanLockTimeoutLive(t *testing.T) {
	database := openBootstrapWaitTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	const hold = 3 * defaultSchemaLockTimeout
	holder := holdFor(ctx, database, hold,
		[]string{fmt.Sprintf("SELECT pg_advisory_lock(%d, %d)", schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID)},
		[]string{fmt.Sprintf("SELECT pg_advisory_unlock(%d, %d)", schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID)},
	)
	time.Sleep(500 * time.Millisecond) // let the holder take the lock first
	definitions := []Definition{
		{Name: "table", Path: "test/001_wait_table.sql", SQL: "CREATE TABLE IF NOT EXISTS eshu_6956_wait (id INT)"},
	}
	started := time.Now()
	err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, slog.Default(), schemaBootstrapCoordination{})
	waited := time.Since(started)
	if herr := <-holder; herr != nil {
		t.Fatalf("holder: %v", herr)
	}
	if err != nil {
		t.Fatalf("bootstrap behind a %s owner failed after %s: %v", hold, waited.Round(time.Millisecond), err)
	}
	if waited < hold-time.Second {
		t.Fatalf("bootstrap finished after %s, before the owner released at %s; it did not wait for ownership", waited.Round(time.Millisecond), hold)
	}
	var exists bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass('eshu_6956_wait') IS NOT NULL").Scan(&exists); err != nil || !exists {
		t.Fatalf("migration did not apply after the wait: exists=%t err=%v", exists, err)
	}
}

// TestBootstrapRetriesStatementLockTimeoutLive pins #6956 cause 2: a
// migration statement hits lock_timeout (SQLSTATE 55P03) because another
// session holds a conflicting table lock, as an anti-wraparound autovacuum
// does. Before the fix the migrator failed outright and the receipt was not
// recorded; it must retry the statement until the lock clears, within its
// budget, and record the receipt once.
func TestBootstrapRetriesStatementLockTimeoutLive(t *testing.T) {
	database := openBootstrapWaitTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := database.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS eshu_6956_locked (id INT)"); err != nil {
		t.Fatalf("create locked table: %v", err)
	}
	const hold = 3 * defaultSchemaLockTimeout
	holder := holdFor(ctx, database, hold,
		[]string{"BEGIN", "LOCK TABLE eshu_6956_locked IN SHARE UPDATE EXCLUSIVE MODE"},
		[]string{"ROLLBACK"},
	)
	time.Sleep(500 * time.Millisecond)
	definitions := []Definition{
		{Name: "index", Path: "test/002_locked_index.sql", SQL: "CREATE INDEX IF NOT EXISTS eshu_6956_locked_idx ON eshu_6956_locked (id)"},
	}
	started := time.Now()
	err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, slog.Default(), schemaBootstrapCoordination{})
	waited := time.Since(started)
	if herr := <-holder; herr != nil {
		t.Fatalf("holder: %v", herr)
	}
	if err != nil {
		if isPostgresLockNotAvailable(err) || strings.Contains(err.Error(), "55P03") {
			t.Fatalf("migration gave up on lock_timeout after %s instead of retrying behind a %s table lock: %v", waited.Round(time.Millisecond), hold, err)
		}
		t.Fatalf("migration failed after %s: %v", waited.Round(time.Millisecond), err)
	}
	var receipts int
	if err := database.QueryRowContext(ctx,
		"SELECT count(*) FROM eshu_schema_migrations WHERE path = $1", definitions[0].Path,
	).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("receipts for the retried migration = %d (err %v), want exactly 1", receipts, err)
	}
	var valid bool
	if err := database.QueryRowContext(ctx, `SELECT i.indisvalid FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = 'eshu_6956_locked_idx'`).Scan(&valid); err != nil || !valid {
		t.Fatalf("retried index invalid or absent: valid=%t err=%v", valid, err)
	}
}
