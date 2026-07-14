// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentStoreClaimPartitionLeaseBlocksActivePartitionCountRescale(t *testing.T) {
	t.Parallel()

	store := NewSharedIntentStore(partitionRescaleGuardDB{})
	claimed, err := store.ClaimPartitionLease(
		context.Background(),
		string(reducer.DomainCodeCalls),
		0,
		8,
		"new-worker",
		30*time.Second,
	)
	if err != nil {
		t.Fatalf("ClaimPartitionLease: %v", err)
	}
	if claimed {
		t.Fatal("ClaimPartitionLease claimed new partition count while old count lease was active")
	}
}

// TestSharedIntentStorePartitionCountRescaleAgainstPostgres proves the
// partition-count fence with real Postgres advisory-lock and transaction
// visibility semantics. Set ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN or
// ESHU_POSTGRES_DSN to run it.
func TestSharedIntentStorePartitionCountRescaleAgainstPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres rescale proof")
	}

	ctx := context.Background()
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open bootstrap connection: %v", err)
	}
	schemaName := fmt.Sprintf("shared_intent_rescale_proof_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := bootstrapDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
		if err := bootstrapDB.Close(); err != nil {
			t.Errorf("close bootstrap connection: %v", err)
		}
	})

	scopedDSN := dsn + "?search_path=" + schemaName
	if strings.Contains(dsn, "?") {
		scopedDSN = dsn + "&search_path=" + schemaName
	}
	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open scoped connection pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(6)
	if _, err := db.ExecContext(ctx, SharedIntentSchemaSQL()); err != nil {
		t.Fatalf("create shared-intent proof tables: %v", err)
	}

	store := NewSharedIntentStore(SQLDB{DB: db})
	const domain = "repo_dependency_rescale_proof"
	const leaseTTL = 30 * time.Second

	claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 4, "process-a/worker-0-of-4", leaseTTL)
	if err != nil || !claimed {
		t.Fatalf("claim process A shard = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 4, "process-b/worker-0-of-4", leaseTTL)
	if err != nil {
		t.Fatalf("competing process B shard claim: %v", err)
	}
	if claimed {
		t.Fatal("two process-unique owners both claimed one active shard")
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE shared_projection_partition_leases"); err != nil {
		t.Fatalf("reset process-owner proof leases: %v", err)
	}

	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 2, "old-count-owner", leaseTTL)
	if err != nil || !claimed {
		t.Fatalf("claim active two-worker lease = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 4, "new-count-owner", leaseTTL)
	if err != nil {
		t.Fatalf("claim blocked four-worker lease: %v", err)
	}
	if claimed {
		t.Fatal("four-worker lease claimed while a two-worker lease was active")
	}
	if err := store.ReleasePartitionLease(ctx, domain, 0, 2, "old-count-owner"); err != nil {
		t.Fatalf("release two-worker lease: %v", err)
	}
	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 4, "new-count-owner", leaseTTL)
	if err != nil || !claimed {
		t.Fatalf("claim four-worker lease after release = %v, %v; want true, nil", claimed, err)
	}

	if _, err := db.ExecContext(ctx, "TRUNCATE shared_projection_partition_leases"); err != nil {
		t.Fatalf("reset proof leases: %v", err)
	}
	start := make(chan struct{})
	type claimResult struct {
		partitionCount int
		claimed        bool
		err            error
	}
	results := make(chan claimResult, 2)
	var workers sync.WaitGroup
	for _, partitionCount := range []int{2, 4} {
		partitionCount := partitionCount
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			won, claimErr := store.ClaimPartitionLease(
				ctx,
				domain,
				0,
				partitionCount,
				fmt.Sprintf("racing-%d-worker-owner", partitionCount),
				leaseTTL,
			)
			results <- claimResult{partitionCount: partitionCount, claimed: won, err: claimErr}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	winners := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("racing %d-worker claim: %v", result.partitionCount, result.err)
		}
		if result.claimed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("racing partition-count winners = %d, want exactly 1", winners)
	}
	assertOneActivePartitionCount(t, db, ctx, domain)

	if _, err := db.ExecContext(ctx, `
		UPDATE shared_projection_partition_leases
		SET lease_expires_at = $2
		WHERE projection_domain = $1 AND lease_owner IS NOT NULL
	`, domain, time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatalf("expire winning lease family: %v", err)
	}
	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 8, "post-expiry-owner", leaseTTL)
	if err != nil || !claimed {
		t.Fatalf("claim new count after expiry = %v, %v; want true, nil", claimed, err)
	}
	assertOneActivePartitionCount(t, db, ctx, domain)
}

