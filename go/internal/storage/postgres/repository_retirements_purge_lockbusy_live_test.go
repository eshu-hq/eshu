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

// TestPurgeDeriveLockBudgetRestoresLockTimeoutLive proves the budget is
// transaction-local and puts back the value the transaction had: the deletes
// after the locks keep their prior wait behavior.
func TestPurgeDeriveLockBudgetRestoresLockTimeoutLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_lock_restore",
		"set ESHU_POSTGRES_DSN to run the derive lock-budget restore Postgres proof")
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
	defer func() { _ = rows.Close() }()
	var got string
	if !rows.Next() {
		t.Fatal("SHOW lock_timeout returned no row")
	}
	if err := rows.Scan(&got); err != nil {
		t.Fatalf("scan lock_timeout: %v", err)
	}
	if got != "7s" {
		t.Fatalf("lock_timeout after the locks = %q, want the prior 7s restored", got)
	}
}
