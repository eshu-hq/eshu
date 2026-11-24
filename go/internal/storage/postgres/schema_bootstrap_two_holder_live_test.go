// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// activeAdvisoryHolder takes the schema bootstrap advisory lock, reports its
// backend pid, then stays ACTIVELY running a migration-shape statement
// (pg_sleep stands in for a production-size DDL/backfill that outlasts
// lock_timeout) before releasing. An idle holder would understate the QA
// stall shape in eshu-hq/eshu#11352: the other bootstrapper was mid-migration,
// not parked.
func activeAdvisoryHolder(ctx context.Context, database *sql.DB, run time.Duration, lockSQL, unlockSQL string) (pid <-chan int, done <-chan error) {
	pidCh := make(chan int, 1)
	doneCh := make(chan error, 1)
	go func() {
		conn, err := database.Conn(ctx)
		if err != nil {
			pidCh <- 0
			doneCh <- fmt.Errorf("advisory holder session: %w", err)
			return
		}
		defer func() { _ = conn.Close() }()
		var backend int
		if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&backend); err != nil {
			pidCh <- 0
			doneCh <- fmt.Errorf("advisory holder pid: %w", err)
			return
		}
		if _, err := conn.ExecContext(ctx, lockSQL); err != nil {
			pidCh <- 0
			doneCh <- fmt.Errorf("advisory holder lock: %w", err)
			return
		}
		pidCh <- backend
		// The migration-shape statement the holder runs while owning the
		// advisory lock: actively executing, longer than lock_timeout.
		if _, err := conn.ExecContext(ctx, "SELECT pg_sleep($1)", run.Seconds()); err != nil {
			doneCh <- fmt.Errorf("advisory holder migration-shape statement: %w", err)
			return
		}
		if _, err := conn.ExecContext(ctx, unlockSQL); err != nil {
			doneCh <- fmt.Errorf("advisory holder unlock: %w", err)
			return
		}
		doneCh <- nil
	}()
	return pidCh, doneCh
}