func TestRepoDependencyAcceptanceUnitGateOrdersLeaseTakeoverAgainstPostgres(t *testing.T) {
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
	store := NewSharedIntentStore(SQLDB{DB: db})
	gate := NewRepoDependencyAcceptanceUnitGate(SQLDB{DB: db})
	const (
		domain = "repo_dependency_gate_proof"
		repoID = "repository:gate-proof"
	)
	ownerA := "process-a/worker-0-of-4"
	ownerB := "process-b/worker-0-of-4"
	claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 4, ownerA, 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim owner A = %v, %v; want true, nil", claimed, err)
	}

	keyA := reducer.RepoDependencyAcceptanceUnitGateKey{
		Domain: domain, AcceptanceUnitID: repoID,
		PartitionID: 0, PartitionCount: 4, LeaseOwner: ownerA,
	}
	keyB := keyA
	keyB.LeaseOwner = ownerB
	enteredA := make(chan struct{})
	releaseA := make(chan struct{})
	type gateResult struct {
		ran bool
		err error
	}
	resultA := make(chan gateResult, 1)
	go func() {
		ran, gateErr := gate.WithAcceptanceUnit(ctx, keyA, func(context.Context, reducer.RepoDependencyProjectionIntentReader) error {
			close(enteredA)
			<-releaseA
			return nil
		})
		resultA <- gateResult{ran: ran, err: gateErr}
	}()
	<-enteredA

	if _, err := db.ExecContext(ctx, `
		UPDATE shared_projection_partition_leases
		SET lease_expires_at = $2
		WHERE projection_domain = $1
	`, domain, time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatalf("expire owner A lease: %v", err)
	}
	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 4, ownerB, 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim owner B after expiry = %v, %v; want true, nil", claimed, err)
	}

	enteredB := make(chan struct{})
	resultB := make(chan gateResult, 1)
	go func() {
		ran, gateErr := gate.WithAcceptanceUnit(ctx, keyB, func(context.Context, reducer.RepoDependencyProjectionIntentReader) error {
			close(enteredB)
			return nil
		})
		resultB <- gateResult{ran: ran, err: gateErr}
	}()
	select {
	case <-enteredB:
		t.Fatal("owner B entered the same-repository gate before owner A released it")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseA)
	if result := <-resultA; result.err != nil || !result.ran {
		t.Fatalf("owner A gate result = %+v, want ran without error", result)
	}
	select {
	case <-enteredB:
	case <-ctx.Done():
		t.Fatal("owner B did not enter after owner A committed")
	}
	if result := <-resultB; result.err != nil || !result.ran {
		t.Fatalf("owner B gate result = %+v, want ran without error", result)
	}

	staleRan, err := gate.WithAcceptanceUnit(ctx, keyA, func(context.Context, reducer.RepoDependencyProjectionIntentReader) error {
		t.Fatal("expired owner A callback ran after owner B took the shard")
		return nil
	})
	if err != nil {
		t.Fatalf("stale owner A gate: %v", err)
	}
	if staleRan {
		t.Fatal("stale owner A passed lease validation after owner B takeover")
	}
}

