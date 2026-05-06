package projector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestRuntimeProjectDefaultWritesCanonicalBeforeContent(t *testing.T) {
	t.Parallel()

	var order []string
	runtime := Runtime{
		CanonicalWriter: &orderedCanonicalWriter{order: &order},
		ContentWriter:   &orderedContentWriter{order: &order, result: content.Result{RecordCount: 1}},
	}

	_, err := runtime.Project(context.Background(), orderScope(), orderGeneration(), orderFacts())
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}
	assertOrder(t, order, []string{"canonical", "content"})
}

func TestRuntimeProjectContentBeforeCanonicalWritesContentFirst(t *testing.T) {
	t.Parallel()

	var order []string
	runtime := Runtime{
		CanonicalWriter:        &orderedCanonicalWriter{order: &order},
		ContentWriter:          &orderedContentWriter{order: &order, result: content.Result{RecordCount: 1}},
		ContentBeforeCanonical: true,
	}

	_, err := runtime.Project(context.Background(), orderScope(), orderGeneration(), orderFacts())
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}
	assertOrder(t, order, []string{"content", "canonical"})
}

func TestRuntimeProjectContentBeforeCanonicalKeepsContentWhenCanonicalFails(t *testing.T) {
	t.Parallel()

	var order []string
	expectedErr := errors.New("graph backend timed out")
	runtime := Runtime{
		CanonicalWriter:        &orderedCanonicalWriter{order: &order, err: expectedErr},
		ContentWriter:          &orderedContentWriter{order: &order, result: content.Result{RecordCount: 1}},
		ContentBeforeCanonical: true,
	}

	_, err := runtime.Project(context.Background(), orderScope(), orderGeneration(), orderFacts())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Project() error = %v, want %v", err, expectedErr)
	}
	assertOrder(t, order, []string{"content", "canonical"})
}

func orderScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-order",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-order",
		Metadata: map[string]string{
			"repo_id": "repository:r_order",
		},
	}
}

func orderGeneration() scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "generation-order",
		ScopeID:      "scope-order",
		ObservedAt:   time.Date(2026, time.April, 22, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 22, 10, 1, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func orderFacts() []facts.Envelope {
	observedAt := time.Date(2026, time.April, 22, 10, 0, 0, 0, time.UTC)
	return []facts.Envelope{
		{
			FactID:       "fact-repo",
			ScopeID:      "scope-order",
			GenerationID: "generation-order",
			FactKind:     "repository",
			ObservedAt:   observedAt,
			Payload: map[string]any{
				"repo_id": "repository:r_order",
				"name":    "eshu",
				"path":    "/workspace/eshu",
			},
		},
		{
			FactID:       "fact-content",
			ScopeID:      "scope-order",
			GenerationID: "generation-order",
			FactKind:     "source_content",
			ObservedAt:   observedAt,
			Payload: map[string]any{
				"content_path":   "README.md",
				"content_body":   "# Eshu",
				"content_digest": "digest-order",
			},
		},
	}
}

type orderedCanonicalWriter struct {
	order *[]string
	err   error
}

func (w *orderedCanonicalWriter) Write(_ context.Context, _ CanonicalMaterialization) error {
	*w.order = append(*w.order, "canonical")
	return w.err
}

type orderedContentWriter struct {
	order  *[]string
	result content.Result
}

func (w *orderedContentWriter) Write(_ context.Context, materialization content.Materialization) (content.Result, error) {
	*w.order = append(*w.order, "content")
	if len(materialization.Records) == 0 {
		return content.Result{}, errors.New("content materialization is empty")
	}
	return w.result, nil
}

func assertOrder(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
