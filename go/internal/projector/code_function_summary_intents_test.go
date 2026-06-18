package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCodeFunctionSummaryReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()
	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	if _, ok := buildCodeFunctionSummaryReducerIntent(scopeValue, generation, []facts.Envelope{{FactKind: "file"}}); ok {
		t.Fatal("queued a summary intent without any code_function_summary fact")
	}
}

func TestBuildCodeFunctionSummaryReducerIntentFromFact(t *testing.T) {
	t.Parallel()
	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeFunctionSummaryReducerIntent(scopeValue, generation, []facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeFunctionSummaryFactKind, FactID: "summary-fact-1", CollectorKind: "git"},
	})
	if !ok {
		t.Fatal("no intent queued for a code_function_summary fact")
	}
	if intent.Domain != reducer.DomainCodeFunctionSummary || intent.EntityKey != "code_function_summary:scope-1" {
		t.Fatalf("intent domain/key wrong: %+v", intent)
	}
	if intent.FactID != "summary-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
}
