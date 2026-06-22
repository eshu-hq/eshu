package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestIngestionStoreCommitScopeGenerationSkipsUnchangedActiveGeneration(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-active", "fingerprint-same"}},
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-456",
		ScopeID:       "scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-same",
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got, want := db.beginCalls, 0; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if got, want := len(db.tx.execs), 0; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := telemetry.SkippedRefreshCount(), uint64(1); got != want {
		t.Fatalf("SkippedRefreshCount() = %d, want %d", got, want)
	}
}

func TestIngestionStoreCommitScopeGenerationSkipsUnchangedPendingGeneration(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-pending", "fingerprint-same"}},
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-456",
		ScopeID:       "scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-same",
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"generation.status IN ('pending', 'active')",
		"ORDER BY generation.ingested_at DESC",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("freshness query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.beginCalls, 0; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if got, want := telemetry.SkippedRefreshCount(), uint64(1); got != want {
		t.Fatalf("SkippedRefreshCount() = %d, want %d", got, want)
	}
}

func TestIngestionStoreCommitScopeGenerationContinuesWhenActiveFingerprintDiffers(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-active", "fingerprint-old"}},
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-456",
		ScopeID:       "scope-123",
		ObservedAt:    time.Date(2026, time.April, 12, 11, 59, 0, 0, time.UTC),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-new",
	}

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, nil); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}

	// One base-connection query: the active-generation freshness check (run on
	// s.db before the tx opens). The repository catalog cold load now runs on the
	// OPEN transaction's connection (issue #3521 P1), so it lands in db.tx.queries
	// and must not add a second base-connection query.
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("base query count = %d, want %d", got, want)
	}
	if got, want := len(db.tx.queries), 1; got != want {
		t.Fatalf("transaction query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.tx.queries[0].query, "fact_kind = 'repository'") {
		t.Fatalf("transaction query = %q, want repository catalog load", db.tx.queries[0].query)
	}
	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin call count = %d, want %d", got, want)
	}
	if got, want := len(db.tx.execs), 4; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := telemetry.SkippedRefreshCount(), uint64(0); got != want {
		t.Fatalf("SkippedRefreshCount() = %d, want %d", got, want)
	}
}
