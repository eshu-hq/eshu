package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildObservabilityCoverageCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if !observabilityCoverageCorrelationTriggerFact(envelope) {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainObservabilityCoverageCorrelation,
			EntityKey:    "observability_coverage_correlation:" + scopeValue.ScopeID,
			Reason:       observabilityCoverageCorrelationReason(envelope),
			FactID:       envelope.FactID,
			SourceSystem: observabilitySourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}

func observabilityCoverageCorrelationTriggerFact(envelope facts.Envelope) bool {
	if envelope.FactKind == facts.AWSResourceFactKind {
		_, ok := observabilityResourceTypes[awsResourceTypeForEnvelope(envelope)]
		return ok
	}
	if envelope.FactKind == facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	_, ok := facts.ObservabilitySchemaVersion(envelope.FactKind)
	return ok
}

func observabilityCoverageCorrelationReason(envelope facts.Envelope) string {
	if envelope.FactKind == facts.AWSResourceFactKind {
		return "aws observability resource facts observed"
	}
	return "observability source facts observed"
}

func observabilitySourceSystem(envelope facts.Envelope) string {
	if envelope.SourceRef.SourceSystem != "" {
		return envelope.SourceRef.SourceSystem
	}
	if envelope.CollectorKind != "" {
		return envelope.CollectorKind
	}
	return "observability"
}
