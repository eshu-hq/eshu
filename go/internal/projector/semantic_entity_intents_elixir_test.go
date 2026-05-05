package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildSemanticEntityReducerIntentAcceptsElixirProtocolEntities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		entityType string
		entityName string
	}{
		{name: "protocol", entityType: "Protocol", entityName: "Demo.Serializable"},
		{name: "protocol implementation", entityType: "ProtocolImplementation", entityName: "Demo.Serializable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			intent, ok := buildSemanticEntityReducerIntent(testElixirSemanticEntityFact(
				tt.entityType,
				tt.entityName,
				"elixir",
				map[string]any{"module_kind": "protocol"},
			))
			if !ok {
				t.Fatalf("buildSemanticEntityReducerIntent() ok = false, want true")
			}
			if got, want := intent.Domain, reducer.DomainSemanticEntityMaterialization; got != want {
				t.Fatalf("intent.Domain = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildSemanticEntityReducerIntentAcceptsElixirModuleAttributeEntities(t *testing.T) {
	t.Parallel()

	intent, ok := buildSemanticEntityReducerIntent(testElixirSemanticEntityFact(
		"Variable",
		"@timeout",
		"elixir",
		map[string]any{
			"attribute_kind": "module_attribute",
			"value":          "5_000",
		},
	))
	if !ok {
		t.Fatalf("buildSemanticEntityReducerIntent() ok = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainSemanticEntityMaterialization; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
}

func testElixirSemanticEntityFact(entityType, entityName, language string, metadata map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind:     "content_entity",
		FactID:       "fact-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceRef: facts.Ref{
			SourceSystem: "parser",
			SourceURI:    "lib/demo/serializable.ex",
		},
		Payload: map[string]any{
			"repo_id":         "repo-1",
			"relative_path":   "lib/demo/serializable.ex",
			"entity_type":     entityType,
			"entity_name":     entityName,
			"language":        language,
			"start_line":      1,
			"entity_metadata": metadata,
		},
	}
}