// TestBootstrapTwoHolderProductionShapeRecoveryLive pins eshu-hq/eshu#11352:
// the only live bootstrap tests used a disposable tiny schema, so nothing
// exercised a real pg_advisory_lock held across a migration-shape statement
// exceeding lock_timeout, or the snapshot-wait path, in one run.
//
// Two holders contend with one real bootstrap run (no fake clocks):
//
//   - H1 owns the schema advisory lock while actively running a
//     migration-shape statement longer than lock_timeout (the other
//     bootstrapper mid-migration). The waiter must log
//     ownership.waiting naming H1's pid, then ownership.acquired.
//   - H2 first holds a conflicting table lock on the DDL target (the
//     statement exceeds the 2 s run lock_timeout, retries with lock_wait,
//     and recovers with lock_recovered), then holds an old snapshot while a
//     bare CREATE INDEX CONCURRENTLY builds (snapshot-wait: starting/finished,
//     never lock_wait for that statement).
//
// The run must finish in a bounded total wait instead of stalling the way
// the QA hook did.
func TestBootstrapTwoHolderProductionShapeRecoveryLive(t *testing.T) {
	database := openBootstrapWaitTestDB(t)
	// The context must outlive the cleanups that release the holders, so it
	// is cancelled by the first-registered cleanup (which runs last), not by
	// a defer that fires before cleanups.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	const lockTimeout = 2 * time.Second
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tableDDL := "eshu_11352_ddl_" + suffix
	tableCIC := "eshu_11352_cic_" + suffix
	indexCIC := tableCIC + "_idx"
	alterPath := "test/11352_alter_" + suffix + ".sql"
	cicPath := "test/11352_cic_" + suffix + ".sql"
	cleanupBootstrapWaitObjects(t, database, tableDDL, alterPath)
	cleanupBootstrapWaitObjects(t, database, tableCIC, cicPath)

	// Production shape: real heaps behind both statements, not empty shells.
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+tableDDL+" (id INT, payload TEXT)"); err != nil {
		t.Fatalf("create DDL target: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		"INSERT INTO "+tableDDL+" (id, payload) SELECT g, md5(g::text) FROM generate_series(1, 20000) g"); err != nil {
		t.Fatalf("seed DDL target: %v", err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE "+tableCIC+" (id INT)"); err != nil {
		t.Fatalf("create CIC target: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		"INSERT INTO "+tableCIC+" (id) SELECT g FROM generate_series(1, 20000) g"); err != nil {
		t.Fatalf("seed CIC target: %v", err)
	}

	lockSQL := fmt.Sprintf("SELECT pg_advisory_lock(%d, %d)", schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID)
	unlockSQL := fmt.Sprintf("SELECT pg_advisory_unlock(%d, %d)", schemaBootstrapAdvisoryLockClass, schemaBootstrapAdvisoryLockID)

	// H1: the other bootstrapper, owning the advisory lock across a
	// migration-shape statement longer than lock_timeout.
	holderPid, holderDone := activeAdvisoryHolder(ctx, database, 3*lockTimeout, lockSQL, unlockSQL)
	t.Cleanup(func() {
		if err := <-holderDone; err != nil {
			t.Errorf("advisory holder: %v", err)
		}
	})

	// H2: one session, two phases. Phase 1 holds a conflicting table lock on
	// the DDL target while ACTIVELY running (so the waiter's ALTER exceeds
	// lock_timeout and must retry). Phase 2 drops the table lock and holds
	// only an old snapshot while the concurrent build runs (snapshot-wait).
	// Phase 1 must still be held when the waiter clears ownership (H1 runs
	// 3x lock_timeout) and releases after at least one full retry cycle.
	const phase1 = 5 * lockTimeout
	const phase2 = 4 * lockTimeout
	h2Done := make(chan error, 1)
	h2Locked := make(chan struct{})
	go func() {
		conn, err := database.Conn(ctx)
		if err != nil {
			h2Done <- fmt.Errorf("H2 session: %w", err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			h2Done <- fmt.Errorf("H2 begin: %w", err)
			return
		}
		if _, err := conn.ExecContext(ctx, "LOCK TABLE "+tableDDL+" IN SHARE UPDATE EXCLUSIVE MODE"); err != nil {
			h2Done <- fmt.Errorf("H2 lock %s: %w", tableDDL, err)
			return
		}
		close(h2Locked)
		// Actively executing while holding the table lock: this keeps the
		// waiter's first ALTER attempt parked past lock_timeout AND holds
		// the snapshot horizon back at the same time.
		if _, err := conn.ExecContext(ctx, "SELECT pg_sleep($1)", phase1.Seconds()); err != nil {
			h2Done <- fmt.Errorf("H2 phase 1: %w", err)
			return
		}
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			h2Done <- fmt.Errorf("H2 commit phase 1: %w", err)
			return
		}
		// Phase 2: no table lock, only an old snapshot for the CIC build to
		// wait out.
		if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
			h2Done <- fmt.Errorf("H2 begin phase 2: %w", err)
			return
		}
		if _, err := conn.ExecContext(ctx, "SELECT pg_sleep($1)", phase2.Seconds()); err != nil {
			h2Done <- fmt.Errorf("H2 phase 2: %w", err)
			return
		}
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			h2Done <- fmt.Errorf("H2 commit phase 2: %w", err)
			return
		}
		h2Done <- nil
	}()
	t.Cleanup(func() {
		if err := <-h2Done; err != nil {
			t.Errorf("H2: %v", err)
		}
	})
	select {
	case <-h2Locked:
	case <-ctx.Done():
		t.Fatalf("H2 never took its table lock: %v", ctx.Err())
	}
	// Let both holders be parked server-side before the waiter starts, so the
	// ownership wait and the first statement attempt both contend for real.
	time.Sleep(500 * time.Millisecond)

	pid := <-holderPid

	logger, logs := capturedLogger()
	definitions := []Definition{
		{Name: "alter", Path: alterPath, SQL: "ALTER TABLE " + tableDDL + " ADD COLUMN tag TEXT"},
		{Name: "index", Path: cicPath, SQL: "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + indexCIC + " ON " + tableCIC + " (id)"},
	}
	started := time.Now()
	err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions, logger, schemaBootstrapCoordination{
		ownershipWait:   90 * time.Second,
		lockRetryBudget: 90 * time.Second,
		lockTimeout:     lockTimeout,
	})
	waited := time.Since(started)
	if err != nil {
		t.Fatalf("bootstrap behind two holders failed after %s: %v\n%s", waited.Round(time.Millisecond), err, logs.String())
	}
	// The waiter must have outlasted H1's advisory hold, H2's table lock,
	// and H2's snapshot: anything shorter skipped a wait it was required to
	// perform.
	if waited < phase1+phase2-time.Second {
		t.Fatalf("bootstrap finished after %s, before H2 released at ~%s; it did not wait out both holders", waited.Round(time.Millisecond), phase1+phase2)
	}
	if waited > 90*time.Second {
		t.Fatalf("bootstrap took %s, past the bounded wait; the QA-stall shape regressed", waited.Round(time.Millisecond))
	}
	text := logs.String()
	if !strings.Contains(text, "bootstrap.postgres.ownership.waiting") || !strings.Contains(text, fmt.Sprintf("pid=%d ", pid)) {
		t.Fatalf("want an ownership.waiting event naming the advisory holder pid %d, got:\n%s", pid, text)
	}
	if !strings.Contains(text, "bootstrap.postgres.ownership.acquired") {
		t.Fatalf("want an ownership.acquired event after the wait, got:\n%s", text)
	}
	// Attribute retries per statement: the ALTER that exceeded lock_timeout
	// must retry with lock_wait and recover, while the CONCURRENTLY build
	// must never appear in a lock_wait line (#7004 exempts it from
	// lock_timeout entirely, so it waits out the snapshot silently).
	lockWaitFor := func(path string) bool {
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "bootstrap.postgres.migration.lock_wait") && strings.Contains(line, path) {
				return true
			}
		}
		return false
	}
	if !lockWaitFor(alterPath) {
		t.Fatalf("want a lock_wait event for the ALTER that exceeded lock_timeout (%s), got:\n%s", alterPath, text)
	}
	if lockWaitFor(cicPath) {
		t.Fatalf("the CONCURRENTLY build (%s) retried via lock_timeout instead of waiting out the snapshot, got:\n%s", cicPath, text)
	}
	recovered := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "bootstrap.postgres.migration.lock_recovered") {
			recovered = line
		}
	}
	if recovered == "" || !strings.Contains(recovered, alterPath) {
		t.Fatalf("want a lock_recovered event for %s, got:\n%s", alterPath, text)
	}
	if !strings.Contains(text, "bootstrap.postgres.migration.concurrent_index_build.starting") ||
		!strings.Contains(text, "bootstrap.postgres.migration.concurrent_index_build.finished") {
		t.Fatalf("want concurrent-index-build starting/finished events for the snapshot-wait path, got:\n%s", text)
	}
	var receipts int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM eshu_schema_migrations WHERE path IN ($1, $2)", alterPath, cicPath).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatalf("receipts for the two migrations = %d (err %v), want exactly 2", receipts, err)
	}
	var valid bool
	if err := database.QueryRowContext(ctx, `SELECT i.indisvalid FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = $1`, indexCIC).Scan(&valid); err != nil || !valid {
		t.Fatalf("concurrent index invalid or absent: valid=%t err=%v", valid, err)
	}
}
