// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

// This file holds the root-side seam tests for the #7794 backfill extraction:
// the commit-path lock-split guard and the end-to-end engine pass driven
// through IngestionStore, asserting on the fake's recorded SQL. The engine's
// fake-driven unit tests live in the backfill package.
// backfillTxDB adapts a single fakeExecQueryer into a transactional store so the
// batched deferred backfill (which opens a transaction per repository batch) can
// run against one ordered exec/query log. Begin returns a transaction that
// delegates to the same inner queryer; Commit/Rollback are no-ops. This keeps the
// backfill tests asserting on one execs slice while exercising the per-batch
// transaction path.
type backfillTxDB struct {
	inner      *fakeExecQueryer
	beginCalls int
}

func newBackfillTxDB(inner *fakeExecQueryer) *backfillTxDB {
	return &backfillTxDB{inner: inner}
}

func (database *backfillTxDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return database.inner.ExecContext(ctx, query, args...)
}

func (database *backfillTxDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return database.inner.QueryContext(ctx, query, args...)
}

func (database *backfillTxDB) Begin(context.Context) (db.Transaction, error) {
	database.beginCalls++
	return &backfillTx{inner: database.inner}, nil
}

type backfillTx struct {
	inner *fakeExecQueryer
}

func (tx *backfillTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.inner.ExecContext(ctx, query, args...)
}

func (tx *backfillTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return tx.inner.QueryContext(ctx, query, args...)
}

func (tx *backfillTx) Commit() error   { return nil }
func (tx *backfillTx) Rollback() error { return nil }

// TestIngestionStoreCommitScopeGenerationExcludesBackfillFromMainTransaction is
// the issue #4451 (§ T8) lock-split regression guard: the atomic
// scope/generation/fact commit transaction must never issue the relationship
// backfill's catalog-reload or evidence queries, even when the commit
// onboards a brand-new repository. Those queries now run only in the separate
// post-commit transaction opened by runPostCommitRelationshipBackfill, AFTER
// this transaction has already committed and released the deferred-
// maintenance shared advisory barrier, which is the change that shrinks the
// barrier's hold time to the atomic write alone.
func TestIngestionStoreCommitScopeGenerationExcludesBackfillFromMainTransaction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	commitTx := &fakeTx{}
	backfillTx := &fakeTx{}
	database := &fakeTransactionalDB{tx: commitTx, txs: []*fakeTx{commitTx, backfillTx}}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	// repo-123 is brand-new relative to the empty catalog cache, so this
	// commit is exactly the onboarding case that used to run the backfill
	// in-transaction.
	envelopes := []facts.Envelope{{
		FactID:        "fact-1",
		ScopeID:       "scope-123",
		GenerationID:  "generation-456",
		FactKind:      "repository",
		StableFactKey: "repository:repo-123",
		ObservedAt:    generation.ObservedAt,
		Payload:       map[string]any{"graph_id": "repo-123"},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			FactKey:      "fact-key",
		},
	}}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, testfixtures.FactChannel(envelopes)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	// Issue #3481/#3521: the repository catalog loads through the shared
	// cache, and the cold load runs on the OPEN ingestion transaction's
	// connection (so a single-connection pool cannot deadlock). Issue #4451
	// removes the in-TX backfill call, so the main commit transaction's only
	// reads are that catalog load and the fact_records upsert's RETURNING
	// fact_id query (issue #4444 review, codex P1); it must never see a
	// second repository-catalog reload or an evidence query, even though
	// repo-123 is new.
	if got, want := len(commitTx.queries), 2; got != want {
		t.Fatalf("main commit transaction query count = %d, want %d: %#v", got, want, commitTx.queries)
	}
	if !strings.Contains(commitTx.queries[0].query, "fact_kind = 'repository'") {
		t.Fatalf("transaction query[0] = %q, want repository catalog load first", commitTx.queries[0].query)
	}
	if !strings.Contains(commitTx.queries[1].query, "INSERT INTO fact_records") {
		t.Fatalf("transaction query[1] = %q, want fact_records upsert", commitTx.queries[1].query)
	}
	if got, want := len(database.queries), 0; got != want {
		t.Fatalf("base connection query count = %d, want %d (catalog must not use a second connection)", got, want)
	}

	// The post-commit backfill transaction runs separately, after commitTx
	// commits, and takes the shared barrier for its own bounded work.
	if !commitTx.committed {
		t.Fatal("main commit transaction committed = false, want true")
	}
	if !backfillTx.committed {
		t.Fatal("post-commit backfill transaction committed = false, want true")
	}
	foundBackfillLock := false
	for _, exec := range backfillTx.execs {
		if strings.Contains(exec.query, "pg_advisory_xact_lock_shared") {
			foundBackfillLock = true
			break
		}
	}
	if !foundBackfillLock {
		t.Fatalf("post-commit backfill transaction execs = %#v, want the shared advisory barrier re-acquired", backfillTx.execs)
	}
}

