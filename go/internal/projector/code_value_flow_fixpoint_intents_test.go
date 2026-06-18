package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCodeValueFlowFixpointReducerIntentFromSummaryFact(t *testing.T) {
	t.Parallel()
	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeValueFlowFixpointReducerIntent(scopeValue, generation, []facts.Envelope{
		{FactKind: facts.CodeFunctionSummaryFactKind, FactID: "sum-1", CollectorKind: "git"},
	})
	if !ok {
		t.Fatal("no fixpoint intent queued for a code_function_summary fact")
	}
	if intent.Domain != reducer.DomainCodeValueFlowFixpoint || intent.EntityKey != "code_value_flow_fixpoint:scope-1" {
		t.Fatalf("intent domain/key wrong: %+v", intent)
	}
}

func TestBuildCodeValueFlowFixpointReducerIntentNoSummaryNoIntent(t *testing.T) {
	t.Parallel()
	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	if _, ok := buildCodeValueFlowFixpointReducerIntent(scopeValue, generation, []facts.Envelope{{FactKind: "file"}}); ok {
		t.Fatal("queued a fixpoint intent without any summary fact")
	}
}
