// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestRepoDependencyLeaseOwnerActiveUsesWallClockTimestamp(t *testing.T) {
	if !strings.Contains(repoDependencyLeaseOwnerActiveSQL, "clock_timestamp()") {
		t.Fatal("lease validation must use wall-clock time after waiting for the repository lock")
	}
	if strings.Contains(repoDependencyLeaseOwnerActiveSQL, "CURRENT_TIMESTAMP") {
		t.Fatal("transaction-start time can accept a lease that expired while the repository lock was blocked")
	}
}

func TestReducerContentionGateRepoDependencyAcceptanceUnitGateRejectsLeaseExpiredWhileWaitingForRepoLock(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres gate proof")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, cleanup := openSharedIntentRescaleProofDB(t, ctx, dsn)
	defer cleanup()

	const (
		domain = "repo_dependency_gate_expiry_proof"
		repoID = "repository:gate-expiry-proof"
		owner  = "process-a/worker-0-of-4"
	)
	store := NewSharedIntentStore(SQLDB{DB: db})
	claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 4, owner, 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim owner lease = %v, %v; want true, nil", claimed, err)
	}

	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin repository-lock blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback() }()
	if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, SQLTx{Tx: blocker}, []string{repoID}); err != nil {
		t.Fatalf("hold repository lock: %v", err)
	}

	lockAttempted := make(chan struct{})
	gate := NewRepoDependencyAcceptanceUnitGate(&signalingRepoDependencyGateBeginner{
		inner:         SQLDB{DB: db},
		lockAttempted: lockAttempted,
	})
	key := reducer.RepoDependencyAcceptanceUnitGateKey{
		Domain: domain, AcceptanceUnitID: repoID,
		PartitionID: 0, PartitionCount: 4, LeaseOwner: owner,
	}
	type gateResult struct {
		ran bool
		err error
	}
	callbackRan := make(chan struct{}, 1)
	result := make(chan gateResult, 1)
	go func() {
		ran, gateErr := gate.WithAcceptanceUnit(ctx, key, func(context.Context, reducer.RepoDependencyProjectionIntentReader) error {
			callbackRan <- struct{}{}
			return nil
		})
		result <- gateResult{ran: ran, err: gateErr}
	}()

	select {
	case <-lockAttempted:
	case <-ctx.Done():
		t.Fatal("gate did not attempt the blocked repository lock")
	}
	var expiresAt time.Time
	if err := db.QueryRowContext(ctx, `
		UPDATE shared_projection_partition_leases
		SET lease_expires_at = clock_timestamp() + interval '200 milliseconds'
		WHERE projection_domain = $1 AND partition_id = 0 AND partition_count = 4
		RETURNING lease_expires_at
	`, domain).Scan(&expiresAt); err != nil {
		t.Fatalf("set lease expiry after gate transaction began: %v", err)
	}
	waitForPostgresWallClockAfter(t, ctx, db, expiresAt)
	if err := blocker.Commit(); err != nil {
		t.Fatalf("release repository-lock blocker: %v", err)
	}

	got := <-result
	if got.err != nil || got.ran {
		t.Fatalf("expired gate result = %+v, want ran=false and err=nil", got)
	}
	select {
	case <-callbackRan:
		t.Fatal("callback ran after the lease expired while waiting for the repository lock")
	default:
	}
}

type signalingRepoDependencyGateBeginner struct {
	inner         Beginner
	lockAttempted chan struct{}
}

func (b *signalingRepoDependencyGateBeginner) Begin(ctx context.Context) (Transaction, error) {
	tx, err := b.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &signalingRepoDependencyGateTransaction{Transaction: tx, lockAttempted: b.lockAttempted}, nil
}

type signalingRepoDependencyGateTransaction struct {
	Transaction
	lockAttempted chan struct{}
	once          sync.Once
}

func (tx *signalingRepoDependencyGateTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "pg_advisory_xact_lock") {
		tx.once.Do(func() { close(tx.lockAttempted) })
	}
	return tx.Transaction.ExecContext(ctx, query, args...)
}

func waitForPostgresWallClockAfter(t *testing.T, ctx context.Context, db *sql.DB, after time.Time) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var elapsed bool
		if err := db.QueryRowContext(ctx, "SELECT clock_timestamp() > $1", after).Scan(&elapsed); err != nil {
			t.Fatalf("read Postgres wall clock: %v", err)
		}
		if elapsed {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Postgres wall clock did not pass the forced lease expiry")
		}
	}
}
