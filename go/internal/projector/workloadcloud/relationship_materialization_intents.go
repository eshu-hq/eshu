// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadcloud

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildWorkloadCloudRelationshipMaterializationReducerIntent enqueues one
// reducer intent that promotes exact workload anchors on aws_resource facts
// into WorkloadInstance USES CloudResource graph edges. The entity key
// intentionally matches the CloudResource node materialization slice so the
// reducer can gate on that readiness row while the graph writer handles
// missing workload endpoints with MATCH-only no-ops.
func BuildWorkloadCloudRelationshipMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AWSResourceFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainWorkloadCloudRelationshipMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "aws resource workload anchors observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
