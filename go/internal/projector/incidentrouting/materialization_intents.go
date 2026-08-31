// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incidentrouting

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildIncidentRoutingMaterializationReducerIntent enqueues one reducer
// intent when a scope generation carries PagerDuty incident-routing evidence:
// an incident.record fact or any incident_routing.* source fact. The intent is
// anchored to the earliest such fact in original input order across every
// candidate kind, so the reducer claim stays stable across reprojections. The
// projector never compares declared, applied, or live routing evidence and
// never decodes the payload; the reducer owns IncidentRoutingEvidence
// materialization.
func BuildIncidentRoutingMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	candidateKinds := append([]string{facts.IncidentRecordFactKind}, facts.IncidentRoutingFactKinds()...)
	envelope, ok := lookup.FirstAcrossKinds(func(facts.Envelope) bool { return true }, candidateKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainIncidentRoutingMaterialization,
		EntityKey:    "incident_routing_materialization:" + scopeID,
		Reason:       "pagerduty incident-routing evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
