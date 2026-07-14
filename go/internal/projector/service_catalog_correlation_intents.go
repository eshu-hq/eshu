// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstMatchingKindPredicate(
		func(kind string) bool {
			_, isServiceCatalogKind := facts.ServiceCatalogSchemaVersion(kind)
			return isServiceCatalogKind
		},
		func(facts.Envelope) bool { return true },
	)
	if !ok {
		return ReducerIntent{}, false
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
