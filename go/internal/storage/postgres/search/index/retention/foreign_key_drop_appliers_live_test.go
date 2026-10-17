// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7427 PR3 disposable PostgreSQL 18 proof that two migration 191 runs waiting behind a scope_generations holder both succeed (the second one's DROP CONSTRAINT IF EXISTS finds no key; without IF EXISTS it fails with 42704) and hold no terms lock while they wait; enrolled in the live-postgres-readiness runner; reads ESHU_GENERATION_RETENTION_PROOF_DSN (with its DISPOSABLE flag) and creates one isolated bootstrapped schema. local run passed in 1.4s of test time on PostgreSQL 18.6 on 2026-10-16
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_POSTGRES_DSN

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
