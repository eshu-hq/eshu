// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const iamInstanceProfileRoleResourceTypeInstanceProfile = "aws_iam_instance_profile"

// buildIAMInstanceProfileRoleMaterializationReducerIntent enqueues one reducer
// intent that projects IAM instance-profile role_arns into canonical HAS_ROLE
// graph edges (issue #1299). It anchors to the first instance-profile
// aws_resource fact even when role_arns is empty, because a no-role generation
// still has to retract stale reducer-owned HAS_ROLE edges from a prior
// generation.
//
// The entity key intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so the handler's readiness gate
// resolves the CloudResource canonical-nodes phase for both profile and role
// nodes before writing edges.
func buildIAMInstanceProfileRoleMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKindMatching(facts.AWSResourceFactKind, func(envelope facts.Envelope) bool {
		resource, err := decodeAWSResource(envelope)
		if err != nil {
			return false
		}
		return resource.ResourceType == iamInstanceProfileRoleResourceTypeInstanceProfile
	})
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainIAMInstanceProfileRoleMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
		Reason:       "iam instance profiles observed",
		FactID:       envelope.FactID,
		SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
	}, true
}
