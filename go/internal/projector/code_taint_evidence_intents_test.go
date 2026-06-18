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

// TestBuildProjectionQueuesCodeTaintEvidence proves the live runtime projection
// (buildProjection -> appendScopeGenerationReducerIntents) enqueues a
// DomainCodeTaintEvidence intent from a code_taint_evidence fact. This is the
// same FactKind-based intent path the incident-routing domain uses; the fact
// carries graph_kind only (no reducer_domain), so the scope-generation builder —
// not the payload-domain buildReducerIntent — is what enqueues it.
func TestBuildProjectionQueuesCodeTaintEvidence(t *testing.T) {
	t.Parallel()

	scopeValue, generation := incidentRoutingProjectionScope()
	envelopes := []facts.Envelope{{
		FactKind:      facts.CodeTaintEvidenceFactKind,
		FactID:        "taint-fact-1",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		CollectorKind: "git",
		Payload:       map[string]any{"graph_kind": "code_taint_evidence", "function_uid": "func-1"},
	}}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainCodeTaintEvidence)
	if intent.FactID != "taint-fact-1" {
		t.Fatalf("intent.FactID = %q, want taint-fact-1", intent.FactID)
	}
	if intent.EntityKey != "code_taint_evidence:"+scopeValue.ScopeID {
		t.Fatalf("intent.EntityKey = %q", intent.EntityKey)
	}
}
