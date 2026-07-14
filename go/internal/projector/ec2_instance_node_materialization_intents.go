// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildEC2InstanceNodeMaterializationReducerIntent enqueues one reducer intent
// that materializes the scope generation's ec2_instance_posture facts into
// canonical EC2 instance :CloudResource graph nodes (issue #1146 PR-A). It mirrors
// the #805 aws_resource node trigger: a single scope-keyed intent when any
// ec2_instance_posture fact is present, anchored to the first such fact so the
// reducer claim is stable across reprojections of the same generation.
//
// The intent uses a DISTINCT entity key
// ("ec2_instance_node_materialization:<scope>"), NOT the
// "aws_resource_materialization:<scope>" key the #805 node domain uses. The two
// node domains both publish the cloud_resource_uid / canonical_nodes_committed
// phase for the same scope generation; distinct entity keys keep the two phases
// independent so the future USES_PROFILE edge (#1146 PR-B) gates on instance-node
// readiness on its own, instead of opening as soon as either node domain commits.
func buildEC2InstanceNodeMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.EC2InstancePostureFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainEC2InstanceNodeMaterialization,
		EntityKey:    "ec2_instance_node_materialization:" + scopeValue.ScopeID,
		Reason:       "ec2 instance posture facts observed",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
