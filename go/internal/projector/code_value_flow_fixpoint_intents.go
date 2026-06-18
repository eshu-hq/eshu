package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeValueFlowFixpointReducerIntent queues one cross-repo value-flow
// fixpoint intent per scope generation whenever that generation emitted any
// function summary. The handler reads the global summary/source/uid stores, so a
// single intent drives the whole cross-repo composition; re-running converges via
// idempotent edge MERGE.
func buildCodeValueFlowFixpointReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CodeFunctionSummaryFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainCodeValueFlowFixpoint,
			EntityKey:    "code_value_flow_fixpoint:" + scopeValue.ScopeID,
			Reason:       "value-flow summaries observed; recompose cross-repo flows",
			FactID:       envelope.FactID,
			SourceSystem: strings.TrimSpace(envelope.CollectorKind),
		}, true
	}
	return ReducerIntent{}, false
}
