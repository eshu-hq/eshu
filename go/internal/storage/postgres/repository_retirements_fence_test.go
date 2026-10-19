// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// testRetiringScopeValue is the minimal repository scope the gate tests
// commit: its partition key is the tombstone repo_id.
func testRetiringScopeValue() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "fence-scope-main",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "PARTITION:fence-repo",
		Metadata:      map[string]string{"repo_id": "PARTITION:fence-repo"},
	}
}

// testRetiringScopeGeneration is the minimal pending generation the gate
// tests commit alongside the scope.
func testRetiringScopeGeneration() scope.ScopeGeneration {
	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	return scope.ScopeGeneration{
		GenerationID: "fence-gen-1",
		ScopeID:      "fence-scope-main",
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

// fenceTestRequest is one repository's operator retirement fence input.
func fenceTestRequest() FenceRequest {
	return FenceRequest{
		Retirements: []FenceRetirement{
			{RetirementID: "rr_fence01", RepoID: "PARTITION:fence-repo", ScopeID: "fence-scope-main"},
		},
		ReasonCode:         RetirementReasonOperatorRetired,
		ReasonHash:         "reason-hash",
		ActorClass:         "operator",
		ActorIDHash:        "",
		IdempotencyKeyHash: "key-hash-1",
	}
}

// stageFenceScriptedDB answers the fencer's pre-transaction reads (scope
// resolution, precheck counts, two drain checks) and every in-transaction
// fence statement with empty results, so the shape test below exercises the
// full step order without a database.
func stageFenceScriptedDB() (*fakeTransactionalDB, *fakeTx) {
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			// Scope row locks (authoritative scope set under the key).
			{rows: [][]any{{"fence-scope-main"}, {"fence-scope-ref"}}},
			// Step-4 generation pairs (materialized for the 7c binding).
			{rows: [][]any{{"fence-scope-main", "fence-gen-1"}, {"fence-scope-ref", "fence-gen-2"}}},
			// Step-4 reducer-row budget recheck.
			{rows: [][]any{{int64(1)}}},
			// Step-6 live-lease recheck over both stages (no live lease).
			{rows: [][]any{}},
			// 7g marker insert.
			{rows: [][]any{{"rr_fence01", "PARTITION:fence-repo", "fence-scope-main", "pending", "fenced"}}},
			// Step-8 fence assertion (zero live generations, zero
			// markable reducer rows).
			{rows: [][]any{{int64(0)}}},
			{rows: [][]any{{int64(0)}}},
		},
	}
	database := &fakeTransactionalDB{
		tx: tx,
		queryResponses: []queueFakeRows{
			// Scope resolution (scope_id, partition_key).
			{rows: [][]any{{"fence-scope-main", "PARTITION:fence-repo"}, {"fence-scope-ref", "PARTITION:fence-repo"}}},
			// Precheck per-repository counts: 2 generations, 1 reducer row.
			{rows: [][]any{{"PARTITION:fence-repo", int64(2)}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(1)}}},
			// Drain checks (no live lease in either stage).
			{rows: [][]any{}},
			{rows: [][]any{}},
		},
	}
	return database, tx
}

// TestFenceRepositoryRunsDesignedStepOrder proves the phase-1 fence issues
// the amended-design sequence: precheck before any lock, the advisory key in
// sorted order, scope rows before work rows, the 1s LOCK TABLE with a live
// lease recheck under EXCLUSIVE, tuned mark bindings, the marker insert, and
// the fence assertion before commit. No DETACH DELETE and no worker
// serialization may appear anywhere in the sequence.
func TestFenceRepositoryRunsDesignedStepOrder(t *testing.T) {
	t.Parallel()

	database, tx := stageFenceScriptedDB()
	fencer := NewRetirementFencer(database, database)
	result, err := fencer.FenceRepository(context.Background(), fenceTestRequest())
	if err != nil {
		t.Fatalf("FenceRepository: %v", err)
	}
	if len(result.RetirementIDs) != 1 || result.RetirementIDs[0] != "rr_fence01" {
		t.Fatalf("retirement ids = %v, want [rr_fence01]", result.RetirementIDs)
	}

	var stmts []string
	for _, q := range tx.execs {
		stmts = append(stmts, q.query)
	}
	for _, q := range tx.queries {
		stmts = append(stmts, q.query)
	}
	joined := strings.Join(stmts, "\n")

	for _, want := range []string{
		"pg_advisory_xact_lock",
		"FOR NO KEY UPDATE",
		"LOCK TABLE fact_work_items IN EXCLUSIVE MODE",
		"set_config('lock_timeout', '1s'",
		"SET status = 'superseded'",
		"failure_class = 'repository_retired'",
		"DELETE FROM repository_reindex_requests",
		"INSERT INTO repository_retirements",
		"active_generation_id = NULL",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("fence sequence missing %q:\n%s", want, joined)
		}
	}
	for _, banned := range []string{
		"DETACH DELETE",
		"DELETE FROM fact_work_items",
		"DELETE FROM scope_generations",
	} {
		if strings.Contains(joined, banned) {
			t.Fatalf("fence sequence must never contain %q:\n%s", banned, joined)
		}
	}
	if !tx.committed {
		t.Fatal("fence transaction committed = false, want true")
	}

	// The scope ANY binding is the tuned form: no per-generation pair list
	// on the reducer mark, and the request carries at most one page of ids.
	var reducerMark string
	for _, q := range tx.execs {
		if strings.Contains(q.query, "stage = 'reducer'") && strings.Contains(q.query, "SET status = 'superseded'") {
			reducerMark = q.query
		}
	}
	if reducerMark == "" {
		t.Fatal("fence sequence has no reducer mark statement")
	}
	if !strings.Contains(reducerMark, "scope_id = ANY") {
		t.Fatalf("reducer mark does not bind scope_id = ANY:\n%s", reducerMark)
	}
}

