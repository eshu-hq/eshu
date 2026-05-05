package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildSemanticEntityReducerIntentSkipsPlainGoFunction(t *testing.T) {
	t.Parallel()

	_, ok := buildSemanticEntityReducerIntent(facts.Envelope{
		FactID:       "fact-go-plain",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "content_entity",
		Payload: map[string]any{
			"entity_type":   "Function",
			"entity_id":     "function-go-plain",
			"entity_name":   "plain",
			"relative_path": "go/plain.go",
			"repo_id":       "repo-1",
			"language":      "go",
		},
	})
	if ok {
		t.Fatal("buildSemanticEntityReducerIntent() ok = true, want false for plain Go function")
	}
}

func TestBuildSemanticEntityReducerIntentQueuesEnrichedGoFunction(t *testing.T) {
	t.Parallel()

	intent, ok := buildSemanticEntityReducerIntent(facts.Envelope{
		FactID:       "fact-go-method",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "content_entity",
		Payload: map[string]any{
			"entity_type":   "Function",
			"entity_id":     "function-go-method",
			"entity_name":   "ServeHTTP",
			"relative_path": "go/handler.go",
			"repo_id":       "repo-1",
			"language":      "go",
			"entity_metadata": map[string]any{
				"class_context": "Handler",
			},
		},
	})
	if !ok {
		t.Fatal("buildSemanticEntityReducerIntent() ok = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainSemanticEntityMaterialization; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "repo:repo-1"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
}
