package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeInterprocEvidenceReducerIntent queues one cross-function evidence
// materialization intent per scope generation when direct code_interproc_evidence
// facts are present. Summary-driven fixpoint projection is triggered after the
// function-summary handler persists durable summaries, sources, and graph ids so
// it cannot retract direct interproc evidence under the same scope.
func buildCodeInterprocEvidenceReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CodeInterprocEvidenceFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainCodeInterprocEvidence,
			EntityKey:    "code_interproc_evidence:" + scopeValue.ScopeID,
			Reason:       "cross-function value-flow evidence observed",
			FactID:       envelope.FactID,
			SourceSystem: strings.TrimSpace(envelope.CollectorKind),
		}, true
	}
	return ReducerIntent{}, false
}
