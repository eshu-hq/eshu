// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildUsesProfileMaterializationReducerIntent enqueues one reducer intent that
// projects the scope generation's ec2_instance_posture instance_profile_arn fields
// into canonical USES_PROFILE graph edges (issue #1146 PR-B). The intent is
// anchored to the first posture fact that has a non-blank instance_profile_arn so
// the reducer claim is stable across reprojections of the same generation, and is
// only enqueued when at least one instance has an attached profile
// (no-profile-only generations enqueue nothing).
//
// Unlike the single-phase edge intents (S3 LOGS_TO, CAN_ASSUME) whose entity key
// is deliberately set to the node phase they gate on, the USES_PROFILE edge gates
// on TWO node phases published under DIFFERENT entity keys
// (ec2_instance_node_materialization:<scope> for the source instance node and
// aws_resource_materialization:<scope> for the target instance-profile node). A
// single entity key cannot match both, so the durable Postgres claim gate
// references those two entity-key prefixes directly. The edge intent therefore
// carries its OWN distinct entity key (ec2_uses_profile_materialization:<scope>)
// which keeps the edge's own conflict/readiness identity independent of either
// node domain.
func BuildUsesProfileMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKindMatching(facts.EC2InstancePostureFactKind, func(envelope facts.Envelope) bool {
		posture, err := decodeEC2InstancePosture(envelope)
		if err != nil {
			return false
		}
		return strings.TrimSpace(derefString(posture.InstanceProfileARN)) != ""
	})
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainEC2UsesProfileMaterialization,
		EntityKey:    "ec2_uses_profile_materialization:" + scopeID,
		Reason:       "ec2 instance profile usage observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

// derefString returns the value a *string points at, or "" when it is nil.
// Local per-package copy matching the repo convention of a small
// family-scoped deref helper (e.g. projector root's codegraphDerefString,
// ociDerefString, tfstateDerefString) rather than a shared one.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
