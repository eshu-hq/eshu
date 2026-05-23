package projector

import (
	"fmt"
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

func validateServiceCatalogSchemaVersion(envelope facts.Envelope) error {
	want, ok := facts.ServiceCatalogSchemaVersion(envelope.FactKind)
	if !ok {
		return nil
	}
	got := strings.TrimSpace(envelope.SchemaVersion)
	if got == "" {
		return fmt.Errorf("service catalog fact %q schema_version must not be blank", envelope.FactID)
	}
	if got != want {
		return fmt.Errorf(
			"service catalog fact %q schema_version %q is unsupported for %s; want %q",
			envelope.FactID,
			got,
			envelope.FactKind,
			want,
		)
	}
	return nil
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
