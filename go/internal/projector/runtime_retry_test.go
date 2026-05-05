package projector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestRuntimeProjectFailsRetryablyOnceBeforeMaterializing(t *testing.T) {
	t.Parallel()

	injector, err := NewRetryOnceInjector("scope-123:generation-456")
	if err != nil {
		t.Fatalf("NewRetryOnceInjector() error = %v, want nil", err)
	}

	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{result: content.Result{RecordCount: 1}}

	runtime := Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   contentWriter,
		RetryInjector:   injector,
	}

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	inputFacts := []facts.Envelope{{
		FactID:       "fact-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "repository",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 31, 0, 0, time.UTC),
		Payload: map[string]any{
			"repo_id": "repo-123",
			"name":    "eshu",
			"path":    "org/eshu",
		},
	}}

	if _, err := runtime.Project(context.Background(), scopeValue, generationValue, inputFacts); err == nil {
		t.Fatal("Project() first call error = nil, want retryable error")
	} else if !IsRetryable(err) {
		t.Fatalf("Project() first call retryable = false, want true")
	}
	if got, want := len(canonicalWriter.calls), 0; got != want {
		t.Fatalf("canonical writes after retryable failure = %d, want %d", got, want)
	}

	if _, err := runtime.Project(context.Background(), scopeValue, generationValue, inputFacts); err != nil {
		t.Fatalf("Project() second call error = %v, want nil", err)
	}
	if got, want := len(canonicalWriter.calls), 1; got < want {
		t.Fatalf("canonical writes after recovery = %d, want >= %d", got, want)
	}
}
