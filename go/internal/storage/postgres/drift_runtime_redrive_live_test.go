// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// configStateDriftRedriveProofDSNEnv gates this suite against a real
// Postgres instance, mirroring crossplaneRedriveProofDSNEnv and the sibling
// *_PROOF_DSN integration proofs in this package.
const configStateDriftRedriveProofDSNEnv = "ESHU_CONFIG_STATE_DRIFT_REDRIVE_PROOF_DSN"

// configStateDriftRedriveProofSchema creates a fresh, uniquely-named schema
// on the proof DSN and applies the full bootstrap layout inside it. Returns
// the DSN and schema name so callers can open their own independent
// single-connection pools pinned to the same schema -- necessary because
// search_path is a per-connection session setting, and a concurrency proof
// needs at least two genuinely independent connections to exercise a real
// Postgres-level race, not just two goroutines serialized through one
// pooled connection. Mirrors crossplaneRedriveProofSchema exactly.
func configStateDriftRedriveProofSchema(t *testing.T) (dsn string, schemaName string) {
	t.Helper()
	dsn = os.Getenv(configStateDriftRedriveProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_CONFIG_STATE_DRIFT_REDRIVE_PROOF_DSN to run the config state drift redrive integration proof")
	}

	setupDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof setup connection: %v", err)
	}
	defer func() { _ = setupDB.Close() }()
	setupDB.SetMaxOpenConns(1)

	ctx := context.Background()
	schemaName = fmt.Sprintf("config_state_drift_redrive_proof_%d", time.Now().UnixNano())
	if _, err := setupDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupDB, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Errorf("open proof cleanup connection: %v", err)
			return
		}
		defer func() { _ = cleanupDB.Close() }()
		if _, err := cleanupDB.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE"); err != nil {
			t.Errorf("drop proof schema %s: %v", schemaName, err)
		}
	})
	// "public" stays on the search_path so extension-defined operator classes
	// (pg_trgm's gin_trgm_ops, required by the content_store bootstrap
	// definition) resolve; the schema still isolates every TABLE this test
	// creates since schemaName is listed first.
	if _, err := setupDB.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: setupDB}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	return dsn, schemaName
}

// configStateDriftRedriveProofConn opens an independent single-connection
// pool pinned to schemaName via search_path. Mirrors crossplaneRedriveProofConn.
func configStateDriftRedriveProofConn(t *testing.T, dsn, schemaName string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	return db
}

// TestConfigStateDriftRedriveClaimDueDoesNotDoubleClaimConcurrentlyLive
// proves the issue #5593 P1-C requirement: two genuinely concurrent
// ClaimDue callers racing the SAME due row converge to exactly ONE winner.
// FOR UPDATE SKIP LOCKED on the ledger row (not an application-level mutex)
// is what prevents double-processing, so this must be proven against a real
// Postgres, not the fake in drift_runtime_redrive_test.go -- a fake mirrors
// intended semantics, not the executed query text, so it cannot catch an
// off-by-one, a wrong column, or a locking-predicate bug in the real SQL.
func TestConfigStateDriftRedriveClaimDueDoesNotDoubleClaimConcurrentlyLive(t *testing.T) {
	dsn, schema := configStateDriftRedriveProofSchema(t)
	dbA := configStateDriftRedriveProofConn(t, dsn, schema)
	dbB := configStateDriftRedriveProofConn(t, dsn, schema)

	storeA := NewConfigStateDriftRedriveStore(SQLDB{DB: dbA})
	storeB := NewConfigStateDriftRedriveStore(SQLDB{DB: dbB})

	ctx := context.Background()
	now := time.Now().UTC()
	const scopeID, generationID = "state_snapshot:s3:race", "gen-race-001"
	if err := storeA.EnsureScheduled(ctx, scopeID, generationID, now); err != nil {
		t.Fatalf("EnsureScheduled: %v", err)
	}

	var wg sync.WaitGroup
	claimsA := make([]ConfigStateDriftRedriveClaim, 0, 1)
	claimsB := make([]ConfigStateDriftRedriveClaim, 0, 1)
	var errA, errB error
	wg.Add(2)
	go func() {
		defer wg.Done()
		claimsA, errA = storeA.ClaimDue(ctx, 4, 10, now.Add(5*time.Minute))
	}()
	go func() {
		defer wg.Done()
		claimsB, errB = storeB.ClaimDue(ctx, 4, 10, now.Add(5*time.Minute))
	}()
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("claim errors: A=%v B=%v", errA, errB)
	}

	total := len(claimsA) + len(claimsB)
	if total != 1 {
		t.Fatalf("expected exactly ONE of the two concurrent ClaimDue calls to claim the row, got %d total claims (A=%d, B=%d) -- FOR UPDATE SKIP LOCKED must serialize this, not let both claim it", total, len(claimsA), len(claimsB))
	}

	// The row must show exactly one increment (attempt_count=1), not two --
	// proving the winner's UPDATE was atomic and the loser genuinely
	// SKIPPED rather than blocking and then ALSO incrementing.
	verifyDB := configStateDriftRedriveProofConn(t, dsn, schema)
	row := verifyDB.QueryRowContext(ctx, "SELECT attempt_count FROM config_state_drift_redrive WHERE scope_id = $1 AND generation_id = $2", scopeID, generationID)
	var attemptCount int
	if err := row.Scan(&attemptCount); err != nil {
		t.Fatalf("verify attempt_count: %v", err)
	}
	if attemptCount != 1 {
		t.Fatalf("attempt_count after concurrent claim = %d, want 1 (exactly one winner's atomic advance, not a double-increment)", attemptCount)
	}
}