// TestFenceRepositoryRefusesOverBudgetBeforeLocking proves the measured
// request budget: a precheck over either candidate ceiling refuses with
// request_too_large and the per-repository counts, before Begin, so no
// advisory key, table lock, or marker is ever taken for an oversized
// request.
func TestFenceRepositoryRefusesOverBudgetBeforeLocking(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	database := &fakeTransactionalDB{
		tx: tx,
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"fence-scope-main", "PARTITION:fence-repo"}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(6000)}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(3)}}},
		},
	}
	fencer := NewRetirementFencer(database, database)
	_, err := fencer.FenceRepository(context.Background(), fenceTestRequest())
	if err == nil {
		t.Fatal("over-budget fence succeeded, want request_too_large refusal")
	}
	var blocked *FenceBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error type = %T, want *FenceBlockedError", err)
	}
	if blocked.Reason != FenceReasonRequestTooLarge {
		t.Fatalf("reason = %q, want %q", blocked.Reason, FenceReasonRequestTooLarge)
	}
	if blocked.Generations != 6000 || blocked.ReducerRows != 3 {
		t.Fatalf("sums = %d/%d, want 6000/3", blocked.Generations, blocked.ReducerRows)
	}
	if budget := blocked.PerRepo["PARTITION:fence-repo"]; budget.Generations != 6000 || budget.ReducerRows != 3 {
		t.Fatalf("per-repo = %+v, want 6000/3", budget)
	}
	if database.beginCalls != 0 {
		t.Fatalf("begin calls = %d, want 0 (refused before any lock)", database.beginCalls)
	}
	if len(tx.execs) != 0 || len(tx.queries) != 0 {
		t.Fatal("oversized request issued fence statements, want none")
	}
}

// TestFenceRepositoryRefusesOverBudgetUnderLockWithPerRepo proves the
// step-4 recheck refusal carries the per-repository breakdown: a pair set
// grown past the ceiling between precheck and lock refuses with
// request_too_large, and Generations, ReducerRows, and PerRepo describe
// the locked scopes (#8610 review P2).
func TestFenceRepositoryRefusesOverBudgetUnderLockWithPerRepo(t *testing.T) {
	t.Parallel()

	over := int(RetirementFenceMaxGenerations) + 1
	pairs := make([][]any, 0, over)
	for i := 0; i < over; i++ {
		pairs = append(pairs, []any{"fence-scope-main", "fence-gen-overflow"})
	}
	tx := &fakeTx{}
	database := &fakeTransactionalDB{
		tx: tx,
		queryResponses: []queueFakeRows{
			// Precheck under budget: resolve, 2 generations, 1 reducer row, drains.
			{rows: [][]any{{"fence-scope-main", "PARTITION:fence-repo"}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(2)}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(1)}}},
			{rows: [][]any{}},
			{rows: [][]any{}},
		},
	}
	tx.queryResponses = []queueFakeRows{
		// Scope row lock, then the grown pair set.
		{rows: [][]any{{"fence-scope-main"}}},
		{rows: pairs},
		// Under-lock budget recount, grouped by repository.
		{rows: [][]any{{"PARTITION:fence-repo", int64(over)}}},
		{rows: [][]any{{"PARTITION:fence-repo", int64(7)}}},
	}
	fencer := NewRetirementFencer(database, database)
	_, err := fencer.FenceRepository(context.Background(), fenceTestRequest())
	if err == nil {
		t.Fatal("over-budget fence succeeded, want request_too_large refusal")
	}
	var blocked *FenceBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error type = %T, want *FenceBlockedError", err)
	}
	if blocked.Reason != FenceReasonRequestTooLarge {
		t.Fatalf("reason = %q, want %q", blocked.Reason, FenceReasonRequestTooLarge)
	}
	if blocked.Generations != int64(over) || blocked.ReducerRows != 7 {
		t.Fatalf("sums = %d/%d, want %d/7", blocked.Generations, blocked.ReducerRows, over)
	}
	budget, ok := blocked.PerRepo["PARTITION:fence-repo"]
	if !ok {
		t.Fatalf("per-repo missing PARTITION:fence-repo: %+v", blocked.PerRepo)
	}
	if budget.Generations != int64(over) || budget.ReducerRows != 7 {
		t.Fatalf("per-repo = %+v, want %d/7", budget, over)
	}
}

