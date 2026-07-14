// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildGCPResourceMaterializationReducerIntent enqueues one reducer intent that
// materializes the scope generation's gcp_cloud_resource facts into canonical
// CloudResource graph nodes (issue #2358), mirroring the AWS resource
// materialization trigger: a single scope-keyed intent when any
// gcp_cloud_resource fact is present, anchored to the first such fact so the
// reducer claim is stable across reprojections of the same generation.
//
// The entity key uses the gcp_resource_materialization:<scope> namespace so the
// canonical-nodes-committed readiness phase keys to a GCP acceptance unit
// distinct from the AWS resource materialization unit. The GCP relationship edge
// projection (#2348) reuses this same entity key so its readiness gate resolves
// the exact phase row this materialization publishes.
func buildGCPResourceMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.GCPCloudResourceFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainGCPResourceMaterialization,
		EntityKey:    "gcp_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "gcp cloud resource facts observed",
		FactID:       envelope.FactID,
		SourceSystem: cloudInventoryAdmissionSourceSystem(envelope),
	}, true
}
