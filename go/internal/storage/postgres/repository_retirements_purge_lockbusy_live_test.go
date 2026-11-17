// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10820 disposable PostgreSQL 18 proof that the purge's derive advisory-lock wait is bounded: a parked holder of either lock makes the purge return derive_lock_busy within the 10 s budget with nothing written, the retry completes once the holder ends, and the budget restores the prior lock_timeout
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestRetirementPurgeDeriveLockBusyLive parks a session on one derive advisory
// lock and proves the purge returns derive_lock_busy within the budget, wrote
// nothing, and completes on retry once the holder ends.
func TestRetirementPurgeDeriveLockBusyLive(t *testing.T) {
	for _, holder := range []struct {
		name string
		take func(ctx context.Context, tx *sql.Tx, partition string) error
	}{
		{"infra", func(ctx context.Context, tx *sql.Tx, partition string) error {
			return inventory.LockRepository(ctx, tx, partition)
		}},
		{"config_artifact", func(ctx context.Context, tx *sql.Tx, partition string) error {
			return artifacts.LockRepository(ctx, tx, partition)
		}},
	} {
		holder := holder
		t.Run(holder.name, func(t *testing.T) {
			database, ctx := openIsolatedLiveDB(t, "retire_lock_busy_"+holder.name,
				"set ESHU_POSTGRES_DSN to run the derive lock-wait budget Postgres proof")
			partition := "PARTITION:purge-live"
			scopeIDs := seedPurgeLiveScope(t, ctx, database, partition)
			seedCensus2eRows(t, ctx, database, partition, scopeIDs)
			// Two connections: the parked holder owns one, the purge the other,
			// so the connection the purge used is the one the leak check reads
			// back after the blocked exit.
			database.SetMaxOpenConns(2)
			priorLockTimeout := showLockTimeout(t, ctx, database)
			if priorLockTimeout == "10s" {
				t.Fatalf("pooled lock_timeout is already %q before the purge; the leak check needs a distinct prior value", priorLockTimeout)
			}
			store := NewRetirementStore(SQLDB{DB: database})
			if _, err := store.InsertPending(ctx, RetirementRequest{
				RetirementID: "rr_lock_busy", RepoID: partition, ScopeID: scopeIDs[0],
				ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
				IdempotencyKeyHash: "lock-busy-key",
			}); err != nil {
				t.Fatalf("InsertPending: %v", err)
			}

			parked, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin holder: %v", err)
			}
			defer func() { _ = parked.Rollback() }()
			if err := holder.take(ctx, parked, partition); err != nil {
				t.Fatalf("holder lock: %v", err)
			}

			purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
			started := time.Now()
			result, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{partition}})
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("PurgeRetired: %v", err)
			}
			if len(result.Completed) != 0 || len(result.Blocked) != 1 || result.Blocked[0].Reason != "derive_lock_busy" {
				t.Fatalf("result = %+v, want one derive_lock_busy block", result)
			}
			if elapsed < 9*time.Second || elapsed > 20*time.Second {
				t.Fatalf("purge returned after %s, want about the 10 s budget", elapsed)
			}

			// The blocked exit leaves the pooled connection's lock_timeout as it
			// was. This is a regression check on the rollback path only: a
			// session-scoped set_config is also undone by the rollback, so it
			// cannot catch one; TestPurgeDeriveLockBudgetRestoresLockTimeoutLive
			// does, on the commit path.
			if got := showLockTimeout(t, ctx, database); got != priorLockTimeout {
				t.Fatalf("pooled lock_timeout after the blocked exit = %q, want the prior %q: the purge's budget leaked past its transaction", got, priorLockTimeout)
			}

			// Nothing was written: the tombstone is still fenced and every row stays.
			var state, phase string
			if err := database.QueryRowContext(ctx, `SELECT state, phase FROM repository_retirements
				WHERE retirement_id = 'rr_lock_busy'`).Scan(&state, &phase); err != nil {
				t.Fatalf("read tombstone: %v", err)
			}
			if state != RetirementStatePending || phase != RetirementPhaseFenced {
				t.Fatalf("tombstone = %s/%s, want pending/fenced", state, phase)
			}
			for _, q := range []string{
				`SELECT count(*) FROM ingestion_scopes WHERE partition_key = $1`,
				`SELECT count(*) FROM content_files WHERE repo_id = $1`,
				`SELECT count(*) FROM projection_decisions WHERE repository_id = $1`,
			} {
				if liveCount(t, ctx, database, q, partition) == 0 {
					t.Fatalf("%q: rows vanished although the purge was blocked", q)
				}
			}

			// The retry completes once the holder ends.
			if err := parked.Rollback(); err != nil {
				t.Fatalf("release holder: %v", err)
			}
			retried, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{partition}})
			if err != nil || len(retried.Completed) != 1 || len(retried.Blocked) != 0 {
				t.Fatalf("retry = %+v, %v, want one completion", retried, err)
			}
		})
	}
}

// showLockTimeout reads lock_timeout on whichever pooled connection is free.
func showLockTimeout(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()
	var got string
	if err := database.QueryRowContext(ctx, `SHOW lock_timeout`).Scan(&got); err != nil {
		t.Fatalf("show lock_timeout: %v", err)
	}
	return got
}

// TestPurgeDeriveLockBudgetRestoresLockTimeoutLive proves the budget is
// transaction-local and puts back the value the transaction had: the deletes
// after the locks keep their prior wait behavior, and nothing outlives the
// commit. The check after the commit is what catches a session-scoped
// set_config: it survives a commit (a rollback would undo it) and leaves the
// transaction's prior value on the pooled connection.
func TestPurgeDeriveLockBudgetRestoresLockTimeoutLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_lock_restore",
		"set ESHU_POSTGRES_DSN to run the derive lock-budget restore Postgres proof")
	// One connection, so the read after the commit is the connection the
	// transaction used.
	database.SetMaxOpenConns(1)
	baseline := showLockTimeout(t, ctx, database)
	if baseline == "7s" {
		t.Fatalf("session lock_timeout is already %q; the commit check needs a distinct baseline", baseline)
	}
	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '7s'`); err != nil {
		t.Fatalf("set prior lock_timeout: %v", err)
	}
	if _, err := lockPurgeDerivedModels(ctx, tx, "PARTITION:restore"); err != nil {
		t.Fatalf("lockPurgeDerivedModels: %v", err)
	}
	rows, err := tx.QueryContext(ctx, `SHOW lock_timeout`)
	if err != nil {
		t.Fatalf("show lock_timeout: %v", err)
	}
	var got string
	if !rows.Next() {
		_ = rows.Close()
		t.Fatal("SHOW lock_timeout returned no row")
	}
	if err := rows.Scan(&got); err != nil {
		_ = rows.Close()
		t.Fatalf("scan lock_timeout: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close lock_timeout rows: %v", err)
	}
	if got != "7s" {
		t.Fatalf("lock_timeout after the locks = %q, want the prior 7s restored", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if after := showLockTimeout(t, ctx, database); after != baseline {
		t.Fatalf("lock_timeout after the commit = %q, want the session baseline %q: "+
			"the budget or its restore outlived the transaction", after, baseline)
	}
}
