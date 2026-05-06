package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBuildSemanticEntityReducerIntentQueuesRustImplBlockSemanticEntities(t *testing.T) {
	t.Parallel()

	intent, ok := buildSemanticEntityReducerIntent(facts.Envelope{
		FactID:       "fact-1",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "content_entity",
		Payload: map[string]any{
			"entity_type":   "ImplBlock",
			"entity_id":     "impl-1",
			"entity_name":   "Point",
			"relative_path": "src/point.rs",
			"repo_id":       "repo-1",
			"language":      "rust",
			"kind":          "trait_impl",
			"trait":         "Display",
			"target":        "Point",
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
	if got, want := intent.Reason, "semantic entity follow-up for ImplBlock"; got != want {
		t.Fatalf("intent.Reason = %q, want %q", got, want)
	}
}

func TestBuildSemanticEntityReducerIntentQueuesPythonFunctionSemanticEntities(t *testing.T) {
	t.Parallel()

	intent, ok := buildSemanticEntityReducerIntent(facts.Envelope{
		FactID:       "fact-2",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "content_entity",
		Payload: map[string]any{
			"entity_type":   "Function",
			"entity_id":     "function-1",
			"entity_name":   "handler",
			"relative_path": "src/app.py",
			"repo_id":       "repo-1",
			"language":      "python",
			"decorators":    []any{"@route"},
			"async":         true,
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

func TestBuildSemanticEntityReducerIntentQueuesElixirGuardSemanticEntities(t *testing.T) {
	t.Parallel()

	intent, ok := buildSemanticEntityReducerIntent(facts.Envelope{
		FactID:       "fact-3",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "content_entity",
		Payload: map[string]any{
			"entity_type":   "Function",
			"entity_id":     "function-guard-1",
			"entity_name":   "is_even",
			"relative_path": "lib/demo/macros.ex",
			"repo_id":       "repo-1",
			"language":      "elixir",
			"semantic_kind": "guard",
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
	if got, want := intent.Reason, "semantic entity follow-up for Function"; got != want {
		t.Fatalf("intent.Reason = %q, want %q", got, want)
	}
}

func TestBuildSemanticEntityReducerIntentQueuesTypeScriptModuleSemanticEntities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "namespace",
			payload: map[string]any{
				"entity_type":   "Module",
				"entity_id":     "module-1",
				"entity_name":   "API",
				"relative_path": "src/types.ts",
				"repo_id":       "repo-1",
				"language":      "typescript",
				"module_kind":   "namespace",
			},
		},
		{
			name: "declaration merging",
			payload: map[string]any{
				"entity_type":             "Module",
				"entity_id":               "module-2",
				"entity_name":             "Service",
				"relative_path":           "src/merge.ts",
				"repo_id":                 "repo-1",
				"language":                "typescript",
				"declaration_merge_group": "Service",
				"declaration_merge_count": 2,
				"declaration_merge_kinds": []any{"class", "namespace"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			intent, ok := buildSemanticEntityReducerIntent(facts.Envelope{
				FactID:       "fact-module",
				ScopeID:      "scope-123",
				GenerationID: "generation-456",
				FactKind:     "content_entity",
				Payload:      tt.payload,
			})
			if !ok {
				t.Fatal("buildSemanticEntityReducerIntent() ok = false, want true")
			}
			if got, want := intent.Domain, reducer.DomainSemanticEntityMaterialization; got != want {
				t.Fatalf("intent.Domain = %q, want %q", got, want)
			}
			want := "repo:repo-1"
			if got := intent.EntityKey; got != want {
				t.Fatalf("intent.EntityKey = %q, want %q", got, want)
			}
		})
	}
}
