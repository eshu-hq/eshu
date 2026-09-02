// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
)

// resolveIAMEscalationTarget reads the target identity for an armed primitive
// from the contributing statement's resources and resolves it against the scanned
// CloudResource join index. The resolution ladder is: exact ARN match -> single
// prefix/glob match -> wildcard/many (ambiguous) -> zero (unresolved). For the
// PassRole family the target comes from the iam:passrole statement's resources;
// otherwise from whichever single-action statement carried the primitive's action.
func resolveIAMEscalationTarget(
	index cloudResourceJoinIndex,
	grant iamPrincipalGrant,
	primitive iamEscalationPrimitive,
) (string, iamTargetStatus) {
	carrierAction := primitive.Actions[0]
	if primitive.PassRoleAction != "" {
		carrierAction = primitive.PassRoleAction
	}
	resources := collectTrustedResources(grant.StatementsCovering(carrierAction))
	if len(resources) == 0 {
		return "", iamTargetUnresolved
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
				if iamResourceTypeOfARN(arn) != expectedType {
					continue
				}
				if globMatch(pattern, arn) {
					matches[uid] = struct{}{}
				}
			}
			continue
		}
		// Exact ARN: must be a scanned node of the expected IAM type.
		if uid, ok := index.ByARN[pattern]; ok && iamResourceTypeOfARN(pattern) == expectedType {
			matches[uid] = struct{}{}
		}
	}

	switch {
	case len(matches) == 1:
		for uid := range matches {
			return uid, iamTargetResolved
		}
	case len(matches) > 1:
		return "", iamTargetAmbiguous
	case sawWildcard:
		// A bare "*" (or only-glob with no scanned match) names no single node.
		return "", iamTargetAmbiguous
	}
	return "", iamTargetUnresolved
}

// iamResourceTypeForTarget maps a primitive target kind to the IAM CloudResource
// resource_type the resolver requires the matched node to be, so a policy-target
// primitive never resolves to a role node that happens to share a glob.
func iamResourceTypeForTarget(kind iamEscalationTargetKind) string {
	switch kind {
	case iamEscalationTargetPolicy:
		return iamResourceTypePolicy
	case iamEscalationTargetUser:
		return iamResourceTypeUser
	case iamEscalationTargetGroup:
		return iamResourceTypeGroup
	default: // role and passed_role both resolve to an IAM role node.
		return iamResourceTypeRole
	}
}
