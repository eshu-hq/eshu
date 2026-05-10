package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestIngestionStoreCommitClaimedScopeGenerationRollsBackOnFactStreamError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 10, 35, 0, 0, time.UTC)
	db := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-claimed-stream-error",
		SourceSystem:  "terraform_state",
		ScopeKind:     scope.KindStateSnapshot,
		CollectorKind: scope.CollectorTerraformState,
		PartitionKey:  "tfstate:claimed-stream-error",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-claimed-stream-error",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mutation := workflow.ClaimMutation{
		WorkItemID:    "work-item-claimed-stream-error",
		ClaimID:       "claim-claimed-stream-error",
		FencingToken:  7,
		OwnerID:       "collector-owner",
		ObservedAt:    now,
		LeaseDuration: time.Minute,
	}
	replayErr := errors.New("spool replay failed")

	err := store.CommitClaimedScopeGenerationWithStreamError(
		context.Background(),
		mutation,
		scopeValue,
		generation,
		testFactChannel([]facts.Envelope{{
			FactID:        "fact-claimed-stream-error",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "terraform_state_snapshot",
			StableFactKey: "snapshot:claimed-stream-error",
			ObservedAt:    now,
			Payload:       map[string]any{"serial": float64(1)},
		}}),
		func() error { return replayErr },
	)
	if !errors.Is(err, replayErr) {
		t.Fatalf("CommitClaimedScopeGenerationWithStreamError() error = %v, want %v", err, replayErr)
	}
	if db.tx.committed {
		t.Fatal("transaction committed = true, want false")
	}
	if !db.tx.rolledBack {
		t.Fatal("transaction rolledBack = false, want true")
	}
	if got, want := len(db.tx.execs), 4; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	for _, exec := range db.tx.execs {
		if strings.Contains(exec.query, "INSERT INTO fact_work_items") {
			t.Fatalf("projector work enqueued despite stream error: %q", exec.query)
		}
	}
}

func TestIngestionStoreUnchangedGenerationChecksFactStreamError(t *testing.T) {
	t.Parallel()

	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	now := time.Date(2026, time.May, 10, 10, 38, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-active", "fingerprint-same"}},
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-unchanged-stream-error",
		SourceSystem:  "terraform_state",
		ScopeKind:     scope.KindStateSnapshot,
		CollectorKind: scope.CollectorTerraformState,
		PartitionKey:  "tfstate:unchanged-stream-error",
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-unchanged-stream-error",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now,
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-same",
	}
	replayErr := errors.New("spool replay failed while skipping")

	err := store.CommitScopeGenerationWithStreamError(
		context.Background(),
		scopeValue,
		generation,
		testFactChannel([]facts.Envelope{{
			FactID:        "fact-unchanged-stream-error",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.TerraformStateSnapshotFactKind,
			StableFactKey: "snapshot:unchanged-stream-error",
			ObservedAt:    now,
			Payload:       map[string]any{"serial": float64(1)},
		}}),
		func() error { return replayErr },
	)
	if !errors.Is(err, replayErr) {
		t.Fatalf("CommitScopeGenerationWithStreamError() error = %v, want %v", err, replayErr)
	}
	if got, want := db.beginCalls, 0; got != want {
		t.Fatalf("begin calls = %d, want %d", got, want)
	}
	if db.tx.committed || db.tx.rolledBack {
		t.Fatalf("transaction touched despite freshness skip: committed=%v rolledBack=%v", db.tx.committed, db.tx.rolledBack)
	}
	if got, want := telemetry.SkippedRefreshCount(), uint64(0); got != want {
		t.Fatalf("SkippedRefreshCount() = %d, want %d", got, want)
	}
}

func TestIngestionStoreRollsBackBeforeDrainingAfterEarlyFactError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 10, 40, 0, 0, time.UTC)
	secondFactAccepted := make(chan struct{})
	drainedBeforeRollback := false
	db := &fakeTransactionalDB{tx: &fakeTx{
		rollbackHook: func() {
			select {
			case <-secondFactAccepted:
				drainedBeforeRollback = true
			default:
			}
		},
	}}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-early-fact-error",
		SourceSystem:  "terraform_state",
		ScopeKind:     scope.KindStateSnapshot,
		CollectorKind: scope.CollectorTerraformState,
		PartitionKey:  "tfstate:early-fact-error",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-early-fact-error",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	factStream := make(chan facts.Envelope)
	go func() {
		defer close(factStream)
		factStream <- facts.Envelope{
			FactID:        "fact-wrong-scope",
			ScopeID:       "wrong-scope",
			GenerationID:  generation.GenerationID,
			FactKind:      facts.TerraformStateResourceFactKind,
			StableFactKey: "resource:wrong-scope",
			ObservedAt:    now,
			Payload:       map[string]any{"name": "wrong"},
		}
		factStream <- facts.Envelope{
			FactID:        "fact-remaining",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      facts.TerraformStateResourceFactKind,
			StableFactKey: "resource:remaining",
			ObservedAt:    now,
			Payload:       map[string]any{"name": "remaining"},
		}
		close(secondFactAccepted)
	}()

	err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, factStream)
	if err == nil {
		t.Fatal("CommitScopeGeneration() error = nil, want fact scope validation error")
	}
	if !db.tx.rolledBack {
		t.Fatal("transaction rolledBack = false, want true")
	}
	if drainedBeforeRollback {
		t.Fatal("remaining facts were drained before transaction rollback")
	}
	select {
	case <-secondFactAccepted:
	case <-time.After(time.Second):
		t.Fatal("remaining fact was not drained after rollback")
	}
	if db.tx.committed {
		t.Fatal("transaction committed = true, want false")
	}
}
