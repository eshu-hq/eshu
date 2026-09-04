// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanesatisfiedby

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// crossplaneSatisfiedByEntityFactKind mirrors root's
// FactKindParsedEntityObserved ("content_entity", declared in
// go/internal/projector/stage_facts.go) exactly. This package cannot import
// root — root imports this package to dispatch, so the reverse direction
// cycles — so the shared literal is duplicated here rather than referenced.
const crossplaneSatisfiedByEntityFactKind = "content_entity"

// candidateFactKinds is the single fact kind triggerFact ever inspects,
// mirroring the containerimageidentity package's candidateFactKinds
// closed-list shape.
var candidateFactKinds = []string{crossplaneSatisfiedByEntityFactKind}

// BuildCrossplaneSatisfiedByMaterializationReducerIntent enqueues one
// crossplane_satisfied_by_materialization reducer intent per scope
// generation that observed at least one K8sResource or CrossplaneXRD
// content-entity row — the only two entity_type values the domain's
// extraction reads (issue #5347). A Crossplane Claim candidate is never
// parser-labeled: it is an ordinary K8sResource row, so the trigger checks
// entity_type directly (triggerFact) rather than firing on any
// content_entity presence, which would enqueue a (cheap but unnecessary)
// intent for every repo with parsed code entities. Without this builder the
// additive domain is registered and wired but never receives an intent, so
// no SATISFIED_BY edge is ever committed. One intent per scope generation
// matches the per-scope conflict domain (no per-entity fan-out); the
// handler's FactLoader reads every content_entity fact in the generation
// plus the cross-scope active CrossplaneXRD facts.
func BuildCrossplaneSatisfiedByMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(triggerFact, candidateFactKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainCrossplaneSatisfiedByMaterialization,
		EntityKey:    "crossplane_satisfied_by_materialization:" + scopeID,
		Reason:       "k8s_resource/crossplane_xrd content-entity facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

// triggerFact reports whether envelope is a content_entity row whose
// entity_kind (falling back to entity_type, mirroring
// projector.buildContentEntityRecord's dual-path read) is K8sResource or
// CrossplaneXRD — the two candidate types
// crossplane.ExtractCrossplaneSatisfiedByEdgeRows classifies. These are the
// canonical Neo4j label strings internal/content/shape/materialize.go's
// materializeEntities stamps onto entity_type (PascalCase, matching the
// label a content_entity fact ultimately projects to), not the lowercase
// keys of root's entityTypeLabelMap.
func triggerFact(envelope facts.Envelope) bool {
	entityType, ok := payloadString(envelope.Payload, "entity_kind")
	if !ok || entityType == "" {
		entityType, ok = payloadString(envelope.Payload, "entity_type")
	}
	if !ok {
		return false
	}
	switch entityType {
	case "K8sResource", "CrossplaneXRD":
		return true
	default:
		return false
	}
}
