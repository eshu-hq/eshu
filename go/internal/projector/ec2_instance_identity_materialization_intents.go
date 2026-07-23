// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildEC2InstanceIdentityMaterializationReducerIntent enqueues one reducer
// intent that projects the scope generation's aws_ec2_instance aws_resource
// facts' ami_id onto the already-materialized EC2 instance CloudResource nodes
// (#5448). It mirrors the #805 aws_resource node trigger: a single
// scope-keyed intent when ANY aws_resource fact is present (the handler's own
// extraction filters to resource_type=aws_ec2_instance; this fan-out cannot
// cheaply pre-filter by payload attribute, so an intent with zero matching
// rows is a safe no-op, exactly like DomainAWSResourceMaterialization's own
// builder). The intent is anchored to the first aws_resource fact so the
// reducer claim is stable across reprojections of the same generation.
//
// The intent uses the SAME entity key
// ("ec2_instance_node_materialization:<scope>") DomainEC2InstanceNodeMaterialization
// publishes its canonical_nodes_committed phase under (#1146 PR-A) — NOT the
// "aws_resource_materialization:<scope>" key DomainRDSPostureMaterialization
// reuses. This is the deliberate distinction #5448 CRUX-1 requires: the EC2
// instance CloudResource node this domain augments is owned by the posture
// path, not by the generic aws_resource node path (which explicitly excludes
// aws_ec2_instance, see go/internal/reducer/aws_resource_materialization.go).
// Reusing the wrong entity key would let the readiness gate open before the
// node this domain actually augments has committed.
func buildEC2InstanceIdentityMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.AWSResourceFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainEC2InstanceIdentityMaterialization,
		EntityKey:    "ec2_instance_node_materialization:" + scopeValue.ScopeID,
		Reason:       "aws resource facts observed for ec2 instance identity projection",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
