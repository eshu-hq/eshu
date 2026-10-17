// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8283 pass-vs-claim-vs-ACK double-pass interleave: pass A snapshots the changed-evidence listing, pass B reopens and commits, a worker claims, pass A's resumed reopen UPDATE is a status-guard no-op, the worker's claim-fenced success write lands and its ACK succeeds, the next pass leaves the unchanged item succeeded, and a later foreign change re-opens it; isolated schema from ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN; 2.6 s on local PostgreSQL 18.6 on 2026-10-17, also run under -race; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestDeployableUnitReopenFingerprintInterleavePassVsClaimVsAckVsWriteLive
// walks one item through an overlap of two maintenance passes, a worker
// claim, the claim-fenced fingerprint success write, and the ACK, reusing the
// merged #8283 helpers (no duplicate fixture of its own):
//
//  1. pass A lists the item as changed in its snapshot and stops before its
//     reopen UPDATE;
//  2. pass B reopens the item and commits;
//  3. a worker claims it;
//  4. pass A resumes and runs its reopen UPDATE: the status guard makes it a
//     no-op, so the claimed item is not reopened twice and no lease is lost;
//  5. the worker records a fresh fingerprint through the claim-fenced write
//     and ACKs;
//  6. the next pass finds the item unchanged and leaves it succeeded;
//  7. a foreign change after that is not lost: the next pass reopens it.
//
// Pass A holds its snapshot on its own single-connection handle while every
// other step runs on a second handle against the same isolated schema, so the
// overlap is a real database interleave, not pool serialization.
func TestDeployableUnitReopenFingerprintInterleavePassVsClaimVsAckVsWriteLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #8283 reopen interleave proof")
	}
	ctx := context.Background()
	dbA, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)
	dbB := openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)
	w := seedFingerprintGateWorld(t, ctx, dbB)

	// A re-run deployment_mapping appends a row under scope B's already-active
	// generation: no generation id moves, so only the row-set digest moves.
	upsertFingerprintGateDeploysFrom(t, ctx, dbB, w.b.genID, "repo-b-worker", "repo-a")

	// Step 1: pass A snapshots the changed-evidence listing and stops before
	// its reopen UPDATE.
	txA, err := dbA.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin pass A: %v", err)
	}
	defer func() { _ = txA.Rollback() }()
	reopen, _, err := listDeployableUnitReopenCandidates(ctx, SQLTx{Tx: txA}, string(reducer.DomainDeployableUnitCorrelation))
	if err != nil {
		t.Fatalf("pass A listing: %v", err)
	}
	if !slices.Contains(reopen, "duc-a") {
		t.Fatalf("pass A listing = %v, want duc-a as changed", reopen)
	}

	// Step 2: pass B reopens the item and commits.
	runFingerprintGatePass(t, ctx, dbB, w.at.Add(time.Minute))
	if got := workItemStatus(t, ctx, dbB, "duc-a"); got != "pending" {
		t.Fatalf("status after pass B = %q, want pending", got)
	}

	// Step 3: a worker claims it.
	claim := claimFingerprintGateItem(t, ctx, dbB, "duc-a", w.a.scopeID, w.a.genID,
		reducer.DomainDeployableUnitCorrelation, w.at.Add(time.Second))
	attemptsClaimed := workItemAttempts(t, ctx, dbB, "duc-a")

	// Step 4: pass A resumes. Its reopen UPDATE must be a status-guard no-op:
	// the item is claimed, not succeeded, so nothing is transitioned and the
	// lease survives.
	reopened, err := ReducerQueue{database: SQLTx{Tx: txA}, Now: time.Now}.ReopenSucceeded(ctx, "duc-a")
	if err != nil {
		t.Fatalf("pass A reopen UPDATE: %v", err)
	}
	if reopened {
		t.Fatal("pass A reopened a claimed item: the status guard failed and the item would run twice")
	}
	if err := txA.Commit(); err != nil {
		t.Fatalf("commit pass A: %v", err)
	}
	if got := workItemStatus(t, ctx, dbB, "duc-a"); got != "running" {
		t.Fatalf("status after pass A resumed = %q, want running (the claim survives the stale pass)", got)
	}
	if got := workItemAttempts(t, ctx, dbB, "duc-a"); got != attemptsClaimed {
		t.Fatalf("attempt_count after pass A resumed = %d, want %d (the stale pass must not consume a retry)", got, attemptsClaimed)
	}

	// Step 5: the worker records a fresh fingerprint through the claim-fenced
	// success write — a fresh read after the foreign change — and ACKs. The
	// write must land: the claim still holds the item.
	read := readFingerprintGateInput(t, ctx, dbB, []string{"repo-a"})
	if err := recordFingerprintGateInput(ctx, dbB, claim, read); err != nil {
		t.Fatalf("record fingerprint after the stale pass: %v", err)
	}
	if got := fingerprintRowCount(t, ctx, dbB, "duc-a"); got != 1 {
		t.Fatalf("fingerprint rows after the success write = %d, want 1", got)
	}
	ackFingerprintGateItem(t, ctx, dbB, "duc-a", w.at.Add(2*time.Second))
	if got := workItemStatus(t, ctx, dbB, "duc-a"); got != "succeeded" {
		t.Fatalf("status after ack = %q, want succeeded", got)
	}

	// Step 6: the next pass finds the item unchanged and leaves it succeeded.
	runFingerprintGatePass(t, ctx, dbB, w.at.Add(time.Minute))
	if got := workItemStatus(t, ctx, dbB, "duc-a"); got != "succeeded" {
		t.Fatalf("status after a pass over an unchanged item = %q, want succeeded", got)
	}
	if got := workItemStatus(t, ctx, dbB, "duc-c"); got != "succeeded" {
		t.Fatalf("control duc-c status after a pass = %q, want succeeded", got)
	}

	// Step 7: a foreign change after the ACK is not lost — the next pass
	// reopens the item, and only its reader.
	upsertFingerprintGateDeploysFrom(t, ctx, dbB, w.b.genID, "repo-b-worker-2", "repo-a")
	runFingerprintGatePass(t, ctx, dbB, w.at.Add(time.Minute))
	if got := workItemStatus(t, ctx, dbB, "duc-a"); got != "pending" {
		t.Fatalf("status after a later foreign change = %q, want pending (a lost reopen)", got)
	}
	if got := workItemStatus(t, ctx, dbB, "duc-c"); got != "succeeded" {
		t.Fatalf("control duc-c status after a foreign change elsewhere = %q, want succeeded", got)
	}
}

// workItemAttempts reads one work item's current attempt_count.
func workItemAttempts(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) int {
	t.Helper()
	var attempts int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(attempt_count, 0) FROM fact_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&attempts); err != nil {
		t.Fatalf("read attempt_count for work item %q: %v", workItemID, err)
	}
	return attempts
}
