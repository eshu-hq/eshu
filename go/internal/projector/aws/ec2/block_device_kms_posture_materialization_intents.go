// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildBlockDeviceKMSPostureMaterializationReducerIntent enqueues one reducer
// intent that derives EC2 block-device KMS posture from the scope generation's
// ec2_instance_posture facts joined to EBS volume and KMS facts. It queues on any
// EC2 posture fact, including no-block-device instances, because "no block
// devices" is itself a conservative unknown posture state that should retract
// stale prior properties.
func BuildBlockDeviceKMSPostureMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.EC2InstancePostureFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainEC2BlockDeviceKMSPostureMaterialization,
		EntityKey:    "ec2_block_device_kms_posture_materialization:" + scopeID,
		Reason:       "ec2 block-device posture observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
