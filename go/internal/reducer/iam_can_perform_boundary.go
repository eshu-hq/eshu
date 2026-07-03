// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

type iamCanPerformBoundaryEvidence struct {
	policyARNs map[string]struct{}
	// statementsByPolicyARN maps a boundary policy ARN to the decoded
	// aws_iam_permission statements that make it up. The boundary policy ARNs
	// themselves come from the out-of-scope aws_iam_permission_boundary kind
	// (still read raw); the statements are the in-scope aws_iam_permission kind,
	// decoded through the seam.
	statementsByPolicyARN map[string][]iamv1.Permission
}

type iamCanPerformBoundaryDecision struct {
	evaluated  bool
	skipReason string
}

func groupIAMCanPerformBoundaryEvidence(
	index cloudResourceJoinIndex,
	envelopes []facts.Envelope,
) (map[string]iamCanPerformBoundaryEvidence, error) {
	byPrincipalUID := make(map[string]iamCanPerformBoundaryEvidence)
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		switch env.FactKind {
		case facts.AWSIAMPermissionBoundaryFactKind:
			// aws_iam_permission_boundary is a secrets/IAM-family kind outside this
			// issue's scope; it is read raw for its principal/boundary-policy ARNs.
			principalARN := payloadString(env.Payload, "principal_arn")
			policyARN := payloadString(env.Payload, "boundary_policy_arn")
			principalUID, ok := index.byARN[principalARN]
			if !ok || policyARN == "" {
				continue
			}
			evidence := ensureIAMCanPerformBoundaryEvidence(byPrincipalUID, principalUID)
			evidence.policyARNs[policyARN] = struct{}{}
		case facts.AWSIAMPermissionFactKind:
			permission, err := decodeAWSIAMPermission(env)
			if err != nil {
				return nil, err
			}
			if permission.PolicySource != iamCanPerformPolicySourcePermissionBoundary {
				continue
			}
			policyARN := derefString(permission.PolicyARN)
			principalUID, ok := index.byARN[permission.PrincipalARN]
			if !ok || policyARN == "" {
				continue
			}
			evidence := ensureIAMCanPerformBoundaryEvidence(byPrincipalUID, principalUID)
			evidence.statementsByPolicyARN[policyARN] = append(evidence.statementsByPolicyARN[policyARN], permission)
		}
	}
	return byPrincipalUID, nil
}

func ensureIAMCanPerformBoundaryEvidence(
	byPrincipalUID map[string]iamCanPerformBoundaryEvidence,
	principalUID string,
) iamCanPerformBoundaryEvidence {
	evidence := byPrincipalUID[principalUID]
	if evidence.policyARNs == nil {
		evidence.policyARNs = make(map[string]struct{})
		evidence.statementsByPolicyARN = make(map[string][]iamv1.Permission)
		byPrincipalUID[principalUID] = evidence
	}
	return evidence
}

func evaluateIAMCanPerformPermissionBoundary(
	evidence iamCanPerformBoundaryEvidence,
	action string,
	resourceARN string,
	resourceType string,
) iamCanPerformBoundaryDecision {
	if len(evidence.policyARNs) == 0 {
		return iamCanPerformBoundaryDecision{}
	}
	for _, policyARN := range sortedCanPerformStringSet(evidence.policyARNs) {
		statements := evidence.statementsByPolicyARN[policyARN]
		if len(statements) == 0 {
			return iamCanPerformBoundaryDecision{evaluated: true, skipReason: iamCanPerformSkipUnresolved}
		}
		decision := evaluateIAMCanPerformBoundaryPolicy(statements, action, resourceARN, resourceType)
		if decision.skipReason != "" {
			return decision
		}
	}
	return iamCanPerformBoundaryDecision{evaluated: true}
}

func evaluateIAMCanPerformBoundaryPolicy(
	statements []iamv1.Permission,
	action string,
	resourceARN string,
	resourceType string,
) iamCanPerformBoundaryDecision {
	sawAllow := false
	sawConditionedDeny := false
	sawConditionedAllow := false
	sawNotActionResourceDeny := false
	sawNotActionResourceAllow := false

	for _, permission := range statements {
		effect := permission.Effect
		if effect != "Allow" && effect != "Deny" {
			continue
		}
		actions := permission.Actions
		hasNotActions := len(permission.NotActions) > 0
		hasNotResources := len(permission.NotResources) > 0
		actionTouched := allowStatementTouches(actions, action) || hasNotActions
		if !actionTouched {
			continue
		}
		if hasNotActions || hasNotResources {
			if effect == "Deny" {
				sawNotActionResourceDeny = true
			} else {
				sawNotActionResourceAllow = true
			}
			continue
		}
		if !iamCanPerformBoundaryCoversResource(permission.Resources, resourceARN, resourceType) {
			continue
		}
		if boolPtrValue(permission.HasConditions) {
			if effect == "Deny" {
				sawConditionedDeny = true
			} else {
				sawConditionedAllow = true
			}
			continue
		}
		if effect == "Deny" {
			return iamCanPerformBoundaryDecision{evaluated: true, skipReason: iamCanPerformSkipDeny}
		}
		sawAllow = true
	}

	switch {
	case sawConditionedDeny:
		return iamCanPerformBoundaryDecision{evaluated: true, skipReason: iamCanPerformSkipConditioned}
	case sawNotActionResourceDeny:
		return iamCanPerformBoundaryDecision{evaluated: true, skipReason: iamCanPerformSkipNotActionResource}
	case sawAllow:
		return iamCanPerformBoundaryDecision{evaluated: true}
	case sawConditionedAllow:
		return iamCanPerformBoundaryDecision{evaluated: true, skipReason: iamCanPerformSkipConditioned}
	case sawNotActionResourceAllow:
		return iamCanPerformBoundaryDecision{evaluated: true, skipReason: iamCanPerformSkipNotActionResource}
	default:
		return iamCanPerformBoundaryDecision{evaluated: true, skipReason: iamCanPerformSkipPermissionBoundary}
	}
}

func iamCanPerformBoundaryCoversResource(patterns []string, resourceARN string, _ string) bool {
	if resourceARN == "" || len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		switch {
		case pattern == "*":
			return true
		case pattern == resourceARN:
			return true
		case strings.HasPrefix(pattern, resourceARN+"/"):
			return true
		case globMatch(pattern, resourceARN):
			return true
		}
	}
	return false
}
