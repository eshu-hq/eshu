// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildEC2BlockDeviceKMSPostureMaterializationReducerIntent enqueues one reducer
// intent that derives EC2 block-device KMS posture from the scope generation's
// ec2_instance_posture facts joined to EBS volume and KMS facts. It queues on any
// EC2 posture fact, including no-block-device instances, because "no block
// devices" is itself a conservative unknown posture state that should retract
// stale prior properties.
func buildEC2BlockDeviceKMSPostureMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.EC2InstancePostureFactKind {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainEC2BlockDeviceKMSPostureMaterialization,
			EntityKey:    "ec2_block_device_kms_posture_materialization:" + scopeValue.ScopeID,
			Reason:       "ec2 block-device posture observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
