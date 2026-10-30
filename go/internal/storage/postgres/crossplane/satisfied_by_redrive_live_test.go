// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// crossplaneRedriveProofDSNEnv gates this suite against a real Postgres
// instance, mirroring the sibling *_PROOF_DSN integration proofs in this
// package (e.g. ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN).
const crossplaneRedriveProofDSNEnv = "ESHU_CROSSPLANE_REDRIVE_PROOF_DSN"

// crossplaneRedriveProofDisposableEnv is the explicit opt-in behind creating
// the suite's disposable databases.
const crossplaneRedriveProofDisposableEnv = "ESHU_CROSSPLANE_REDRIVE_PROOF_DISPOSABLE"

// crossplaneRedriveProofDB creates a fresh disposable database, applies the
// full bootstrap layout on its public schema, and returns the open pool
// plus a DSN targeting that database (this feature's target-discovery query
// joins fact_records, ingestion_scopes, scope_generations, and
// fact_work_items, so the whole layout is needed, not just migration 076).
// The database is dropped when the test ends.
//
// Database-level isolation (not an isolated schema): the lane's 241a/241b
// authority migrations are public-only, so ApplyBootstrapWithOptions skips
// them on an isolated schema and the later 242a definition then fails with
// a missing eshu_security schema. A fresh database keeps the full install
// and stays private to the proof.
//
// The runner's ESHU_CROSSPLANE_REDRIVE_PROOF_DSN/_DISPOSABLE pair wins;
// local runs fall back to the generic ESHU_POSTGRES_TEST_DSN/_DISPOSABLE
// pair. A family DSN set without its explicit opt-in fails closed; an
// unset pair skips, never fails.
func crossplaneRedriveProofDB(t *testing.T) (database *sql.DB, targetDSN string) {
	t.Helper()
	adminDSN := strings.TrimSpace(os.Getenv(crossplaneRedriveProofDSNEnv))
	var optIn string
	if adminDSN != "" {
		if optIn = os.Getenv(crossplaneRedriveProofDisposableEnv); optIn != "1" {
			t.Fatal("ESHU_CROSSPLANE_REDRIVE_PROOF_DSN is set without ESHU_CROSSPLANE_REDRIVE_PROOF_DISPOSABLE=1")
		}
	} else {
		adminDSN = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
		optIn = os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	}
	if adminDSN == "" {
		t.Skip("set ESHU_CROSSPLANE_REDRIVE_PROOF_DSN and ESHU_CROSSPLANE_REDRIVE_PROOF_DISPOSABLE=1 " +
			"(or the ESHU_POSTGRES_TEST_DSN/_DISPOSABLE pair) to run the crossplane redrive integration proof")
	}
	admin, err := postgresproof.WithDatabase(adminDSN, "postgres")
	if err != nil {
		t.Fatalf("retarget crossplane redrive proof DSN: %v", err)
	}
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, admin, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
		})
	var name string
	if err := database.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("read proof database: %v", err)
	}
	if targetDSN, err = postgresproof.WithDatabase(admin, name); err != nil {
		t.Fatalf("target crossplane redrive proof database: %v", err)
	}
	return database, targetDSN
}

// crossplaneRedriveProofConn opens an independent single-connection pool on
// the proof's disposable database. Independent from any other pool opened
// this way, so two calls give two genuinely concurrent Postgres sessions
// for a real FOR UPDATE SKIP LOCKED race proof.
func crossplaneRedriveProofConn(t *testing.T, targetDSN string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", targetDSN)
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	return db
}

