// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
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

// holdFor runs setup on a dedicated session, reports that session's backend
// pid, holds for hold, then runs release and closes the session. Errors
// surface through the done channel so the test fails instead of hanging on
// a silent holder.
func holdFor(ctx context.Context, database *sql.DB, hold time.Duration, setup, release []string) (pid <-chan int, done <-chan error) {
	pidCh := make(chan int, 1)
	doneCh := make(chan error, 1)
	go func() {
		conn, err := database.Conn(ctx)
		if err != nil {
			pidCh <- 0
			doneCh <- fmt.Errorf("holder session: %w", err)
			return
		}
		defer func() { _ = conn.Close() }()
		var backend int
		if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&backend); err != nil {
			pidCh <- 0
			doneCh <- fmt.Errorf("holder pid: %w", err)
			return
		}
		for _, stmt := range setup {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				pidCh <- 0
				doneCh <- fmt.Errorf("holder %q: %w", stmt, err)
				return
			}
		}
		pidCh <- backend
		time.Sleep(hold)
		for _, stmt := range release {
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				doneCh <- fmt.Errorf("holder release %q: %w", stmt, err)
				return
			}
		}
		doneCh <- nil
	}()
	return pidCh, doneCh
}

// cleanupBootstrapWaitObjects drops what one run created so the next run
// on the same database exercises the real path again instead of finding a
// recorded receipt and skipping the statement.
func cleanupBootstrapWaitObjects(t *testing.T, database *sql.DB, table, path string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := database.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Errorf("cleanup %s: %v", table, err)
		}
		var ledgerExists bool
		if err := database.QueryRowContext(ctx, "SELECT to_regclass('eshu_schema_migrations') IS NOT NULL").Scan(&ledgerExists); err != nil {
			t.Errorf("cleanup: inspect ledger: %v", err)
			return
		}
		if !ledgerExists {
			return // the run failed before the ledger existed; nothing to delete
		}
		if _, err := database.ExecContext(ctx, "DELETE FROM eshu_schema_migrations WHERE path = $1", path); err != nil {
			t.Errorf("cleanup receipt %s: %v", path, err)
		}
	})
}

func capturedLogger() (*slog.Logger, *bytes.Buffer) {
	var logs bytes.Buffer
	return slog.New(slog.NewTextHandler(&logs, nil)), &logs
}

// TestBootstrapWaitsForOwnershipHeldLongerThanLockTimeoutLive pins #6956
// cause 1: another bootstrapper owns the schema advisory lock for longer
// than the 5 s statement lock timeout. Before the fix the second
// bootstrapper failed with "acquire schema bootstrap ownership: timeout:
// context deadline exceeded" at 5 s; it must instead wait for the owner,
// name that owner's pid in its waiting log, and then apply. A holder of the
// same advisory key in ANOTHER database of the instance does not block and
// must not be named (advisory locks are per database).
func TestBootstrapWaitsForOwnershipHeldLongerThanLockTimeoutLive(t *testing.T) {
	database := openBootstrapWaitTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	table := "eshu_6956_wait_" + suffix
	path := "test/6956_wait_" + suffix + ".sql"
	cleanupBootstrapWaitObjects(t, database, table, path)

	lockSQL := fmt.Sprintf("SELECT pg_advisory_lock(%d, %d)", schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID)
	unlockSQL := fmt.Sprintf("SELECT pg_advisory_unlock(%d, %d)", schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID)
	const hold = 3 * defaultSchemaLockTimeout
	holderPid, holder := holdFor(ctx, database, hold, []string{lockSQL}, []string{unlockSQL})
	pid := <-holderPid

	// A same-key holder in another database: must not block, must not be named.
	otherPid := holdAdvisoryKeyInAnotherDatabase(ctx, t, database, hold, lockSQL, unlockSQL)

	logger, logs := capturedLogger()
	definitions := []Definition{{Name: "table", Path: path, SQL: "CREATE TABLE " + table + " (id INT)"}}
	started := time.Now()
	err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, logger, schemaBootstrapCoordination{})
	waited := time.Since(started)
	if herr := <-holder; herr != nil {
		t.Fatalf("holder: %v", herr)
	}
	if err != nil {
		t.Fatalf("bootstrap behind a %s owner failed after %s: %v\n%s", hold, waited.Round(time.Millisecond), err, logs.String())
	}
	if waited < hold-time.Second {
		t.Fatalf("bootstrap finished after %s, before the owner released at %s; it did not wait for ownership", waited.Round(time.Millisecond), hold)
	}
	text := logs.String()
	// The holder string is "pid=<n> application_name=..."; match with the
	// trailing space so 312 cannot match 3127.
	if !strings.Contains(text, "bootstrap.postgres.ownership.waiting") || !strings.Contains(text, fmt.Sprintf("pid=%d ", pid)) {
		t.Fatalf("want a waiting event naming the holder pid %d, got:\n%s", pid, text)
	}
	if !strings.Contains(text, "bootstrap.postgres.ownership.acquired") {
		t.Fatalf("want an acquired event after the wait, got:\n%s", text)
	}
	if otherPid != 0 && strings.Contains(text, fmt.Sprintf("pid=%d ", otherPid)) {
		t.Fatalf("waiting event named pid %d, a holder in another database that cannot block this one:\n%s", otherPid, text)
	}
	var exists bool
	if err := database.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil || !exists {
		t.Fatalf("migration did not apply after the wait: exists=%t err=%v", exists, err)
	}
}

