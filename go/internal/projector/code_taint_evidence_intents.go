package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeTaintEvidenceReducerIntent queues one taint-evidence materialization
// intent per scope generation when any code_taint_evidence fact is present. The
// reducer handler loads all such facts for the generation, so a single intent
// drives the whole projection.
func buildCodeTaintEvidenceReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CodeTaintEvidenceFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainCodeTaintEvidence,
			EntityKey:    "code_taint_evidence:" + scopeValue.ScopeID,
			Reason:       "value-flow taint evidence observed",
			FactID:       envelope.FactID,
			SourceSystem: strings.TrimSpace(envelope.CollectorKind),
		}, true
	}
	return ReducerIntent{}, false
}