// TestConfigStateDriftRedriveClaimDueConcurrentFinalAttemptDeletesExactlyOnceLive
// proves the SAME concurrent-claim safety holds for the DELETE-on-exhaustion
// path (issue #5593 P1-B): two concurrent ClaimDue callers racing a row on
// its FINAL allowed attempt must converge on exactly one winner claiming
// (and deleting) it, and the row must actually be gone afterward -- not
// left in a half-deleted or double-deleted state.
func TestConfigStateDriftRedriveClaimDueConcurrentFinalAttemptDeletesExactlyOnceLive(t *testing.T) {
	dsn, schema := configStateDriftRedriveProofSchema(t)
	dbA := configStateDriftRedriveProofConn(t, dsn, schema)
	dbB := configStateDriftRedriveProofConn(t, dsn, schema)
	seedDB := configStateDriftRedriveProofConn(t, dsn, schema)

	ctx := context.Background()
	now := time.Now().UTC()
	const scopeID, generationID = "state_snapshot:s3:race-final", "gen-race-final-001"
	const maxAttempts = 2

	// Seed the row directly at attempt_count = maxAttempts-1 (its last
	// allowed attempt) so both concurrent claimants race the DELETE path.
	if _, err := seedDB.ExecContext(ctx, `
		INSERT INTO config_state_drift_redrive (scope_id, generation_id, attempt_count, next_attempt_at, first_scheduled_at, updated_at)
		VALUES ($1, $2, $3, $4, $4, $4)
	`, scopeID, generationID, maxAttempts-1, now); err != nil {
		t.Fatalf("seed ledger row: %v", err)
	}

	storeA := NewConfigStateDriftRedriveStore(SQLDB{DB: dbA})
	storeB := NewConfigStateDriftRedriveStore(SQLDB{DB: dbB})

	var wg sync.WaitGroup
	claimsA := make([]ConfigStateDriftRedriveClaim, 0, 1)
	claimsB := make([]ConfigStateDriftRedriveClaim, 0, 1)
	var errA, errB error
	wg.Add(2)
	go func() {
		defer wg.Done()
		claimsA, errA = storeA.ClaimDue(ctx, maxAttempts, 10, now.Add(5*time.Minute))
	}()
	go func() {
		defer wg.Done()
		claimsB, errB = storeB.ClaimDue(ctx, maxAttempts, 10, now.Add(5*time.Minute))
	}()
	wg.Wait()

	if errA != nil || errB != nil {
		t.Fatalf("claim errors: A=%v B=%v", errA, errB)
	}

	total := len(claimsA) + len(claimsB)
	if total != 1 {
		t.Fatalf("expected exactly ONE of the two concurrent ClaimDue calls to claim+delete the row, got %d total claims (A=%d, B=%d)", total, len(claimsA), len(claimsB))
	}
	var winner ConfigStateDriftRedriveClaim
	if len(claimsA) == 1 {
		winner = claimsA[0]
	} else {
		winner = claimsB[0]
	}
	if !winner.Exhausted {
		t.Fatal("winning claim Exhausted = false, want true (this was the row's final attempt)")
	}
	if winner.AttemptCount != maxAttempts {
		t.Fatalf("winning claim AttemptCount = %d, want %d", winner.AttemptCount, maxAttempts)
	}

	verifyDB := configStateDriftRedriveProofConn(t, dsn, schema)
	var remaining int
	if err := verifyDB.QueryRowContext(ctx, "SELECT count(*) FROM config_state_drift_redrive WHERE scope_id = $1 AND generation_id = $2", scopeID, generationID).Scan(&remaining); err != nil {
		t.Fatalf("verify row deleted: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining rows for the raced key = %d, want 0 (P1-B: exactly one DELETE, row must be gone)", remaining)
	}
}
