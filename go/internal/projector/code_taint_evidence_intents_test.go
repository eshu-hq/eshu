package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCodeTaintEvidenceReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	if _, ok := buildCodeTaintEvidenceReducerIntent(scopeValue, generation, []facts.Envelope{{FactKind: "file"}}); ok {
		t.Fatal("queued a taint intent without any code_taint_evidence fact")
	}
}

func TestBuildCodeTaintEvidenceReducerIntentFromFact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeTaintEvidenceReducerIntent(scopeValue, generation, []facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeTaintEvidenceFactKind, FactID: "taint-fact-1", CollectorKind: "git"},
	})
	if !ok {
		t.Fatal("no intent queued for a code_taint_evidence fact")
	}
	if intent.Domain != reducer.DomainCodeTaintEvidence {
		t.Fatalf("intent.Domain = %q, want code_taint_evidence", intent.Domain)
	}
	if intent.EntityKey != "code_taint_evidence:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.FactID != "taint-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
}
