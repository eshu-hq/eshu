package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeInterprocEvidenceReducerIntent queues one cross-function evidence
// materialization intent per scope generation when any code_interproc_evidence
// fact is present. The reducer handler loads all such facts for the generation,
// so a single intent drives the whole projection.
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
