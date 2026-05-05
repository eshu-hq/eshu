package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestProofDomainReplayRetryReplacesStaleProjectionState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 14, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repo_id": "repo-123",
		},
	}
	generationA := scope.ScopeGeneration{
		GenerationID:  "generation-aaa",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-2 * time.Minute),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-aaa",
	}
	generationB := scope.ScopeGeneration{
		GenerationID:  "generation-bbb",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-time.Minute),
		IngestedAt:    now.Add(time.Minute),
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-bbb",
	}

	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{}

	if err := store.CommitScopeGeneration(
		context.Background(),
		scopeValue,
		generationA,
		testFactChannel(proofReplayFacts(scopeValue.ScopeID, generationA.GenerationID, "fact-1", "initial body", generationA.ObservedAt)),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() generation A error = %v, want nil", err)
	}
	runProofProjectorCycleWithWriters(t, db, now, nil, canonicalWriter, contentWriter)

	if got, want := db.activeGenerationID(scopeValue.ScopeID), generationA.GenerationID; got != want {
		t.Fatalf("active generation after generation A = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generationA.GenerationID), scope.GenerationStatusActive; got != want {
		t.Fatalf("generation A status after generation A = %q, want %q", got, want)
	}
	if got, want := len(contentWriter.calls), 1; got != want {
		t.Fatalf("content materialization count after generation A = %d, want %d", got, want)
	}
	if got, want := contentWriter.calls[0].ScopeGenerationKey(), "scope-123:generation-aaa"; got != want {
		t.Fatalf("generation A content scope-generation key = %q, want %q", got, want)
	}
	if got, want := contentWriter.calls[0].Records[0].Body, "initial body"; got != want {
		t.Fatalf("generation A content body = %q, want %q", got, want)
	}

	if err := store.CommitScopeGeneration(
		context.Background(),
		scopeValue,
		generationB,
		testFactChannel(proofReplayFacts(scopeValue.ScopeID, generationB.GenerationID, "fact-2", "changed body", generationB.ObservedAt)),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() generation B error = %v, want nil", err)
	}

	injector, err := projector.NewRetryOnceInjector(scopeValue.ScopeID + ":" + generationB.GenerationID)
	if err != nil {
		t.Fatalf("NewRetryOnceInjector() error = %v, want nil", err)
	}
	runProofProjectorCycleWithWriters(t, db, now.Add(2*time.Second), injector, canonicalWriter, contentWriter)

	if got, want := db.activeGenerationID(scopeValue.ScopeID), generationA.GenerationID; got != want {
		t.Fatalf("active generation after injected retry = %q, want %q", got, want)
	}
	if got, want := db.projectorWorkItemStatus(generationB.GenerationID), "retrying"; got != want {
		t.Fatalf("generation B projector status after injected retry = %q, want %q", got, want)
	}

	runProofProjectorCycleWithWriters(
		t,
		db,
		now.Add(4*time.Second),
		nil,
		canonicalWriter,
		contentWriter,
	)

	if got, want := db.activeGenerationID(scopeValue.ScopeID), generationB.GenerationID; got != want {
		t.Fatalf("active generation after replay/retry = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generationA.GenerationID), scope.GenerationStatusSuperseded; got != want {
		t.Fatalf("generation A status after replay/retry = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generationB.GenerationID), scope.GenerationStatusActive; got != want {
		t.Fatalf("generation B status after replay/retry = %q, want %q", got, want)
	}
	if got, want := db.projectorWorkItemStatus(generationA.GenerationID), "succeeded"; got != want {
		t.Fatalf("generation A projector status after replay/retry = %q, want %q", got, want)
	}
	if got, want := db.projectorWorkItemStatus(generationB.GenerationID), "succeeded"; got != want {
		t.Fatalf("generation B projector status after replay/retry = %q, want %q", got, want)
	}
	if got, want := len(contentWriter.calls), 2; got != want {
		t.Fatalf("content materialization count after replay/retry = %d, want %d", got, want)
	}
	if got, want := contentWriter.calls[1].ScopeGenerationKey(), "scope-123:generation-bbb"; got != want {
		t.Fatalf("generation B content scope-generation key = %q, want %q", got, want)
	}
	if got, want := contentWriter.calls[1].Records[0].Body, "changed body"; got != want {
		t.Fatalf("generation B content body = %q, want %q", got, want)
	}
}

func proofReplayFacts(
	scopeID string,
	generationID string,
	factID string,
	body string,
	observedAt time.Time,
) []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:        factID,
			ScopeID:       scopeID,
			GenerationID:  generationID,
			FactKind:      "repository",
			StableFactKey: "repository:" + factID,
			ObservedAt:    observedAt,
			Payload: map[string]any{
				"graph_id":       "repo-123",
				"graph_kind":     "repository",
				"name":           "eshu",
				"content_path":   "README.md",
				"content_body":   body,
				"content_digest": body,
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      factID,
			},
		},
	}
}
