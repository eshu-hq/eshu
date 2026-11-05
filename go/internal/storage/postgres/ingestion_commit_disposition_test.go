// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestCommitMarksItsDisposition is the store half of #10037: every commit path
// that returns nil says what it did with the generation on the box the caller
// put on the context. The return stays nil on every row, so no collector starts
// failing claims over a refusal.
func TestCommitMarksItsDisposition(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	unchangedGeneration := func() (scope.IngestionScope, scope.ScopeGeneration) {
		scopeValue, generation := successorBackstopFixture("generation-next", false)
		generation.FreshnessHint = "hint-prior"
		return scopeValue, generation
	}

	tests := []struct {
		name        string
		database    func() *fakeTransactionalDB
		fixture     func() (scope.IngestionScope, scope.ScopeGeneration)
		facts       func(scopeID, generationID string) <-chan facts.Envelope
		wantKind    collector.CommitDispositionKind
		wantRefusal collector.CommitRefusal
		wantStored  bool
	}{
		{
			name:     "stored",
			database: func() *fakeTransactionalDB { return successorBackstopDB(&fakeTx{}, 100) },
			facts: func(scopeID, generationID string) <-chan facts.Envelope {
				return backstopFactStream(scopeID, generationID, 50)
			},
			wantKind:   collector.CommitDispositionCommitted,
			wantStored: true,
		},
		{
			name:     "refused under half the prior",
			database: func() *fakeTransactionalDB { return successorBackstopDB(&fakeTx{}, 100) },
			facts: func(scopeID, generationID string) <-chan facts.Envelope {
				return backstopFactStream(scopeID, generationID, 10)
			},
			wantKind: collector.CommitDispositionRefused,
			wantRefusal: collector.CommitRefusal{
				Reason: telemetry.RefreshSkipReasonDestructiveSuccessor,
				Rule:   telemetry.SuccessorGuardRuleCollectorRatio,
			},
		},
		{
			name:     "refused empty over a content-bearing prior",
			database: func() *fakeTransactionalDB { return successorBackstopDB(&fakeTx{}, 7) },
			facts:    func(string, string) <-chan facts.Envelope { return testfixtures.FactChannel(nil) },
			wantKind: collector.CommitDispositionRefused,
			wantRefusal: collector.CommitRefusal{
				Reason: telemetry.RefreshSkipReasonDestructiveSuccessor,
				Rule:   telemetry.SuccessorGuardRuleEmpty,
			},
		},
		{
			name: "refused by the retirement gate",
			database: func() *fakeTransactionalDB {
				// The gate CTE returns no row: the scope write is fenced out.
				return &fakeTransactionalDB{tx: &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{}}}}}
			},
			fixture: func() (scope.IngestionScope, scope.ScopeGeneration) {
				return testRetiringScopeValue(), testRetiringScopeGeneration()
			},
			facts:       func(string, string) <-chan facts.Envelope { return testfixtures.FactChannel(nil) },
			wantKind:    collector.CommitDispositionRefused,
			wantRefusal: collector.CommitRefusal{Reason: commitRefusalRepositoryRetiring},
		},
		{
			name: "unchanged hint skips",
			database: func() *fakeTransactionalDB {
				return &fakeTransactionalDB{tx: &fakeTx{}, queryResponses: []queueFakeRows{{
					rows: [][]any{{"generation-active", "hint-prior", "", "active", false, true}},
				}}}
			},
			fixture:  unchangedGeneration,
			facts:    func(string, string) <-chan facts.Envelope { return testfixtures.FactChannel(nil) },
			wantKind: collector.CommitDispositionUnchangedSkip,
		},
		{
			name: "finalized generation replay skips",
			database: func() *fakeTransactionalDB {
				return &fakeTransactionalDB{tx: &fakeTx{zeroRowExec: upsertScopeGenerationQuery}}
			},
			fixture: func() (scope.IngestionScope, scope.ScopeGeneration) {
				return testRetiringScopeValue(), testRetiringScopeGeneration()
			},
			facts:    func(string, string) <-chan facts.Envelope { return testfixtures.FactChannel(nil) },
			wantKind: collector.CommitDispositionUnchangedSkip,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			database := tc.database()
			scopeValue, generation := successorBackstopFixture("generation-next", false)
			if tc.fixture != nil {
				scopeValue, generation = tc.fixture()
			}
			ctx, box := collector.WithCommitDisposition(context.Background())

			err := NewIngestionStore(database).CommitScopeGeneration(ctx, scopeValue, generation,
				tc.facts(scopeValue.ScopeID, generation.GenerationID))
			if err != nil {
				t.Fatalf("CommitScopeGeneration() error = %v, want nil on every disposition", err)
			}
			if got := box.Kind(); got != tc.wantKind {
				t.Fatalf("disposition = %q, want %q", got, tc.wantKind)
			}
			refusal, _ := box.Refusal()
			if refusal != tc.wantRefusal {
				t.Fatalf("refusal = %+v, want %+v", refusal, tc.wantRefusal)
			}
			if got := database.tx.committed; got != tc.wantStored {
				t.Fatalf("transaction committed = %v, want %v", got, tc.wantStored)
			}
		})
	}
}

// TestCommitWithoutABoxStillRefuses proves an unclaimed commit, whose context
// carries no box, keeps its behavior: the refusal rolls back and returns nil.
func TestCommitWithoutABoxStillRefuses(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{}
	scopeValue, generation := successorBackstopFixture("generation-next", false)
	err := NewIngestionStore(successorBackstopDB(tx, 7)).CommitScopeGeneration(context.Background(), scopeValue, generation,
		testfixtures.FactChannel(nil))
	if err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !tx.rolledBack || tx.committed {
		t.Fatalf("rolled back = %v, committed = %v, want a refused commit", tx.rolledBack, tx.committed)
	}
}
