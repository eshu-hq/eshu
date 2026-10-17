// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

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
		_, err := first.ExecContext(f.ctx, migration)
		results <- err
	}()
	f.waitForLockWait(t, firstPID)
	go func() {
		_, err := second.ExecContext(f.ctx, migration)
		results <- err
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
