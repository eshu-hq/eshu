package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildServiceCatalogCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if _, ok := facts.ServiceCatalogSchemaVersion(envelope.FactKind); !ok {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainServiceCatalogCorrelation,
			EntityKey:    "service_catalog_correlation:" + scopeValue.ScopeID,
			Reason:       "service catalog facts observed",
			FactID:       envelope.FactID,
			SourceSystem: serviceCatalogCorrelationSourceSystem(scopeValue, envelope),
		}, true
	}
	return ReducerIntent{}, false
}

func serviceCatalogCorrelationSourceSystem(
	scopeValue scope.IngestionScope,
	envelope facts.Envelope,
) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	if value := strings.TrimSpace(envelope.CollectorKind); value != "" {
		return value
	}
	return strings.TrimSpace(scopeValue.SourceSystem)
}
