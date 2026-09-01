// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// BuildServiceCatalogCorrelationReducerIntent enqueues one reducer intent
// that asks the reducer to correlate the scope generation's service-catalog
// facts against repository and deployment truth. Any fact whose kind the
// facts.ServiceCatalogSchemaVersion registry recognizes (entity, ownership,
// repository link, dependency, API link, operational link, scorecard
// definition and result, warning) is a trigger; the anchor is the earliest
// such fact in original input order across every recognized kind, so a
// generation carrying several catalog kinds still enqueues once with a stable
// FactID. Only envelope.FactKind is read — the payload is never decoded here,
// and schema-version admission stays with root projection. A generation with
// no service-catalog fact enqueues nothing.
func BuildServiceCatalogCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstMatchingKindPredicate(
		func(kind string) bool {
			_, isServiceCatalogKind := facts.ServiceCatalogSchemaVersion(kind)
			return isServiceCatalogKind
		},
		func(facts.Envelope) bool { return true },
	)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainServiceCatalogCorrelation,
		EntityKey:    "service_catalog_correlation:" + scopeValue.ScopeID,
		Reason:       "service catalog facts observed",
		FactID:       envelope.FactID,
		SourceSystem: sourceSystem(scopeValue, envelope),
	}, true
}

// sourceSystem labels the intent with the anchor fact's SourceRef source
// system, then its CollectorKind, then the ingestion scope's own source
// system, each trimmed. The third tier is what distinguishes this family from
// projectorintent.SourceSystem: a catalog fact with neither envelope label
// still carries the scope's label instead of an empty string, so the fallback
// order must stay exactly as written.
func sourceSystem(scopeValue scope.IngestionScope, envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	if value := strings.TrimSpace(envelope.CollectorKind); value != "" {
		return value
	}
	return strings.TrimSpace(scopeValue.SourceSystem)
}
