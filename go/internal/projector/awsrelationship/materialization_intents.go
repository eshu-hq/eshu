// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsrelationship

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildAWSRelationshipMaterializationReducerIntent enqueues one reducer intent
// that projects the scope generation's aws_relationship facts into canonical
// AWS relationship graph edges (issue #805 PR 2). The intent is anchored to the
// first aws_relationship fact so the reducer claim is stable across
// reprojections of the same generation.
//
// The entity key intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so the edge handler's readiness gate
// resolves the exact GraphProjectionPhaseCanonicalNodesCommitted row that PR 1
// publishes for the same acceptance unit — edges never project before nodes
// commit.
func BuildAWSRelationshipMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.AWSRelationshipFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainAWSRelationshipMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "aws runtime relationship facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
