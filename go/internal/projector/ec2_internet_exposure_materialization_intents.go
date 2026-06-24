// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildEC2InternetExposureMaterializationReducerIntent enqueues one reducer
// intent that derives EC2 internet-exposure properties from ec2_instance_posture
// and EC2 relationship/rule facts for the scope generation. The entity key
// intentionally matches the EC2 instance-node materialization readiness row.
func buildEC2InternetExposureMaterializationReducerIntent(
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
			Domain:       reducer.DomainEC2InternetExposureMaterialization,
			EntityKey:    "ec2_instance_node_materialization:" + scopeValue.ScopeID,
			Reason:       "ec2 instance posture observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