func TestIngestionStoreBackfillAllRelationshipEvidenceSkipsUnknownTargetGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	otherGen := [][]any{
		{"repo-other", "scope-other", "gen-other"},
	}
	otherPartitions := [][]any{
		{"scope-other", "gen-other"},
	}
	inner := &fakeExecQueryer{
		// Per-scope deferred fact load (issue #3710): the source fact lives in
		// scope-infra/gen-infra but references repo-app. Its target generation is
		// unknown to the write path (active generations only know repo-other), so no
		// evidence is persisted but readiness is still published.
		deferredFactsByScope: map[string][][]any{
			"scope-other": {
				{
					"fact-1",
					"scope-infra",
					"gen-infra",
					"content",
					"content:1",
					"content.v1",
					"git",
					int64(0),
					"unknown",
					"git",
					"source-fact-1",
					"",
					"",
					now,
					false,
					[]byte(`{"artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`),
				},
			},
		},
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`), catalogFakeObservedAt},
				},
			},
			// scope-generation partition snapshot (fact-load partitioning, #3710)
			{rows: otherPartitions},
			// active repository generations snapshot (write phase)
			{rows: otherGen},
			// batch transaction re-load of active generations under the lock
			{rows: otherGen},
		},
	}
	database := newBackfillTxDB(inner)
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			t.Fatalf("unexpected evidence insert for unknown target generation:\n%s", execCall.query)
		}
	}
	foundPhasePublish := false
	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO graph_projection_phase_state") {
			foundPhasePublish = true
			break
		}
	}
	if !foundPhasePublish {
		t.Fatal("expected backward evidence readiness publish")
	}
}

func TestIngestionStoreBackfillAllRelationshipEvidencePersistsBySourceGeneration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	activeGens := [][]any{
		{"repo-infra", "scope-infra", "gen-infra"},
		{"repo-app", "scope-app", "gen-app"},
	}
	scopeGenPartitions := [][]any{
		{"scope-infra", "gen-infra"},
		{"scope-app", "gen-app"},
	}
	inner := &fakeExecQueryer{
		// Per-scope deferred fact load (issue #3710): the infra source fact lives in
		// scope-infra/gen-infra and references repo-app, so evidence attaches to the
		// infra source generation.
		deferredFactsByScope: map[string][][]any{
			"scope-infra": {
				{
					"fact-1",
					"scope-infra",
					"gen-infra",
					"content",
					"content:1",
					"content.v1",
					"git",
					int64(0),
					"unknown",
					"git",
					"source-fact-1",
					"",
					"",
					now,
					false,
					[]byte(`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`),
				},
			},
		},
		queryResponses: []queueFakeRows{
			// catalog
			{
				rows: [][]any{
					{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`), catalogFakeObservedAt},
					{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`), catalogFakeObservedAt},
				},
			},
			// scope-generation partition snapshot (fact-load partitioning, #3710)
			{rows: scopeGenPartitions},
			// active repository generations snapshot (write phase)
			{rows: activeGens},
			// batch transaction re-load of active generations under the lock
			{rows: activeGens},
		},
	}
	database := newBackfillTxDB(inner)
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }

	if err := store.BackfillAllRelationshipEvidence(context.Background(), nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	var evidenceInserts []fakeExecCall
	for _, execCall := range inner.execs {
		if strings.Contains(execCall.query, "INSERT INTO relationship_evidence_facts") {
			evidenceInserts = append(evidenceInserts, execCall)
		}
	}
	if len(evidenceInserts) != 1 {
		t.Fatalf("relationship evidence inserts = %d, want 1", len(evidenceInserts))
	}
	if got, want := evidenceInserts[0].args[1], "gen-infra"; got != want {
		t.Fatalf("evidence generation_id = %v, want source generation %q", got, want)
	}
}
