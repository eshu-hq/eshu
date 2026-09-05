// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamescalation

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"
)

// resolveIAMEscalationTarget reads the target identity for an armed primitive
// from the contributing statement's resources and resolves it against the scanned
// CloudResource join index. The resolution ladder is: exact ARN match -> single
// prefix/glob match -> wildcard/many (ambiguous) -> zero (unresolved). For the
// PassRole family the target comes from the iam:passrole statement's resources;
// otherwise from whichever single-action statement carried the primitive's action.
func resolveIAMEscalationTarget(
	index cloudjoin.CloudResourceJoinIndex,
	grant iampolicy.PrincipalGrant,
	primitive iamEscalationPrimitive,
) (string, iampolicy.TargetStatus) {
	carrierAction := primitive.Actions[0]
	if primitive.PassRoleAction != "" {
		carrierAction = primitive.PassRoleAction
	}
	resources := iampolicy.CollectTrustedResources(grant.StatementsCovering(carrierAction))
	if len(resources) == 0 {
		return "", iampolicy.TargetUnresolved
	}

	expectedType := iamResourceTypeForTarget(primitive.TargetKind)
	matches := make(map[string]struct{})
	sawWildcard := false
	for _, pattern := range resources {
		if pattern == "*" {
			sawWildcard = true
			continue
		}
		if strings.ContainsAny(pattern, "*?") {
			// A glob pattern: resolve by membership against scanned ARNs of the
			// expected IAM type. Many matches are ambiguous; exactly one is a confident
			// edge.
			for arn, uid := range index.ByARN {
				if iampolicy.ResourceTypeOfARN(arn) != expectedType {
					continue
				}
				if iampolicy.GlobMatch(pattern, arn) {
					matches[uid] = struct{}{}
				}
			}
			continue
		}
		// Exact ARN: must be a scanned node of the expected IAM type.
		if uid, ok := index.ByARN[pattern]; ok && iampolicy.ResourceTypeOfARN(pattern) == expectedType {
			matches[uid] = struct{}{}
		}
	}

	switch {
	case len(matches) == 1:
		for uid := range matches {
			return uid, iampolicy.TargetResolved
		}
	case len(matches) > 1:
		return "", iampolicy.TargetAmbiguous
	case sawWildcard:
		// A bare "*" (or only-glob with no scanned match) names no single node.
		return "", iampolicy.TargetAmbiguous
	}
	return "", iampolicy.TargetUnresolved
}

// iamResourceTypeForTarget maps a primitive target kind to the IAM CloudResource
// resource_type the resolver requires the matched node to be, so a policy-target
// primitive never resolves to a role node that happens to share a glob.
func iamResourceTypeForTarget(kind iamEscalationTargetKind) string {
	switch kind {
	case iamEscalationTargetPolicy:
		return iampolicy.ResourceTypePolicy
	case iamEscalationTargetUser:
		return iampolicy.ResourceTypeUser
	case iamEscalationTargetGroup:
		return iampolicy.ResourceTypeGroup
	default: // role and passed_role both resolve to an IAM role node.
		return iampolicy.ResourceTypeRole
	}
}
