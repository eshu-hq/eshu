// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildWorkloadCloudRelationshipMaterializationReducerIntent enqueues one
// reducer intent that promotes exact workload anchors on aws_resource facts into
// WorkloadInstance USES CloudResource graph edges. The entity key intentionally
// matches the CloudResource node materialization slice so the reducer can gate
// on that readiness row while the graph writer handles missing workload
// endpoints with MATCH-only no-ops.
func buildWorkloadCloudRelationshipMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainWorkloadCloudRelationshipMaterialization,
			EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
			Reason:       "aws resource workload anchors observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
