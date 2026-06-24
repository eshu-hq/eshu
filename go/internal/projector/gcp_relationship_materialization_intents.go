// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildGCPRelationshipMaterializationReducerIntent enqueues one reducer intent
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
func buildGCPRelationshipMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.GCPCloudRelationshipFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainGCPRelationshipMaterialization,
			EntityKey:    "gcp_resource_materialization:" + scopeValue.ScopeID,
			Reason:       "gcp runtime relationship facts observed",
			FactID:       envelope.FactID,
			SourceSystem: cloudInventoryAdmissionSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
