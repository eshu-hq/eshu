// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perform

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// iamCanPerformIdentityPolicySources are the policy_source values that mark an
// aws_iam_permission fact as a trustable identity-policy statement (issue
// #1134 PR4a §3.3). They mirror the reducer's iamCanPerformPolicySourceInline
// and iamCanPerformPolicySourceAttachedManaged constants
// (internal/reducer/iamcan/iam_can_perform_catalog.go); the duplication keeps
// the projector from importing the reducer package for two strings.
var iamCanPerformIdentityPolicySources = map[string]struct{}{
	"inline":           {},
	"attached_managed": {},
}

// BuildIAMCanPerformMaterializationReducerIntent enqueues one reducer intent
// that projects the scope generation's trustable aws_iam_permission identity
// statements and aws_resource_policy_permission resource-policy statements
// into conservative IAM CAN_PERFORM effective-permission graph edges (issue
// #1134 PR4a/PR4b). The intent is anchored to the earliest qualifying fact
// across both kinds in original input order so the reducer claim is stable
// across reprojections of the same generation, and is only enqueued when at
// least one candidate exists: an aws_iam_permission fact whose payload decodes
// and carries policy_source "inline" or "attached_managed" (a trust statement
// or any other policy_source is not a candidate — CAN_ASSUME owns trust
// statements), or any aws_resource_policy_permission fact. A permission fact
// whose payload fails the typed decode is skipped as a candidate rather than
// failing the build, so a later valid statement in the same generation still
// anchors the intent. This builder does not itself resolve grants, actions, or
// target resources — the reducer's IAMCanPerformMaterializationHandler owns
// the bounded join, the closed action catalog, the skip taxonomy, and the
// readiness-gated graph write.
//
// The entity key intentionally matches the AWS resource materialization
// intent ("aws_resource_materialization:<scope>"), the same key
// BuildIAMCanAssumeMaterializationReducerIntent uses, so the edge handler's
// readiness gate resolves the exact GraphProjectionPhaseCanonicalNodesCommitted
// row that #805 PR1 publishes on the cloud_resource_uid keyspace for the same
// acceptance unit — CAN_PERFORM edges never project before the IAM
// principal and target CloudResource nodes commit.
func BuildIAMCanPerformMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(
		func(envelope facts.Envelope) bool {
			switch envelope.FactKind {
			case facts.AWSIAMPermissionFactKind:
				permission, err := decodeIAMCanPerformAWSIAMPermission(envelope)
				if err != nil {
					return false
				}
				_, qualifies := iamCanPerformIdentityPolicySources[permission.PolicySource]
				return qualifies
			case facts.AWSResourcePolicyPermissionFactKind:
				_, err := decodeIAMCanPerformAWSResourcePolicyPermission(envelope)
				return err == nil
			default:
				return false
			}
		},
		facts.AWSIAMPermissionFactKind,
		facts.AWSResourcePolicyPermissionFactKind,
	)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainIAMCanPerformMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "aws iam identity or resource-policy permission statements observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
