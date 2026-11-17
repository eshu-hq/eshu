// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10820 disposable PostgreSQL 18 proof of the request-level derive advisory-lock budget: one purge request with parked holders on four repositories blocks the first three after about 10 s each and the fourth within 1 s once the 30 s budget is spent, a fifth repository with free locks completes, nothing is written for the blocked ones, the pooled lock_timeout is restored, and a re-POST purges the four once the holders end
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// seedBudgetRepository writes one fenced repository the purge can complete: a
// scope with a terminal generation, a repo-keyed content row, and the
// pending/fenced tombstone. Each repository gets its own scope id, which
// seedPurgeLiveScope does not (it hardcodes one repository's).
func seedBudgetRepository(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	store RetirementStore,
	partition string,
	n int,
) {
	t.Helper()
	scopeID := fmt.Sprintf("budget-scope-%d", n)
	for _, stmt := range []struct {
		name string
		sql  string
		args []any
	}{
		{
			"scope", `INSERT INTO ingestion_scopes
			(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
			 observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1, 'repository', 'git', $2, 'git_collector', $3, NOW(), NOW(), 'active', NULL, '{}'::jsonb)`,
			[]any{scopeID, "key-" + scopeID, partition},
		},
		{
			"generation", `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
			VALUES ($1, $2, 'push', NOW(), NOW(), 'superseded', '{}'::jsonb)`,
			[]any{scopeID + "-gen", scopeID},
		},
		{
			"content file", `INSERT INTO content_files
			(repo_id, relative_path, content, content_hash, line_count, indexed_at)
			VALUES ($1, 'main.go', 'package main', 'hash-1', 1, NOW())`,
			[]any{partition},
		},
	} {
		if _, err := database.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed %s for %s: %v", stmt.name, partition, err)
		}
	}
	if _, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: fmt.Sprintf("rr_budget_%d", n), RepoID: partition, ScopeID: scopeID,
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "budget-key",
	}); err != nil {
		t.Fatalf("InsertPending %s: %v", partition, err)
	}
}

