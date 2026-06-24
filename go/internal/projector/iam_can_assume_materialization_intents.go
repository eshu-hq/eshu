// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// iamCanAssumePolicySourceTrust is the policy_source value that marks an
// aws_iam_permission fact as a role trust statement. It mirrors the collector's
// IAMPolicySourceTrust constant; the duplication keeps the projector from
// importing the collector package for one string.
const iamCanAssumePolicySourceTrust = "trust"

// buildIAMCanAssumeMaterializationReducerIntent enqueues one reducer intent that
// projects the scope generation's aws_iam_permission trust statements into
// canonical CAN_ASSUME graph edges (issue #1134 PR2). The intent is anchored to
// the first trust-source aws_iam_permission fact so the reducer claim is stable
// across reprojections of the same generation, and is only enqueued when at
// least one trust statement exists (identity-policy-only generations enqueue
// nothing).
//
// The entity key intentionally matches the AWS resource materialization intent
// ("aws_resource_materialization:<scope>") so the edge handler's readiness gate
// resolves the exact GraphProjectionPhaseCanonicalNodesCommitted row that #805
// PR1 publishes on the cloud_resource_uid keyspace for the same acceptance unit
// — trust edges never project before the IAM role/user nodes commit.
func buildIAMCanAssumeMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) (ReducerIntent, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSIAMPermissionFactKind {
			continue
		}
		if source, _ := payloadString(envelope.Payload, "policy_source"); source != iamCanAssumePolicySourceTrust {
			continue
		}
		return ReducerIntent{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
			Domain:       reducer.DomainIAMCanAssumeMaterialization,
			EntityKey:    "aws_resource_materialization:" + scopeValue.ScopeID,
			Reason:       "aws iam trust statements observed",
			FactID:       envelope.FactID,
			SourceSystem: awsCloudRuntimeDriftSourceSystem(envelope),
		}, true
	}
	return ReducerIntent{}, false
}
