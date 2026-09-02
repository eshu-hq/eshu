// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iaminstanceprofile

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const iamInstanceProfileRoleResourceTypeInstanceProfile = "aws_iam_instance_profile"

// BuildIAMInstanceProfileRoleMaterializationReducerIntent enqueues one reducer
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
func BuildIAMInstanceProfileRoleMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKindMatching(facts.AWSResourceFactKind, func(envelope facts.Envelope) bool {
		resource, err := decodeIAMInstanceProfileAWSResource(envelope)
		if err != nil {
			return false
		}
		return resource.ResourceType == iamInstanceProfileRoleResourceTypeInstanceProfile
	})
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainIAMInstanceProfileRoleMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "iam instance profiles observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
