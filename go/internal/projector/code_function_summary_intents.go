package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildCodeFunctionSummaryReducerIntent queues one function-summary persistence
// intent per scope generation when any code_function_summary fact is present. The
// reducer handler loads all such facts for the generation, recomputes their
// content versions, and upserts the snapshot, so a single intent drives the whole
// persistence.
func buildCodeFunctionSummaryReducerIntent(
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
			Domain:       reducer.DomainCodeFunctionSummary,
			EntityKey:    "code_function_summary:" + scopeValue.ScopeID,
			Reason:       "value-flow function summaries observed",
			FactID:       envelope.FactID,
			SourceSystem: strings.TrimSpace(envelope.CollectorKind),
		}, true
	}
	return ReducerIntent{}, false
}