// TestFenceRepositoryRefusesReducerOverBudgetUnderLockWithPerRepo proves
// the reducer-row recheck refusal carries the same breakdown: the
// single-count recheck trips first, and the recount fills Generations,
// ReducerRows, and PerRepo over the locked scopes (#8610 review P2).
func TestFenceRepositoryRefusesReducerOverBudgetUnderLockWithPerRepo(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	database := &fakeTransactionalDB{
		tx: tx,
		queryResponses: []queueFakeRows{
			// Precheck under budget: resolve, 2 generations, 1 reducer row, drains.
			{rows: [][]any{{"fence-scope-main", "PARTITION:fence-repo"}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(2)}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(1)}}},
			{rows: [][]any{}},
			{rows: [][]any{}},
		},
	}
	tx.queryResponses = []queueFakeRows{
		// Scope row lock, small pair set, then the tripped single count.
		{rows: [][]any{{"fence-scope-main"}}},
		{rows: [][]any{{"fence-scope-main", "fence-gen-1"}, {"fence-scope-main", "fence-gen-2"}}},
		{rows: [][]any{{int64(RetirementFenceMaxReducerRows + 1)}}},
		// Under-lock budget recount, grouped by repository.
		{rows: [][]any{{"PARTITION:fence-repo", int64(2)}}},
		{rows: [][]any{{"PARTITION:fence-repo", int64(RetirementFenceMaxReducerRows + 1)}}},
	}
	fencer := NewRetirementFencer(database, database)
	_, err := fencer.FenceRepository(context.Background(), fenceTestRequest())
	if err == nil {
		t.Fatal("over-budget fence succeeded, want request_too_large refusal")
	}
	var blocked *FenceBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("error type = %T, want *FenceBlockedError", err)
	}
	if blocked.Reason != FenceReasonRequestTooLarge {
		t.Fatalf("reason = %q, want %q", blocked.Reason, FenceReasonRequestTooLarge)
	}
	if blocked.Generations != 2 || blocked.ReducerRows != RetirementFenceMaxReducerRows+1 {
		t.Fatalf("sums = %d/%d, want 2/%d", blocked.Generations, blocked.ReducerRows, RetirementFenceMaxReducerRows+1)
	}
	budget, ok := blocked.PerRepo["PARTITION:fence-repo"]
	if !ok {
		t.Fatalf("per-repo missing PARTITION:fence-repo: %+v", blocked.PerRepo)
	}
	if budget.Generations != 2 || budget.ReducerRows != RetirementFenceMaxReducerRows+1 {
		t.Fatalf("per-repo = %+v, want 2/%d", budget, RetirementFenceMaxReducerRows+1)
	}
}

