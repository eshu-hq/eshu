// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func successorBackstopFixture(generationID string, trusted bool) (scope.IngestionScope, scope.ScopeGeneration) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	return scope.IngestionScope{
			ScopeID: "scope-7926", SourceSystem: "jira", ScopeKind: scope.KindJiraSite,
			CollectorKind: scope.CollectorJira, PartitionKey: "example.atlassian.net",
		}, scope.ScopeGeneration{
			GenerationID: generationID, ScopeID: "scope-7926",
			ObservedAt: now.Add(-time.Minute), IngestedAt: now,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
			FreshnessHint: "hint-next", TrustedDeletion: trusted,
		}
}

// successorBackstopDB stages the pre-write reads for one Store-path commit:
// the unchanged-generation candidate row plus the candidate's guard counts.
func successorBackstopDB(tx *fakeTx, priorFacts int) *fakeTransactionalDB {
	return &fakeTransactionalDB{tx: tx, queryResponses: []queueFakeRows{
		{rows: [][]any{{"generation-active", "hint-prior", "", "active", false, false}}},
		// The delta-union base probe (#8189) CountFacts issues before the
		// count: not a delta here, so no union.
		{rows: [][]any{{false, ""}}},
		// Guard counts: collector rows, then all rows (#8750).
		{rows: [][]any{{priorFacts, priorFacts}}},
	}}
}

func backstopFactStream(scopeID, generationID string, count int) <-chan facts.Envelope {
	envelopes := make([]facts.Envelope, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("fact-%d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactID: id, ScopeID: scopeID, GenerationID: generationID,
			FactKind: "work_item_record", StableFactKey: "jira:issue:" + id,
			ObservedAt: time.Date(2026, time.October, 11, 11, 59, 0, 0, time.UTC),
		})
	}
	return testfixtures.FactChannel(envelopes)
}

// TestCommitRefusesEmptySuccessorOverContentBearingPrior is the #7926
// backstop regression: an empty successor over a prior generation that holds
// facts stores nothing and commits nothing, so the prior generation stays
// active. The refusal is recorded under the destructive-successor reason and
// the commit reports success, leaving the next scheduled tick to recollect.
func TestCommitRefusesEmptySuccessorOverContentBearingPrior(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	db := successorBackstopDB(tx, 7)
	scopeValue, generation := successorBackstopFixture("generation-next", false)
	stream := testfixtures.FactChannel(nil)

	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.rolledBack {
		t.Fatal("transaction rolled back = false, want true")
	}
	if tx.committed {
		t.Fatal("transaction committed = true, want false: the empty successor must not land")
	}
	for _, exec := range tx.execs {
		if strings.Contains(exec.query, "INSERT INTO fact_work_items") {
			t.Fatalf("refused successor enqueued projector work:\n%s", exec.query)
		}
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 1 {
		t.Fatalf("destructive_successor skips = %d, want 1", got)
	}
	if got := len(stream); got != 0 {
		t.Fatalf("fact stream has %d undrained facts, want 0", got)
	}
}

// TestCommitRefusesSharplyShrunkenSuccessor proves the ratio half of the
// backstop: a successor holding less than half the prior's facts is refused
// like an empty one.
func TestCommitRefusesSharplyShrunkenSuccessor(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	db := successorBackstopDB(tx, 100)
	scopeValue, generation := successorBackstopFixture("generation-next", false)

	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation,
		backstopFactStream(scopeValue.ScopeID, generation.GenerationID, 49)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.rolledBack || tx.committed {
		t.Fatalf("rolled back = %v, committed = %v, want rolled back without commit", tx.rolledBack, tx.committed)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 1 {
		t.Fatalf("destructive_successor skips = %d, want 1", got)
	}
}

// TestCommitStoresTrustedEmptySuccessor proves the escape hatch: a generation
// carrying the trusted-deletion signal stores even when it is empty, so a
// legitimate mass-resolve lands.
func TestCommitStoresTrustedEmptySuccessor(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	db := &fakeTransactionalDB{tx: tx, queryResponses: []queueFakeRows{
		{rows: [][]any{{"generation-active", "hint-prior", "", "active", false, false}}},
	}}
	scopeValue, generation := successorBackstopFixture("generation-next", true)

	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation,
		testfixtures.FactChannel(nil)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("committed = %v, rolled back = %v, want a committed store", tx.committed, tx.rolledBack)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0", got)
	}
}

// TestCommitStoresEmptySuccessorOverEmptyPrior proves a first write and an
// empty-over-empty successor are not refused: the backstop protects
// content-bearing priors, it does not wedge legitimately empty scopes.
func TestCommitStoresEmptySuccessorOverEmptyPrior(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	db := successorBackstopDB(tx, 0)
	scopeValue, generation := successorBackstopFixture("generation-next", false)

	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation,
		testfixtures.FactChannel(nil)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("committed = %v, rolled back = %v, want a committed store", tx.committed, tx.rolledBack)
	}
}

// TestCommitStoresMildlyShrunkenSuccessor proves ordinary drift still lands:
// a successor holding half or more of the prior's facts is not a sharp
// shrink.
func TestCommitStoresMildlyShrunkenSuccessor(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	db := successorBackstopDB(tx, 100)
	scopeValue, generation := successorBackstopFixture("generation-next", false)

	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation,
		backstopFactStream(scopeValue.ScopeID, generation.GenerationID, 50)); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("committed = %v, rolled back = %v, want a committed store", tx.committed, tx.rolledBack)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonDestructiveSuccessor); got != 0 {
		t.Fatalf("destructive_successor skips = %d, want 0", got)
	}
}
