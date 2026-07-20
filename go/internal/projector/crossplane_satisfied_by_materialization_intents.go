// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// crossplaneSatisfiedByCandidateFactKinds is the single fact kind
// crossplaneSatisfiedByTriggerFact ever inspects, mirroring
// containerImageIdentityCandidateFactKinds's closed-list shape.
var crossplaneSatisfiedByCandidateFactKinds = []string{FactKindParsedEntityObserved}

// buildCrossplaneSatisfiedByMaterializationReducerIntent enqueues one
// crossplane_satisfied_by_materialization reducer intent per scope
// generation that observed at least one K8sResource or CrossplaneXRD
// content-entity row — the only two entity_type values the domain's
// extraction reads (issue #5347). A Crossplane Claim candidate is never
// parser-labeled: it is an ordinary K8sResource row, so the trigger checks
// entity_type directly (crossplaneSatisfiedByTriggerFact) rather than firing
// on any content_entity presence, which would enqueue a (cheap but
// unnecessary) intent for every repo with parsed code entities. Without this
// builder the additive domain is registered and wired but never receives an
// intent, so no SATISFIED_BY edge is ever committed. One intent per scope
// generation matches the per-scope conflict domain (no per-entity fan-out);
// the handler's FactLoader reads every content_entity fact in the generation
// plus the cross-scope active CrossplaneXRD facts.
func buildCrossplaneSatisfiedByMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstAcrossKinds(crossplaneSatisfiedByTriggerFact, crossplaneSatisfiedByCandidateFactKinds...)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainCrossplaneSatisfiedByMaterialization,
		EntityKey:    "crossplane_satisfied_by_materialization:" + scopeValue.ScopeID,
		Reason:       "k8s_resource/crossplane_xrd content-entity facts observed",
		FactID:       envelope.FactID,
		SourceSystem: crossplaneSatisfiedBySourceSystem(envelope),
	}, true
}

// crossplaneSatisfiedByTriggerFact reports whether envelope is a content_entity
// row whose entity_type (falling back to entity_kind, mirroring
// projector.buildContentEntityRecord's dual-path read) is k8s_resource or
// crossplane_xrd — the two candidate types
// reducer.ExtractCrossplaneSatisfiedByEdgeRows classifies.
func crossplaneSatisfiedByTriggerFact(envelope facts.Envelope) bool {
	entityType, ok := payloadString(envelope.Payload, "entity_kind")
	if !ok || entityType == "" {
		entityType, ok = payloadString(envelope.Payload, "entity_type")
	}
	if !ok {
		return false
	}
	switch entityType {
	case "k8s_resource", "crossplane_xrd":
		return true
	default:
		return false
	}
}

func crossplaneSatisfiedBySourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