// TestFenceRepositoryRetriesLockTimeoutAtStepOne proves the 1s LOCK TABLE
// wait plus retry: a 55P03 on the fence section rolls back and restarts at
// step 1, and the retry then commits.
func TestFenceRepositoryRetriesLockTimeoutAtStepOne(t *testing.T) {
	t.Parallel()

	_, retryTx := stageFenceScriptedDB()
	firstTx := &fakeTx{
		execErrors: map[int]error{},
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"fence-scope-main"}, {"fence-scope-ref"}}},
			{rows: [][]any{{"fence-scope-main", "fence-gen-1"}}},
			{rows: [][]any{{int64(0)}}},
		},
	}
	lockErr := &pgconn.PgError{Code: "55P03", Message: "lock timeout"}
	// Fail the LOCK TABLE exec: execs are advisory, advisory, set, LOCK...
	firstTx.execErrors[3] = lockErr
	database := &fakeTransactionalDB{
		txs: []*fakeTx{firstTx, retryTx},
		queryResponses: []queueFakeRows{
			// First attempt: resolve, generation counts, reducer counts,
			// projector drain, reducer drain.
			{rows: [][]any{{"fence-scope-main", "PARTITION:fence-repo"}, {"fence-scope-ref", "PARTITION:fence-repo"}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(2)}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(1)}}},
			{rows: [][]any{}},
			{rows: [][]any{}},
			// Restarted attempt re-runs steps 1 and 2 from scratch.
			{rows: [][]any{{"fence-scope-main", "PARTITION:fence-repo"}, {"fence-scope-ref", "PARTITION:fence-repo"}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(2)}}},
			{rows: [][]any{{"PARTITION:fence-repo", int64(1)}}},
			{rows: [][]any{}},
			{rows: [][]any{}},
		},
	}
	fencer := NewRetirementFencer(database, database)
	fencer.Options.Sleep = func(time.Duration) {}
	if _, err := fencer.FenceRepository(context.Background(), fenceTestRequest()); err != nil {
		t.Fatalf("FenceRepository after lock timeout: %v", err)
	}
	if !firstTx.rolledBack {
		t.Fatal("first attempt rolledBack = false, want true after 55P03")
	}
	if !retryTx.committed {
		t.Fatal("retry attempt committed = false, want true")
	}
	if got, want := database.beginCalls, 2; got != want {
		t.Fatalf("begin calls = %d, want %d (restart at step 1)", got, want)
	}
}

// TestUpsertIngestionScopeGateRefusesRetiring proves the folded marker gate:
// an open non-complete tombstone writes no scope row and reports the
// repository_retiring refusal on the finalized-skip path.
func TestUpsertIngestionScopeGateRefusesRetiring(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			// Gate CTE returns no row: the scope write is fenced out.
			{rows: [][]any{}},
		},
	}
	scopeValue := testRetiringScopeValue()
	generation := testRetiringScopeGeneration()
	outcome, _, err := upsertIngestionScope(context.Background(), tx, scopeValue, generation)
	if err != nil {
		t.Fatalf("upsertIngestionScope: %v", err)
	}
	if outcome != scopeGateRefusedRetiring {
		t.Fatalf("outcome = %v, want scopeGateRefusedRetiring", outcome)
	}
	if len(tx.execs) != 0 {
		t.Fatalf("execs = %d, want 0 (the gate folds into the scope upsert query)", len(tx.execs))
	}
	if len(tx.queries) != 1 {
		t.Fatalf("queries = %d, want exactly the gated upsert", len(tx.queries))
	}
	gated := tx.queries[0].query
	for _, want := range []string{
		"WITH gate AS",
		"FROM repository_retirements",
		"readmitted_at IS NULL",
		"WHERE NOT EXISTS (SELECT 1 FROM gate WHERE state <> 'complete')",
		"ON CONFLICT (scope_id) DO UPDATE SET",
		"RETURNING",
	} {
		if !strings.Contains(gated, want) {
			t.Fatalf("gated upsert missing %q:\n%s", want, gated)
		}
	}
}

// TestUpsertIngestionScopeGateAdmitsWithoutTombstone proves the gate is
// admission-neutral when no tombstone exists: the scope row writes and the
// outcome admits, so every existing commit path behaves exactly as before.
func TestUpsertIngestionScopeGateAdmitsWithoutTombstone(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{sql.NullString{}, sql.NullString{}}}},
		},
	}
	outcome, _, err := upsertIngestionScope(context.Background(), tx, testRetiringScopeValue(), testRetiringScopeGeneration())
	if err != nil {
		t.Fatalf("upsertIngestionScope: %v", err)
	}
	if outcome != scopeGateAdmitted {
		t.Fatalf("outcome = %v, want scopeGateAdmitted", outcome)
	}
}

// TestUpsertIngestionScopeGateReadmitsComplete proves re-admission: a
// complete tombstone admits the commit and reports readmission, so the
// caller stamps readmitted_at and the repository returns as a fresh first
// generation.
func TestUpsertIngestionScopeGateReadmitsComplete(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{sql.NullString{String: "complete", Valid: true}, sql.NullString{String: "rr_done01", Valid: true}}}},
		},
	}
	outcome, retirementID, err := upsertIngestionScope(context.Background(), tx, testRetiringScopeValue(), testRetiringScopeGeneration())
	if err != nil {
		t.Fatalf("upsertIngestionScope: %v", err)
	}
	if outcome != scopeGateReadmitted {
		t.Fatalf("outcome = %v, want scopeGateReadmitted", outcome)
	}
	if retirementID != "rr_done01" {
		t.Fatalf("retirement id = %q, want rr_done01", retirementID)
	}
}
