// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamTargetStatus is the resolution outcome for an armed primitive's target.
type iamTargetStatus int

const (
	// iamTargetResolved means the target identity matched exactly one scanned node.
	iamTargetResolved iamTargetStatus = iota
	// iamTargetAmbiguous means the resource pattern was a wildcard or matched many
	// scanned nodes; conservatively not promoted to an edge.
	iamTargetAmbiguous
	// iamTargetUnresolved means the target ARN matched zero scanned nodes
	// (including a cross-account ARN whose account was not scanned).
	iamTargetUnresolved
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
	resources := collectTrustedResources(grant.statementsCovering(carrierAction))
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
			for arn, uid := range index.byARN {
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
		if uid, ok := index.byARN[pattern]; ok && iamResourceTypeOfARN(pattern) == expectedType {
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

// collectTrustedResources unions the resources of the statements that carried a
// primitive's action, preserving the verbatim case-sensitive ARN patterns.
func collectTrustedResources(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, env := range envelopes {
		for _, resource := range payloadStringSlice(env.Payload, "resources") {
			if _, ok := seen[resource]; ok {
				continue
			}
			seen[resource] = struct{}{}
			out = append(out, resource)
		}
	}
	sort.Strings(out)
	return out
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

// iamResourceTypeOfARN classifies an IAM ARN's resource segment to the matching
// resource_type token, so target resolution can require the resolved node be the
// right IAM family. Returns "" for a non-IAM or unrecognized ARN.
func iamResourceTypeOfARN(arn string) string {
	// arn:partition:iam::account:resource-type/path...
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[2] != "iam" {
		return ""
	}
	resource := parts[5]
	switch {
	case strings.HasPrefix(resource, "policy/"):
		return iamResourceTypePolicy
	case strings.HasPrefix(resource, "user/"):
		return iamResourceTypeUser
	case strings.HasPrefix(resource, "role/"):
		return iamResourceTypeRole
	case strings.HasPrefix(resource, "group/"):
		return iamResourceTypeGroup
	default:
		return ""
	}
}

// globMatch is a small iterative wildcard matcher (no regexp compile per call) for
// IAM "*"/"?" resource patterns. "*" matches any run (including "/"), "?" matches
// one character. It avoids the catastrophic backtracking of a naive recursive
// matcher by tracking the last "*" position. The caller still requires the
// resolved node be a scanned IAM node of the expected type, so a greedy
// single-segment over-match cannot fabricate a cross-type edge.
func globMatch(pattern, value string) bool {
	var (
		p, v       int
		star       = -1
		starV      int
		plen, vlen = len(pattern), len(value)
	)
	for v < vlen {
		switch {
		case p < plen && (pattern[p] == value[v] || pattern[p] == '?'):
			p++
			v++
		case p < plen && pattern[p] == '*':
			star = p
			starV = v
			p++
		case star != -1:
			p = star + 1
			starV++
			v = starV
		default:
			return false
		}
	}
	for p < plen && pattern[p] == '*' {
		p++
	}
	return p == plen
}