// holdAdvisoryKeyInAnotherDatabase takes the same advisory key on a second
// database of the instance for hold and returns that holder's pid, or 0
// when the test role cannot create a database (the cross-database assertion
// is then skipped, the rest of the test still runs).
func holdAdvisoryKeyInAnotherDatabase(ctx context.Context, t *testing.T, database *sql.DB, hold time.Duration, lockSQL, unlockSQL string) int {
	t.Helper()
	var dsnDatabase string
	if err := database.QueryRowContext(ctx, "SELECT current_database()").Scan(&dsnDatabase); err != nil {
		t.Fatalf("current database: %v", err)
	}
	other := dsnDatabase + "_other"
	if _, err := database.ExecContext(ctx, "CREATE DATABASE "+other); err != nil {
		t.Logf("cannot create a second database (%v); skipping the cross-database holder assertion", err)
		return 0
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := database.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+other+" WITH (FORCE)"); err != nil {
			t.Errorf("drop %s: %v", other, err)
		}
	})
	otherDSN, err := url.Parse(os.Getenv("ESHU_POSTGRES_RECOVERY_TEST_DSN"))
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	otherDSN.Path = "/" + other // only the database path; the user may share the database's name
	otherDB, err := sql.Open("pgx", otherDSN.String())
	if err != nil {
		t.Fatalf("open other database: %v", err)
	}
	t.Cleanup(func() { _ = otherDB.Close() })
	pidCh, done := holdFor(ctx, otherDB, hold, []string{lockSQL}, []string{unlockSQL})
	pid := <-pidCh
	t.Cleanup(func() {
		if err := <-done; err != nil {
			t.Errorf("other-database holder: %v", err)
		}
	})
	return pid
}

// TestBootstrapRetriesStatementLockTimeoutLive pins #6956 cause 2: a
// migration statement hits lock_timeout (SQLSTATE 55P03) because another
// session holds a conflicting table lock, as an anti-wraparound autovacuum
// does. The statement is CREATE INDEX CONCURRENTLY, the production shape
// (migration 118) and the one non-transactional one. Before the fix the
// migrator failed outright and recorded nothing; it must retry the
// statement until the lock clears, log each retry and the recovery, and
// record the receipt once.
func TestBootstrapRetriesStatementLockTimeoutLive(t *testing.T) {
	database := openBootstrapWaitTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	table := "eshu_6956_locked_" + suffix
	index := table + "_idx"
	path := "test/6956_locked_" + suffix + ".sql"
	cleanupBootstrapWaitObjects(t, database, table, path)
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+table+" (id INT)"); err != nil {
		t.Fatalf("create locked table: %v", err)
	}
	const hold = 3 * defaultSchemaLockTimeout
	_, holder := holdFor(ctx, database, hold,
		[]string{"BEGIN", "LOCK TABLE " + table + " IN SHARE UPDATE EXCLUSIVE MODE"},
		[]string{"ROLLBACK"},
	)
	time.Sleep(500 * time.Millisecond)
	logger, logs := capturedLogger()
	definitions := []Definition{{Name: "index", Path: path, SQL: "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + index + " ON " + table + " (id)"}}
	started := time.Now()
	err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, logger, schemaBootstrapCoordination{})
	waited := time.Since(started)
	if herr := <-holder; herr != nil {
		t.Fatalf("holder: %v", herr)
	}
	if err != nil {
		if isPostgresLockNotAvailable(err) {
			t.Fatalf("migration gave up on lock_timeout after %s instead of retrying behind a %s table lock: %v\n%s", waited.Round(time.Millisecond), hold, err, logs.String())
		}
		t.Fatalf("migration failed after %s: %v\n%s", waited.Round(time.Millisecond), err, logs.String())
	}
	text := logs.String()
	if !strings.Contains(text, "bootstrap.postgres.migration.lock_wait") || !strings.Contains(text, "bootstrap.postgres.migration.lock_recovered") {
		t.Fatalf("the statement was not retried (no lock_wait and lock_recovered events); a rerun against a recorded receipt proves nothing:\n%s", text)
	}
	var receipts int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM eshu_schema_migrations WHERE path = $1", path).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("receipts for the retried migration = %d (err %v), want exactly 1", receipts, err)
	}
	var valid bool
	if err := database.QueryRowContext(ctx, `SELECT i.indisvalid FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = $1`, index).Scan(&valid); err != nil || !valid {
		t.Fatalf("retried concurrent index invalid or absent: valid=%t err=%v", valid, err)
	}
}
