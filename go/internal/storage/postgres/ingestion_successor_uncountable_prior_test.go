// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// uncountablePriorFixture stages a watermarked forced-full repository
// snapshot (#8362): the scope's active generation is a delta whose base
// full generation is missing, so the destructive-successor guard cannot
// count the prior. The snapshot carries the repository-reindex reason at a
// new commit, mirroring a forced sync that observed a moved head.
func uncountablePriorFixture() (scope.IngestionScope, scope.ScopeGeneration) {
	now := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
	return scope.IngestionScope{
			ScopeID: "scope-8362", SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: "example.com/org/repo",
		}, scope.ScopeGeneration{
			GenerationID: "generation-forced-full", ScopeID: "scope-8362",
			ObservedAt: now.Add(-time.Minute), IngestedAt: now,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
			FreshnessHint: "hint-prior", SourceCommitSHA: "commit-new",
			ReconcileReason: scope.ReconcileReasonRepositoryReindexRequested,
		}
}

// uncountablePriorDB stages the pre-write reads for a commit over an
// active delta with no base full generation: the unchanged-generation
// candidate, the delta-union target probe marking it a delta, and an
// empty base lookup, which makes CountFacts refuse with
// DeltaBaseMissingError.
func uncountablePriorDB(tx *fakeTx) *fakeTransactionalDB {
	return &fakeTransactionalDB{tx: tx, queryResponses: []queueFakeRows{
		{rows: [][]any{{"generation-active", "hint-prior", "commit-old", "active", false, false}}},
		{rows: [][]any{{true, "commit-old-baseline"}}},
		{},
	}}
}

// TestCommitStoresForcedFullOverMissingBasePrior is the #8362 regression:
// a watermarked forced-full snapshot over an active delta whose base full
// generation is missing stores (and enqueues projector work) instead of
// failing the commit in the prior-fact count, so the reindex watermark
// can converge.
func TestCommitStoresForcedFullOverMissingBasePrior(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	db := uncountablePriorDB(tx)
	scopeValue, generation := uncountablePriorFixture()

	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation,
		backstopFactStream(scopeValue.ScopeID, generation.GenerationID, 3)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("committed = %v, rolled back = %v, want a committed store", tx.committed, tx.rolledBack)
	}
	enqueued := false
	for _, exec := range tx.execs {
		if strings.Contains(exec.query, "INSERT INTO fact_work_items") {
			enqueued = true
		}
	}
	if !enqueued {
		t.Fatal("forced full enqueued projector work = false, want true: the repair must project")
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0", got)
	}
}

// TestCommitRefusesEmptySuccessorOverMissingBasePrior pins the #7926
// backstop's core under #8362: an empty successor over an uncountable
// prior is still refused, since it retracts everything regardless of the
// prior's size.
func TestCommitRefusesEmptySuccessorOverMissingBasePrior(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	db := uncountablePriorDB(tx)
	scopeValue, generation := uncountablePriorFixture()

	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation,
		testfixtures.FactChannel(nil)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.rolledBack || tx.committed {
		t.Fatalf("rolled back = %v, committed = %v, want rolled back without commit", tx.rolledBack, tx.committed)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 1 {
		t.Fatalf("destructive_successor skips = %d, want 1", got)
	}
}

// TestCommitFailsClosedOnPriorCountError proves the #8362 degrade is
// narrow: a prior-fact count failure that is not a missing delta base
// still fails the commit instead of storing over an unknown prior.
func TestCommitFailsClosedOnPriorCountError(t *testing.T) {
	tx := &fakeTx{}
	db := &fakeTransactionalDB{tx: tx, queryResponses: []queueFakeRows{
		{rows: [][]any{{"generation-active", "hint-prior", "commit-old", "active", false, false}}},
		{err: errors.New("connection reset")},
	}}
	scopeValue, generation := uncountablePriorFixture()

	err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation,
		backstopFactStream(scopeValue.ScopeID, generation.GenerationID, 3))
	if err == nil {
		t.Fatal("CommitScopeGeneration() error = nil, want prior count failure")
	}
	if !strings.Contains(err.Error(), "count prior generation facts") {
		t.Fatalf("CommitScopeGeneration() error = %v, want count prior generation facts failure", err)
	}
	if tx.committed {
		t.Fatal("transaction committed = true, want false")
	}
}
