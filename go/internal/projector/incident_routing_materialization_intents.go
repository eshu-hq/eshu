// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildIncidentRoutingMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	candidateKinds := append([]string{facts.IncidentRecordFactKind}, facts.IncidentRoutingFactKinds()...)
	envelope, ok := index.firstAcrossKinds(func(facts.Envelope) bool { return true }, candidateKinds...)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainIncidentRoutingMaterialization,
		EntityKey:    "incident_routing_materialization:" + scopeValue.ScopeID,
		Reason:       "pagerduty incident-routing evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: incidentRoutingMaterializationSourceSystem(envelope),
	}, true
}

func incidentRoutingMaterializationSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
