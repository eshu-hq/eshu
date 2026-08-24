// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildRelationshipMaterializationReducerIntent returns one reducer intent
// that projects the scope generation's gcp_cloud_relationship facts into
// canonical GCP relationship graph edges (issue #2348), mirroring the AWS
// relationship trigger. The intent is anchored to the first gcp_cloud_relationship
// fact so the reducer claim is stable across reprojections of the same
// generation.
//
// The entity key intentionally matches the GCP resource materialization intent
// ("gcp_resource_materialization:<scope>") so the edge handler's readiness gate
// resolves the exact GraphProjectionPhaseCanonicalNodesCommitted row that
// DomainGCPResourceMaterialization publishes for the same acceptance unit —
// edges never project before GCP nodes commit.
func BuildRelationshipMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.GCPCloudRelationshipFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainGCPRelationshipMaterialization,
		EntityKey:    "gcp_resource_materialization:" + scopeID,
		Reason:       "gcp runtime relationship facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
