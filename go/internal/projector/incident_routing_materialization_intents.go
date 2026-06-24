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
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if !isIncidentRoutingMaterializationFactKind(envelope.FactKind) {
			continue
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
	return ReducerIntent{}, false
}

func isIncidentRoutingMaterializationFactKind(factKind string) bool {
	if factKind == facts.IncidentRecordFactKind {
		return true
	}
	for _, routingKind := range facts.IncidentRoutingFactKinds() {
		if factKind == routingKind {
			return true
		}
	}
	return false
}

func incidentRoutingMaterializationSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