func TestRepoDependencyAcceptanceUnitGateConnectionLossCannotTransferActiveShard(t *testing.T) {
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
	store := NewSharedIntentStore(SQLDB{DB: db})
	gate := NewRepoDependencyAcceptanceUnitGate(SQLDB{DB: db})
	const (
		domain = "repo_dependency_gate_disconnect_proof"
		repoID = "repository:gate-disconnect-proof"
	)
	ownerA := "process-a/worker-0-of-4"
	ownerB := "process-b/worker-0-of-4"
	claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 4, ownerA, 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim owner A = %v, %v; want true, nil", claimed, err)
	}

	keyA := reducer.RepoDependencyAcceptanceUnitGateKey{
		Domain: domain, AcceptanceUnitID: repoID,
		PartitionID: 0, PartitionCount: 4, LeaseOwner: ownerA,
	}
	txBackend := make(chan int, 1)
	releaseCallback := make(chan struct{})
	gateResult := make(chan error, 1)
	go func() {
		_, gateErr := gate.WithAcceptanceUnit(ctx, keyA, func(
			callbackCtx context.Context,
			reader reducer.RepoDependencyProjectionIntentReader,
		) error {
			txStore, ok := reader.(*SharedIntentStore)
			if !ok {
				return fmt.Errorf("transaction reader type = %T, want *SharedIntentStore", reader)
			}
			pid, pidErr := postgresBackendPID(callbackCtx, txStore.db)
			if pidErr != nil {
				return pidErr
			}
			txBackend <- pid
			<-releaseCallback
			return nil
		})
		gateResult <- gateErr
	}()

	pid := <-txBackend
	var terminated bool
	if err := db.QueryRowContext(ctx, "SELECT pg_terminate_backend($1)", pid).Scan(&terminated); err != nil {
		t.Fatalf("terminate gate backend %d: %v", pid, err)
	}
	if !terminated {
		t.Fatalf("terminate gate backend %d returned false", pid)
	}

	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 4, ownerB, 30*time.Second)
	if err != nil {
		t.Fatalf("owner B claim while A lease active: %v", err)
	}
	if claimed {
		t.Fatal("owner B took the shard after only A's gate connection died")
	}
	close(releaseCallback)
	if err := <-gateResult; err == nil {
		t.Fatal("gate commit after backend termination succeeded, want transaction error")
	}

	if err := store.ReleasePartitionLease(ctx, domain, 0, 4, ownerA); err != nil {
		t.Fatalf("release owner A after graph callback drained: %v", err)
	}
	claimed, err = store.ClaimPartitionLease(ctx, domain, 0, 4, ownerB, 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("owner B claim after A callback drained = %v, %v; want true, nil", claimed, err)
	}
}

func postgresBackendPID(ctx context.Context, db ExecQueryer) (int, error) {
	rows, err := db.QueryContext(ctx, "SELECT pg_backend_pid()")
	if err != nil {
		return 0, fmt.Errorf("read gate backend pid: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, fmt.Errorf("read gate backend pid returned no row")
	}
	var pid int
	if err := rows.Scan(&pid); err != nil {
		return 0, fmt.Errorf("scan gate backend pid: %w", err)
	}
	return pid, rows.Err()
}

func openSharedIntentRescaleProofDB(t *testing.T, ctx context.Context, dsn string) (*sql.DB, func()) {
	t.Helper()
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open gate proof bootstrap connection: %v", err)
	}
	schemaName := fmt.Sprintf("shared_intent_gate_proof_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create gate proof schema: %v", err)
	}
	scopedDSN := dsn + "?search_path=" + schemaName
	if strings.Contains(dsn, "?") {
		scopedDSN = dsn + "&search_path=" + schemaName
	}
	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open gate proof connection pool: %v", err)
	}
	if _, err := db.ExecContext(ctx, SharedIntentSchemaSQL()); err != nil {
		_ = db.Close()
		t.Fatalf("create gate proof tables: %v", err)
	}
	return db, func() {
		_ = db.Close()
		_, _ = bootstrapDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = bootstrapDB.Close()
	}
}

func assertOneActivePartitionCount(t *testing.T, db *sql.DB, ctx context.Context, domain string) {
	t.Helper()
	var activeRows int
	var activeCounts int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*), count(DISTINCT partition_count)
		FROM shared_projection_partition_leases
		WHERE projection_domain = $1
		  AND lease_owner IS NOT NULL
		  AND lease_expires_at > $2
	`, domain, time.Now().UTC()).Scan(&activeRows, &activeCounts); err != nil {
		t.Fatalf("read active partition-count families: %v", err)
	}
	if activeRows != 1 || activeCounts != 1 {
		t.Fatalf("active lease rows/count families = %d/%d, want 1/1", activeRows, activeCounts)
	}
}

type partitionRescaleGuardDB struct{}

func (partitionRescaleGuardDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (partitionRescaleGuardDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	if !strings.Contains(query, "pg_advisory_xact_lock") ||
		!strings.Contains(query, "shared_projection_partition_leases") ||
		!strings.Contains(query, "hashtext($1)") ||
		!strings.Contains(query, "partition_count <> $3") ||
		!strings.Contains(query, "lease_owner IS NOT NULL") ||
		!strings.Contains(query, "lease_expires_at > $6") {
		return &leaseResultRows{
			data: [][]any{{args[0].(string)}},
			idx:  -1,
		}, nil
	}
	return &leaseResultRows{idx: -1}, nil
}