// TestCrossplaneRedriveStateConcurrentClaimConvergesLive proves two
// genuinely concurrent sweepers racing to claim the SAME (xrd_scope_id,
// xrd_generation_id) row converge to exactly one winner: FOR UPDATE SKIP
// LOCKED on the marker/claim row (not an application-level mutex) is what
// prevents double-processing, so this must be proven against a real
// Postgres, not a fake.
func TestCrossplaneRedriveStateConcurrentClaimConvergesLive(t *testing.T) {
	dbA, targetDSN := crossplaneRedriveProofDB(t)
	dbB := crossplaneRedriveProofConn(t, targetDSN)

	stateA := crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: dbA})
	stateB := crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: dbB})

	ctx := context.Background()
	const xrdScopeID, xrdGenerationID = "scope-xrd-race", "gen-xrd-race-001"
	if err := stateA.EnsureQueued(ctx, xrdScopeID, xrdGenerationID); err != nil {
		t.Fatalf("EnsureQueued: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]bool, 2)
	tokens := make([]int64, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], tokens[0], errs[0] = stateA.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-a", time.Minute)
	}()
	go func() {
		defer wg.Done()
		results[1], tokens[1], errs[1] = stateB.ClaimExact(ctx, xrdScopeID, xrdGenerationID, "owner-b", time.Minute)
	}()
	wg.Wait()

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("claim errors: a=%v b=%v", errs[0], errs[1])
	}
	if results[0] == results[1] {
		t.Fatalf("expected exactly one claim winner, got a=%v b=%v", results[0], results[1])
	}
	var winnerToken int64
	if results[0] {
		winnerToken = tokens[0]
	} else {
		winnerToken = tokens[1]
	}
	if winnerToken <= 0 {
		t.Fatalf("expected the winning claim to receive a positive fencing token, got %d", winnerToken)
	}

	// The non-winner must be able to claim a DIFFERENT generation without
	// interference (SKIP LOCKED must not deadlock or wrongly block unrelated
	// rows).
	const otherGenerationID = "gen-xrd-race-002"
	if err := stateA.EnsureQueued(ctx, xrdScopeID, otherGenerationID); err != nil {
		t.Fatalf("EnsureQueued other generation: %v", err)
	}
	otherClaimed, _, err := stateB.ClaimExact(ctx, xrdScopeID, otherGenerationID, "owner-b", time.Minute)
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
//
// This is also the P2-f split-brain regression: production wires ONE static
// per-process-class Owner string ("projector") shared by every replica AND
// the periodic catch-up scanner (mirroring ProjectorQueue/ReducerQueue's own
// LeaseOwner convention) -- so every ClaimExact call below deliberately uses
// the SAME owner string throughout, exactly like a crashed invocation and its
// reclaiming invocation would in production. If MarkCompleted fenced on
// claimed_by alone, the stale invocation's completion would incorrectly
// succeed because claimed_by still reads "same-owner-throughout"; fencing on
// the bumped claim_fencing_token instead closes that gap.
func TestCrossplaneRedriveStateCrashRecoveryLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)

	fakeNow := time.Date(2026, time.January, 2, 3, 0, 0, 0, time.UTC)
	state := crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: db})
	state.Now = func() time.Time { return fakeNow }

	ctx := context.Background()
	const xrdScopeID, xrdGenerationID = "scope-xrd-crash", "gen-xrd-crash-001"
	const sameOwnerThroughout = "projector" // every claim below uses this SAME string

	// Marker absent -> full sweep: EnsureQueued creates the row, ClaimExact
	// succeeds immediately.
	if err := state.EnsureQueued(ctx, xrdScopeID, xrdGenerationID); err != nil {
		t.Fatalf("EnsureQueued: %v", err)
	}
	claimed, firstToken, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, sameOwnerThroughout, time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact: %v", err)
	}
	if !claimed {
		t.Fatalf("expected first claim to succeed on a freshly queued row")
	}

	// Simulate the owning process crashing mid-sweep: it never calls
	// MarkCompleted. Before the lease expires, a second claimant (same owner
	// string) must NOT be able to reclaim it (this would double-process the
	// same sweep).
	stillLeased, _, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, sameOwnerThroughout, time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact before expiry: %v", err)
	}
	if stillLeased {
		t.Fatalf("expected claim to be rejected while the original lease is still live")
	}

	// Advance time past the original lease's expiry: a new claimant (SAME
	// owner string as the crashed one) now reclaims the abandoned sweep and
	// receives a NEW, higher fencing token.
	state.Now = func() time.Time { return fakeNow.Add(2 * time.Minute) }
	reclaimed, secondToken, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, sameOwnerThroughout, time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact after expiry: %v", err)
	}
	if !reclaimed {
		t.Fatalf("expected an expired lease to be reclaimable")
	}
	if secondToken <= firstToken {
		t.Fatalf("expected the reclaim's fencing token (%d) to exceed the original claim's (%d)", secondToken, firstToken)
	}

	// The stale (crashed) invocation "wakes up" and tries to complete using
	// its ORIGINAL token, presenting the SAME owner string the reclaiming
	// invocation also used. This must be a fenced no-op: claimed_by alone
	// cannot distinguish these two invocations, only the token can.
	completedByStaleInvocation, err := state.MarkCompleted(ctx, xrdScopeID, xrdGenerationID, firstToken)
	if err != nil {
		t.Fatalf("MarkCompleted (stale invocation, stale token): %v", err)
	}
	if completedByStaleInvocation {
		t.Fatalf("expected the stale invocation's completion (superseded token %d) to be a fenced no-op", firstToken)
	}
	completedByCurrentInvocation, err := state.MarkCompleted(ctx, xrdScopeID, xrdGenerationID, secondToken)
	if err != nil {
		t.Fatalf("MarkCompleted (current invocation, current token): %v", err)
	}
	if !completedByCurrentInvocation {
		t.Fatalf("expected the current claim-holder's completion (token %d) to succeed", secondToken)
	}

	// Marker present as 'completed' -> no-op: no further claim succeeds, even
	// with the current (now-stale-again) token or the same owner string.
	noopClaim, _, err := state.ClaimExact(ctx, xrdScopeID, xrdGenerationID, sameOwnerThroughout, time.Minute)
	if err != nil {
		t.Fatalf("ClaimExact after completion: %v", err)
	}
	if noopClaim {
		t.Fatalf("expected a completed generation's sweep to never be reclaimable")
	}
}
