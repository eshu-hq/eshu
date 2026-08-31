// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildInternetExposureMaterializationReducerIntent enqueues one reducer
// intent that derives EC2 internet-exposure properties from ec2_instance_posture
// and EC2 relationship/rule facts for the scope generation. The entity key
// intentionally matches the EC2 instance-node materialization readiness row.
func BuildInternetExposureMaterializationReducerIntent(
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
		Domain:       reducer.DomainEC2InternetExposureMaterialization,
		EntityKey:    "ec2_instance_node_materialization:" + scopeID,
		Reason:       "ec2 instance posture observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
