// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcanassume

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// iamCanAssumePolicySourceTrust is the policy_source value that marks an
// aws_iam_permission fact as a role trust statement. It mirrors the collector's
// IAMPolicySourceTrust constant; the duplication keeps the projector from
// importing the collector package for one string.
const iamCanAssumePolicySourceTrust = "trust"

// BuildIAMCanAssumeMaterializationReducerIntent enqueues one reducer intent that
// projects the scope generation's aws_iam_permission trust statements into
// canonical CAN_ASSUME graph edges (issue #1134 PR2). The intent is anchored to
// the first trust-source aws_iam_permission fact so the reducer claim is stable
// across reprojections of the same generation, and is only enqueued when at
// least one trust statement exists (identity-policy-only generations enqueue
// nothing). A permission fact whose payload fails the typed decode is skipped
// as a candidate rather than failing the build, so a later valid trust
// statement in the same generation still anchors the intent.
//
// The entity key intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so the edge handler's readiness gate
// resolves the exact GraphProjectionPhaseCanonicalNodesCommitted row that #805
// PR1 publishes on the cloud_resource_uid keyspace for the same acceptance unit
// — trust edges never project before the IAM role/user nodes commit.
func BuildIAMCanAssumeMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKindMatching(facts.AWSIAMPermissionFactKind, func(envelope facts.Envelope) bool {
		permission, err := decodeAWSIAMPermission(envelope)
		if err != nil {
			return false
		}
		return permission.PolicySource == iamCanAssumePolicySourceTrust
	})
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainIAMCanAssumeMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "aws iam trust statements observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
