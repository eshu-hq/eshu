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
// (#5448). It triggers on the ec2_instance_posture fact — the SAME fact
// DomainEC2InstanceNodeMaterialization triggers on — so it enqueues exactly when
// the node it augments does. The ami_id it writes comes from the co-present
// aws_ec2_instance aws_resource fact, which the handler loads and extracts
// itself; an intent whose scope has the posture node but no matching ami_id row
// is a safe no-op.
//
// The intent uses the SAME entity key
// ("ec2_instance_node_materialization:<scope>") DomainEC2InstanceNodeMaterialization
// publishes its canonical_nodes_committed phase under (#1146 PR-A) — NOT the
// "aws_resource_materialization:<scope>" key DomainRDSPostureMaterialization
// reuses. This is the deliberate distinction #5448 CRUX-1 requires: the EC2
// instance CloudResource node this domain augments is owned by the posture
// path, not by the generic aws_resource node path (which explicitly excludes
// aws_ec2_instance, see go/internal/reducer/aws_resource_materialization.go).
//
// Triggering on the posture fact rather than on any aws_resource fact is the
// #5743 residual fix: the readiness gate for this domain waits on the EC2
// instance node, which a no-EC2-instance aws scope (ecr/lambda/ecs) never
// materializes. Enqueuing there on a generic aws_resource fact left the work
// item stuck 'pending' forever because the gate could never open — the
// golden-corpus fact_work_items_residual failure. Aligning the trigger with the
// node's own trigger enqueues the intent only when the gate is satisfiable.
func buildEC2InstanceIdentityMaterializationReducerIntent(
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
		Domain:       reducer.DomainEC2InstanceIdentityMaterialization,
		EntityKey:    "ec2_instance_node_materialization:" + scopeValue.ScopeID,
		Reason:       "ec2 instance posture observed for ec2 instance identity projection",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