// TestRetirementPurgeRequestDeriveLockBudgetLive proves the request-level
// derive-lock budget against real PostgreSQL. One purge request covers five
// fenced repositories. Parked sessions hold the infra advisory lock of the
// first four: the first three block after about 10 s each, which spends the
// 30 s request budget, and the fourth blocks within 1 s on the 100 ms floor. The
// fifth repository's locks are free and it still completes. Nothing is written
// for the blocked four, the pooled connection keeps its lock_timeout, and a
// re-POST purges the four once the holders end.
func TestRetirementPurgeRequestDeriveLockBudgetLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_req_budget",
		"set ESHU_POSTGRES_DSN to run the request-level derive lock budget Postgres proof")
	partitions := budgetRepoIDs("live", 5)
	held, free := partitions[:4], partitions[4]
	store := NewRetirementStore(SQLDB{DB: database})
	for i, partition := range partitions {
		seedBudgetRepository(t, ctx, database, store, partition, i+1)
	}

	// Four parked holders leave exactly one connection for the purge, so the
	// lock_timeout read-back after the request reads the connection every purge
	// transaction used, including the fifth, which commits.
	database.SetMaxOpenConns(len(held) + 1)
	priorLockTimeout := showLockTimeout(t, ctx, database)
	if priorLockTimeout == "10s" || priorLockTimeout == "100ms" {
		t.Fatalf("pooled lock_timeout is already %q before the purge; the leak check needs a distinct prior value", priorLockTimeout)
	}
	var parked []*sql.Tx
	defer func() {
		for _, tx := range parked {
			_ = tx.Rollback()
		}
	}()
	for _, partition := range held {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin holder for %s: %v", partition, err)
		}
		parked = append(parked, tx)
		if err := inventory.LockRepository(ctx, tx, partition); err != nil {
			t.Fatalf("holder lock for %s: %v", partition, err)
		}
	}

	logs := &logCapture{}
	purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
	purger.Options.Logger = slog.New(logs)
	started := time.Now()
	result, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: partitions})
	total := time.Since(started)
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 1 || result.Completed[0] != free || len(result.Blocked) != len(held) {
		t.Fatalf("result = %+v, want %s completed and the four held repositories blocked", result, free)
	}
	for i, blocked := range result.Blocked {
		if blocked.RepoID != held[i] || blocked.Reason != PurgeReasonDeriveLockBusy {
			t.Errorf("blocked[%d] = %s/%s, want %s/%s", i, blocked.RepoID, blocked.Reason, held[i], PurgeReasonDeriveLockBusy)
		}
	}

	// Each repository's wait is the gap between the WARN lines, stamped by this
	// test's own handler: independent of the purge's derive_lock_wait_ms.
	busy := logs.named(deriveBusyLogMessage)
	if len(busy) != len(held) {
		t.Fatalf("derive_lock_busy WARN lines = %d, want %d", len(busy), len(held))
	}
	waits := make([]time.Duration, len(busy))
	previous := started
	for i, entry := range busy {
		waits[i] = entry.at.Sub(previous)
		previous = entry.at
	}
	timeouts := make([]int64, len(busy))
	for i, entry := range busy {
		timeouts[i], _ = entry.attrs["derive_lock_timeout_ms"].(int64)
		if entry.attrs["lock"] != "infra inventory" {
			t.Errorf("WARN %d lock = %v, want infra inventory", i+1, entry.attrs["lock"])
		}
	}
	t.Logf("held repositories blocked after %v with lock_timeout %v ms; the request took %s in all", waits, timeouts, total)

	for i := 0; i < 3; i++ {
		if waits[i] < 9*time.Second || waits[i] > 12*time.Second {
			t.Errorf("held repository %d blocked after %s, want about 10 s", i+1, waits[i])
		}
	}
	if waits[3] >= time.Second {
		t.Errorf("fourth held repository blocked after %s, want under 1 s: the 30 s request budget was spent", waits[3])
	}
	if spent := waits[0] + waits[1] + waits[2]; spent < 29*time.Second || spent > 33*time.Second {
		t.Errorf("the first three waits total %s, want about the 30 s budget", spent)
	}
	if total >= 35*time.Second {
		t.Errorf("the request took %s, want under the 35 s worst case", total)
	}
	// The third setting is what the first two waits left of the 30 s. Each ran
	// past 10 s (lock_timeout never fires early, and the clock starts before the
	// statement is sent), so less than 10 s remains and the setting, truncated to
	// milliseconds, is below 10000. Exactly 10000 would mean the purge charged the
	// timeout it set instead of the time the statement took.
	if timeouts[0] != 10000 || timeouts[1] != 10000 || timeouts[2] <= 9000 || timeouts[2] >= 10000 || timeouts[3] != 100 {
		t.Errorf("derive_lock_timeout_ms = %v, want 10000, 10000, just under 10000, then the 100 floor", timeouts)
	}

	// Nothing was written for the blocked four: each tombstone is still
	// pending/fenced and its scope and content rows stay.
	for _, partition := range held {
		var state, phase string
		if err := database.QueryRowContext(ctx, `SELECT state, phase FROM repository_retirements
			WHERE repo_id = $1 AND readmitted_at IS NULL`, partition).Scan(&state, &phase); err != nil {
			t.Fatalf("read tombstone of %s: %v", partition, err)
		}
		if state != RetirementStatePending || phase != RetirementPhaseFenced {
			t.Errorf("%s tombstone = %s/%s, want pending/fenced", partition, state, phase)
		}
		for _, q := range []string{
			`SELECT count(*) FROM ingestion_scopes WHERE partition_key = $1`,
			`SELECT count(*) FROM content_files WHERE repo_id = $1`,
		} {
			if got := liveCount(t, ctx, database, q, partition); got != 1 {
				t.Errorf("%s: %q = %d, want the row kept (the purge was blocked)", partition, q, got)
			}
		}
	}
	// The unheld fifth completed: tombstone done, its rows gone.
	var state, phase string
	if err := database.QueryRowContext(ctx, `SELECT state, phase FROM repository_retirements
		WHERE repo_id = $1`, free).Scan(&state, &phase); err != nil {
		t.Fatalf("read tombstone of %s: %v", free, err)
	}
	if state != RetirementStateComplete || phase != RetirementPhaseDone {
		t.Errorf("%s tombstone = %s/%s, want complete/done", free, state, phase)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM ingestion_scopes WHERE partition_key = $1`, free); got != 0 {
		t.Errorf("%s scope rows = %d, want 0 after the purge", free, got)
	}

	// Every purge transaction used the one free connection, and the fifth
	// committed on it: a session-scoped set_config would show here.
	if got := showLockTimeout(t, ctx, database); got != priorLockTimeout {
		t.Fatalf("pooled lock_timeout after the request = %q, want the prior %q: the budget leaked past its transaction", got, priorLockTimeout)
	}

	// The re-POST: once the holders end, the four blocked repositories purge.
	for _, tx := range parked {
		if err := tx.Rollback(); err != nil {
			t.Fatalf("release holder: %v", err)
		}
	}
	retryStarted := time.Now()
	retried, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: held})
	if err != nil || len(retried.Completed) != len(held) || len(retried.Blocked) != 0 {
		t.Fatalf("retry = %+v, %v, want all four completed", retried, err)
	}
	t.Logf("the re-POST purged the four blocked repositories in %s", time.Since(retryStarted))
}
