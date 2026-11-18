// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that classification re-checks a generation row under its own lock: an activation that commits after the classification statement took its snapshot and before it locks the row (staged by gating the shipped statement on an advisory lock the test holds) leaves the generation unclaimed with no lock held on the now active row, and a whole pass over the same race counts it as left for a later pass, never as swept, with every row intact
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// classifyGateKey is the single-integer advisory key the staged statement waits
// on. It is not the sweep's lock class (that one is two integers).
const classifyGateKey = 1082301

// gatedClassify derives the staging statement from the shipped classification
// statement by one insertion: the first conjunct of the candidate CTE becomes a
// wait for the advisory lock the test holds. The candidate CTE is evaluated in
// the statement's snapshot and the row lock comes after it, so the wait sits
// between the two. The helper fails unless the insertion applies exactly once
// and removing it gives the shipped statement back, so the staged statement can
// never drift from production.
func gatedClassify(t *testing.T) string {
	t.Helper()
	const anchor = "    WHERE NOT EXISTS (SELECT 1 FROM scope_generations sg"
	gate := fmt.Sprintf("    WHERE (SELECT pg_advisory_xact_lock(%d))::text IS NOT NULL\n      AND NOT EXISTS (SELECT 1 FROM scope_generations sg", classifyGateKey)
	if n := strings.Count(relationshipretention.ClassifySQL, anchor); n != 1 {
		t.Fatalf("the classification statement has %d candidate predicates to gate, want exactly 1:\n%s", n, relationshipretention.ClassifySQL)
	}
	derived := strings.Replace(relationshipretention.ClassifySQL, anchor, gate, 1)
	if strings.Replace(derived, gate, anchor, 1) != relationshipretention.ClassifySQL {
		t.Fatal("the gated statement is not the shipped statement plus one insertion")
	}
	return derived
}

// stageActivationRace holds the gate, so a classification of generation gen
// stops after its snapshot, and returns a function that commits the activation
// (the generation row turns active) and releases the gate in one transaction.
func (f *liveFixture) stageActivationRace(t *testing.T, gen string) (activate func()) {
	t.Helper()
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin the activation: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(f.ctx, `SELECT pg_advisory_xact_lock($1)`, classifyGateKey); err != nil {
		t.Fatalf("hold the classification gate: %v", err)
	}
	return func() {
		if _, err := tx.ExecContext(f.ctx, `UPDATE relationship_generations SET status = 'active', activated_at = now() WHERE generation_id = $1`, gen); err != nil {
			t.Fatalf("activate %s: %v", gen, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit the activation: %v", err)
		}
	}
}

// TestActivationCommittedBetweenSnapshotAndRowLockIsNotClaimedLive proves the
// classification re-checks the generation row under its lock. The statement is
// held after its snapshot (the generation is a superseded orphan there), an
// activation commits, and the statement then reaches the row lock. Read
// Committed re-runs the quals of the locked relation on the row's newest
// version, so a row that turned active drops out: the generation must come back
// unclaimed. A statement whose locked relation lacks the status test claims the
// active row, and the batch would go on to delete under it. (PostgreSQL keeps
// the row lock it took on the newest version even when the re-check drops the
// row, so a lock probe cannot tell the two statements apart; the claimed flag is
// the contract.)
func TestActivationCommittedBetweenSnapshotAndRowLockIsNotClaimedLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-race")
	f.generation(t, "scope-race", "gen-race", "superseded")
	f.evidence(t, "gen-race", 3)
	f.candidates(t, "gen-race", 2)
	activate := f.stageActivationRace(t, "gen-race")
	staged := gatedClassify(t)

	classifier, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin the classifier: %v", err)
	}
	defer func() { _ = classifier.Rollback() }()
	type verdict struct {
		predicatesHold, claimed bool
		err                     error
	}
	got := make(chan verdict, 1)
	go func() {
		var v verdict
		var id string
		v.err = classifier.QueryRowContext(f.ctx, staged, []string{"gen-race"}).Scan(&id, &v.predicatesHold, &v.claimed)
		got <- v
	}()
	if !f.waitForLockWait(t, "%classify and lock generations%", 30*time.Second) {
		t.Fatal("the classification never reached its gate")
	}
	activate()

	var v verdict
	select {
	case v = <-got:
	case <-time.After(30 * time.Second):
		t.Fatal("the classification did not finish")
	}
	if v.err != nil {
		t.Fatalf("classify: %v", v.err)
	}
	if v.claimed {
		t.Fatalf("the generation was claimed (predicates held %v) although its row turned active before the row lock", v.predicatesHold)
	}
	if status, _, _ := f.generationStatus(t, "gen-race"); status != "active" {
		t.Fatalf("generation row status = %q, want the committed activation", status)
	}
}

// swapStatement is a db.Beginner whose transactions send replacement for every
// statement equal to original, so a whole Sweep can run the staged statement.
type swapStatement struct {
	db.Beginner
	original, replacement string
}

func (s swapStatement) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := s.Beginner.Begin(ctx)
	return swapTx{Transaction: tx, original: s.original, replacement: s.replacement}, err
}

type swapTx struct {
	db.Transaction
	original, replacement string
}

func (tx swapTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == tx.original {
		query = tx.replacement
	}
	return tx.Transaction.QueryContext(ctx, query, args...)
}

// TestPassOverAnActivationRaceNeverCountsTheGenerationSweptLive runs a whole
// pass over the same race. Whatever the classification reports, the pass must
// leave the generation for a later one: not swept, no batch committed, every row
// and the now active generation row intact.
func TestPassOverAnActivationRaceNeverCountsTheGenerationSweptLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-race")
	f.generation(t, "scope-race", "gen-race", "superseded")
	f.evidence(t, "gen-race", 3)
	f.candidates(t, "gen-race", 2)
	activate := f.stageActivationRace(t, "gen-race")
	database := swapStatement{Beginner: f.database, original: relationshipretention.ClassifySQL, replacement: gatedClassify(t)}

	done := make(chan sweepOutcome, 1)
	go func() {
		result, err := relationshipretention.Sweep(f.ctx, database, relationshipretention.Options{Deletes: true, RowCap: 1 << 40})
		done <- sweepOutcome{result, err}
	}()
	if !f.waitForLockWait(t, "%classify and lock generations%", 30*time.Second) {
		t.Fatal("the classification never reached its gate")
	}
	activate()

	var got sweepOutcome
	select {
	case got = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the sweep did not finish")
	}
	if got.err != nil {
		t.Fatalf("Sweep: %v", got.err)
	}
	if got.result.GenerationsSwept != 0 || got.result.Batches != 0 || got.result.Skipped[relationshipretention.SkipLockBusy] != 1 {
		t.Fatalf("pass = swept %d in %d batches, skipped %v; want the generation left for a later pass",
			got.result.GenerationsSwept, got.result.Batches, got.result.Skipped)
	}
	if rows := f.of(t, "gen-race"); rows != (counts{3, 2, 1, 0}) {
		t.Fatalf("rows after the pass = %+v, want every row intact {3 2 1 0}", rows)
	}
	if status, _, present := f.generationStatus(t, "gen-race"); !present || status != "active" {
		t.Fatalf("generation row = (%q, present %v), want the activation's active row", status, present)
	}
}
