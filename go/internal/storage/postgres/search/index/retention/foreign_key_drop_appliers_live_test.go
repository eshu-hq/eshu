// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7427 PR3 disposable PostgreSQL 18 proof that two migration 191 runs waiting behind a scope_generations holder both succeed (the second one's DROP CONSTRAINT IF EXISTS finds no key; without IF EXISTS it fails with 42704) and hold no terms lock while they wait; enrolled in the live-postgres-readiness runner; reads ESHU_GENERATION_RETENTION_PROOF_DSN (with its DISPOSABLE flag) and creates one isolated bootstrapped schema. local run passed in 1.4s of test time on PostgreSQL 18.6 on 2026-10-16
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// applierDropAttempts bounds how many times one applier runs the drop
// migration. An ambient terms holder (autovacuum, autoanalyze) makes an
// attempt fail at once with 55P03 from the migration's NOWAIT terms lock;
// the production runner retries exactly that error, so each applier retries
// it here too, logged, instead of failing the test (#8528).
const applierDropAttempts = 5

// runDropApplier runs the drop migration until it succeeds or fails with a
// non-55P03 error, retrying at most applierDropAttempts attempts. It returns
// the final error for the caller to report; it never fails the test itself,
// so appliers may run it from goroutines.
func (f *liveFixture) runDropApplier(t *testing.T, conn *sql.Conn, migration string) error {
	t.Helper()
	var err error
	for attempt := 1; attempt <= applierDropAttempts; attempt++ {
		_, err = conn.ExecContext(f.ctx, migration)
		if err == nil {
			return nil
		}
		if !coordination.IsLockNotAvailable(err) {
			return err
		}
		if attempt == applierDropAttempts {
			return err
		}
		t.Logf("drop applier attempt %d of %d refused with 55P03, retrying: %v", attempt, applierDropAttempts, err)
		time.Sleep(200 * time.Millisecond)
	}
	return err
}

// TestTermsForeignKeyDropLiveTwoAppliersBothSucceed: two migration runs (for
// example a retried Job beside the local supervisor) both find the key in
// pg_constraint, then both wait behind a session that holds
// scope_generations. When it ends, the first drops the key; the second gets
// its locks and its ALTER finds no key. DROP CONSTRAINT IF EXISTS turns that
// into a NOTICE, so both runs succeed. Without IF EXISTS the second run fails
// with "constraint ... does not exist" (SQLSTATE 42704), which the runner
// does not retry.
func TestTermsForeignKeyDropLiveTwoAppliersBothSucceed(t *testing.T) {
	f := openLiveFixture(t)
	migration := postgres.MigrationSQL(termsForeignKeyDrop)
	f.restoreTermsForeignKey(t, termsForeignKeyNames[0])

	holder, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin scope_generations holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(f.ctx, `LOCK TABLE scope_generations IN ROW SHARE MODE`); err != nil {
		t.Fatalf("holder locks scope_generations: %v", err)
	}

	first, firstPID := f.migrationConn(t)
	second, secondPID := f.migrationConn(t)
	results := make(chan error, 2)
	go func() {
		results <- f.runDropApplier(t, first, migration)
	}()
	f.waitForLockWait(t, firstPID)
	go func() {
		results <- f.runDropApplier(t, second, migration)
	}()
	f.waitForLockWait(t, secondPID)
	for _, pid := range []int{firstPID, secondPID} {
		if held := f.grantedTermsLocks(t, pid); held != 0 {
			t.Fatalf("waiting applier %d holds %d terms locks, want 0", pid, held)
		}
	}
	if err := holder.Commit(); err != nil {
		t.Fatalf("holder commit: %v", err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("an applier failed after the other dropped the key: %v; want both to succeed", err)
		}
	}
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms foreign keys after both appliers = %v, want none", got)
	}
}

// TestTermsForeignKeyDropLiveApplierRetriesAmbientLockTimeout is the
// deterministic repro of #8528: a terms-level holder (the shape an ambient
// autovacuum or autoanalyze takes) refuses the applier's NOWAIT terms lock at
// once with 55P03. The bounded per-applier retry applies the drop once the
// holder releases, the way the production runner's 55P03 retry does.
func TestTermsForeignKeyDropLiveApplierRetriesAmbientLockTimeout(t *testing.T) {
	f := openLiveFixture(t)
	migration := postgres.MigrationSQL(termsForeignKeyDrop)
	f.restoreTermsForeignKey(t, termsForeignKeyNames[0])

	reader, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin ambient terms holder: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	var count int
	if err := reader.QueryRowContext(f.ctx, `SELECT count(*) FROM eshu_search_index_terms`).Scan(&count); err != nil {
		t.Fatalf("ambient holder reads terms: %v", err)
	}
	conn, _ := f.migrationConn(t)
	released := make(chan struct{})
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = reader.Commit()
		close(released)
	}()
	if err := f.runDropApplier(t, conn, migration); err != nil {
		t.Fatalf("applier beside an ambient terms holder: %v; want the bounded retry to succeed", err)
	}
	<-released
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms foreign keys after the retried applier = %v, want none", got)
	}
}
