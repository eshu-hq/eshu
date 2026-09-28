// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package escalation

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// iamEscalationIdentityPolicySources are the policy_source values that mark
// an aws_iam_permission fact as an identity statement the escalation
// extractor can use (#6228). They mirror the sibling perform package's
// iamCanPerformIdentityPolicySources: the reducer's grant folds every Allow
// statement's trusted actions without a policy-source filter, but a trust
// statement (policy_source "trust") is the sibling trust package's trigger,
// not this family's — fanning an escalation intent from trust statements
// would double-project what CAN_ASSUME already owns. "inline" and
// "attached_managed" are the collector's identity-statement sources; the
// duplication keeps the projector from importing the reducer package for two
// strings.
var iamEscalationIdentityPolicySources = map[string]struct{}{
	"inline":           {},
	"attached_managed": {},
}

// BuildIAMEscalationMaterializationReducerIntent enqueues one reducer intent
// that projects the scope generation's identity statements into conservative
// IAM CAN_ESCALATE_TO privilege-escalation graph edges (#6228). The intent
// is anchored to the earliest qualifying fact in original input order so the
// reducer claim is stable across reprojections of the same generation, and
// is only enqueued when at least one candidate exists: an
// aws_iam_permission fact whose payload decodes and carries policy_source
// "inline" or "attached_managed". A trust statement or any other
// policy_source is not a candidate — CAN_ASSUME owns trust statements. A
// permission fact whose payload fails the typed decode is skipped as a
// candidate rather than failing the build, so a later valid statement in
// the same generation still anchors the intent. Either effect qualifies: an
// Allow can arm a catalog primitive, and a Deny contributes to the grant's
// deny set, so a Deny-only generation still needs the intent fanned out.
// This builder does not itself resolve grants, primitives, or target
// resources — the reducer's IAMEscalationMaterializationHandler owns the
// closed primitive catalog, the skip taxonomy, and the readiness-gated graph
// write.
//
// The entity key intentionally matches the AWS resource materialization
// intent ("aws_resource_materialization:<scope>"), the same key the sibling
// assume and perform builders use, so the edge handler's readiness gate
// resolves the exact GraphProjectionPhaseCanonicalNodesCommitted row that
// #805 PR1 publishes on the cloud_resource_uid keyspace for the same
// acceptance unit — CAN_ESCALATE_TO edges never project before the IAM
// principal and target CloudResource nodes commit.
func BuildIAMEscalationMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKindMatching(facts.AWSIAMPermissionFactKind, func(envelope facts.Envelope) bool {
		permission, err := decodeIAMEscalationAWSIAMPermission(envelope)
		if err != nil {
			return false
		}
		_, qualifies := iamEscalationIdentityPolicySources[permission.PolicySource]
		return qualifies
	})
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainIAMEscalationMaterialization,
		EntityKey:    "aws_resource_materialization:" + scopeID,
		Reason:       "aws iam identity statements observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
