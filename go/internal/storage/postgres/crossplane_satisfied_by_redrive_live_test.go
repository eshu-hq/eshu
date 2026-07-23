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

// crossplaneRedriveProofDSNEnv gates this suite against a real Postgres
// instance, mirroring the sibling *_PROOF_DSN integration proofs in this
// package (e.g. ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN).
const crossplaneRedriveProofDSNEnv = "ESHU_CROSSPLANE_REDRIVE_PROOF_DSN"

// crossplaneRedriveProofSchema creates a fresh, uniquely-named schema on the
// proof DSN and applies the full bootstrap layout inside it (this feature's
// target-discovery query joins fact_records, ingestion_scopes,
// scope_generations, and fact_work_items, so the whole layout is needed, not
// just migration 076). Returns the DSN and schema name so callers can open
// their own independent single-connection pools pinned to the same schema --
// necessary because search_path is a per-connection session setting, and a
// concurrency proof needs at least two genuinely independent connections to
// exercise a real Postgres-level race, not just two goroutines serialized
// through one pooled connection.
func crossplaneRedriveProofSchema(t *testing.T) (dsn string, schemaName string) {
	t.Helper()
	dsn = os.Getenv(crossplaneRedriveProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_CROSSPLANE_REDRIVE_PROOF_DSN to run the crossplane redrive integration proof")
	}

	setupDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof setup connection: %v", err)
	}
	defer func() { _ = setupDB.Close() }()
	setupDB.SetMaxOpenConns(1)

	ctx := context.Background()
	schemaName = fmt.Sprintf("crossplane_redrive_proof_%d", time.Now().UnixNano())
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

// crossplaneRedriveProofConn opens an independent single-connection pool
// pinned to schemaName via search_path. Independent from any other pool
// opened this way, so two calls give two genuinely concurrent Postgres
// sessions for a real FOR UPDATE SKIP LOCKED race proof.
func crossplaneRedriveProofConn(t *testing.T, dsn, schemaName string) *sql.DB {
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

// TestCrossplaneRedriveStateConcurrentClaimConvergesLive proves two
// genuinely concurrent sweepers racing to claim the SAME (xrd_scope_id,
// xrd_generation_id) row converge to exactly one winner: FOR UPDATE SKIP
// LOCKED on the marker/claim row (not an application-level mutex) is what
// prevents double-processing, so this must be proven against a real
// Postgres, not a fake.
func TestCrossplaneRedriveStateConcurrentClaimConvergesLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	dbA := crossplaneRedriveProofConn(t, dsn, schema)
	dbB := crossplaneRedriveProofConn(t, dsn, schema)

	stateA := NewCrossplaneRedriveStateStore(SQLDB{DB: dbA})
	stateB := NewCrossplaneRedriveStateStore(SQLDB{DB: dbB})

	ctx := context.Background()
	const xrdScopeID, xrdGenerationID = "scope-xrd-race", "gen-xrd-race-001"
	if err := stateA.EnsureQueued(ctx, xrdScopeID, xrdGenerationID); err != nil {
		t.Fatalf("EnsureQueued: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = stateA.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-a", time.Minute)
	}()
	go func() {
		defer wg.Done()
		results[1], errs[1] = stateB.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-b", time.Minute)
	}()
	wg.Wait()

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("claim errors: a=%v b=%v", errs[0], errs[1])
	}
	if results[0] == results[1] {
		t.Fatalf("expected exactly one claim winner, got a=%v b=%v", results[0], results[1])
	}

	// The non-winner must be able to claim a DIFFERENT generation without
	// interference (SKIP LOCKED must not deadlock or wrongly block unrelated
	// rows).
	const otherGenerationID = "gen-xrd-race-002"
	if err := stateA.EnsureQueued(ctx, xrdScopeID, otherGenerationID); err != nil {
		t.Fatalf("EnsureQueued other generation: %v", err)
	}
	otherClaimed, err := stateB.ClaimExact(ctx, xrdScopeID, otherGenerationID, "owner-b", time.Minute)
	if err != nil {
		t.Fatalf("claim other generation: %v", err)
	}
	if !otherClaimed {
		t.Fatalf("expected the other generation's row to be claimable independently")
	}
}

// TestCrossplaneRedriveStateCrashRecoveryLive proves the design's required
// crash-recovery shape: a marker absent from the state table means a fresh
// sweep is needed (EnsureQueued+ClaimExact succeeds); a marker present as
// 'completed' means no-op (ClaimExact fails); and a claim whose owning
// process crashed (never called MarkCompleted) is reclaimed once its lease
// expires, but NOT before.
func TestCrossplaneRedriveStateCrashRecoveryLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)

	fakeNow := time.Date(2026, time.January, 2, 3, 0, 0, 0, time.UTC)
	state := NewCrossplaneRedriveStateStore(SQLDB{DB: db})
	state.Now = func() time.Time { return fakeNow }

	ctx := context.Background()
	const xrdScopeID, xrdGenerationID = "scope-xrd-crash", "gen-xrd-crash-001"

	// Marker absent -> full sweep: EnsureQueued creates the row, ClaimExact
	// succeeds immediately.
	if err := state.EnsureQueued(ctx, xrdScopeID, xrdGenerationID); err != nil {
		t.Fatalf("EnsureQueued: %v", err)
	}
	claimed, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-crashed", time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact: %v", err)
	}
	if !claimed {
		t.Fatalf("expected first claim to succeed on a freshly queued row")
	}

	// Simulate the owning process crashing mid-sweep: it never calls
	// MarkCompleted. Before the lease expires, a second claimant must NOT be
	// able to reclaim it (this would double-process the same sweep).
	stillLeased, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-b", time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact before expiry: %v", err)
	}
	if stillLeased {
		t.Fatalf("expected claim to be rejected while the original lease is still live")
	}

	// Advance time past the original lease's expiry: a new claimant now
	// reclaims the abandoned sweep.
	state.Now = func() time.Time { return fakeNow.Add(2 * time.Minute) }
	reclaimed, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-b", time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact after expiry: %v", err)
	}
	if !reclaimed {
		t.Fatalf("expected an expired lease to be reclaimable")
	}

	// owner-b completes it. The stale owner-crashed's own completion call
	// must be a fenced no-op (it no longer holds the claim).
	completedByStaleOwner, err := state.MarkCompleted(ctx, xrdScopeID, xrdGenerationID, "owner-crashed")
	if err != nil {
		t.Fatalf("MarkCompleted (stale owner): %v", err)
	}
	if completedByStaleOwner {
		t.Fatalf("expected the reclaimed, now-stale owner's completion to be a fenced no-op")
	}
	completedByCurrentOwner, err := state.MarkCompleted(ctx, xrdScopeID, xrdGenerationID, "owner-b")
	if err != nil {
		t.Fatalf("MarkCompleted (current owner): %v", err)
	}
	if !completedByCurrentOwner {
		t.Fatalf("expected the current claim-holder's completion to succeed")
	}

	// Marker present as 'completed' -> no-op: neither owner can claim again.
	noopClaim, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-c", time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact after completion: %v", err)
	}
	if noopClaim {
		t.Fatalf("expected a completed generation's sweep to never be reclaimable")
	}
}
