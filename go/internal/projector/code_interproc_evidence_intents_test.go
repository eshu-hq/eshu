package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildCodeInterprocEvidenceReducerIntentNoFactNoIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	if _, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, []facts.Envelope{{FactKind: "file"}}); ok {
		t.Fatal("queued an interproc intent without any code_interproc_evidence fact")
	}
}

func TestBuildCodeInterprocEvidenceReducerIntentFromFact(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intent, ok := buildCodeInterprocEvidenceReducerIntent(scopeValue, generation, []facts.Envelope{
		{FactKind: "file"},
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "interproc-fact-1", CollectorKind: "git"},
	})
	if !ok {
		t.Fatal("no intent queued for a code_interproc_evidence fact")
	}
	if intent.Domain != reducer.DomainCodeInterprocEvidence {
		t.Fatalf("intent.Domain = %q, want code_interproc_evidence", intent.Domain)
	}
	if intent.EntityKey != "code_interproc_evidence:scope-1" {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
	if intent.FactID != "interproc-fact-1" || intent.SourceSystem != "git" {
		t.Fatalf("intent fact/source not carried: %+v", intent)
	}
}

// TestAppendScopeGenerationReducerIntentsWiresCodeInterproc proves the interproc
// builder is actually wired into the scope-generation intent chain, not just
// defined in isolation.
func TestAppendScopeGenerationReducerIntentsWiresCodeInterproc(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "scope-1"}
	generation := scope.ScopeGeneration{GenerationID: "gen-1"}
	intents := appendScopeGenerationReducerIntents(nil, scopeValue, generation, []facts.Envelope{
		{FactKind: facts.CodeInterprocEvidenceFactKind, FactID: "interproc-fact-1", CollectorKind: "git"},
	})
	found := false
	for _, intent := range intents {
		if intent.Domain == reducer.DomainCodeInterprocEvidence {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("code_interproc_evidence intent not produced by the scope-generation chain")
	}
}
