// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type iamCanPerformBoundaryEvidence struct {
	policyARNs            map[string]struct{}
	statementsByPolicyARN map[string][]facts.Envelope
}

type iamCanPerformBoundaryDecision struct {
	evaluated  bool
	skipReason string
}

func groupIAMCanPerformBoundaryEvidence(
	index cloudResourceJoinIndex,
	envelopes []facts.Envelope,
) map[string]iamCanPerformBoundaryEvidence {
	byPrincipalUID := make(map[string]iamCanPerformBoundaryEvidence)
	for _, env := range envelopes {
		if env.IsTombstone {
			continue
		}
		switch env.FactKind {
		case facts.AWSIAMPermissionBoundaryFactKind:
			principalARN := payloadString(env.Payload, "principal_arn")
			policyARN := payloadString(env.Payload, "boundary_policy_arn")
			principalUID, ok := index.byARN[principalARN]
			if !ok || policyARN == "" {
				continue
			}
			evidence := ensureIAMCanPerformBoundaryEvidence(byPrincipalUID, principalUID)
			evidence.policyARNs[policyARN] = struct{}{}
		case facts.AWSIAMPermissionFactKind:
			if payloadString(env.Payload, "policy_source") != iamCanPerformPolicySourcePermissionBoundary {
				continue
			}
			principalARN := payloadString(env.Payload, "principal_arn")
			policyARN := payloadString(env.Payload, "policy_arn")
			principalUID, ok := index.byARN[principalARN]
			if !ok || policyARN == "" {
				continue
			}
			evidence := ensureIAMCanPerformBoundaryEvidence(byPrincipalUID, principalUID)
			evidence.statementsByPolicyARN[policyARN] = append(evidence.statementsByPolicyARN[policyARN], env)
		}
	}
	return byPrincipalUID
}

func ensureIAMCanPerformBoundaryEvidence(
	byPrincipalUID map[string]iamCanPerformBoundaryEvidence,
	principalUID string,
) iamCanPerformBoundaryEvidence {
	evidence := byPrincipalUID[principalUID]
	if evidence.policyARNs == nil {
		evidence.policyARNs = make(map[string]struct{})
		evidence.statementsByPolicyARN = make(map[string][]facts.Envelope)
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
	statements []facts.Envelope,
	action string,
	resourceARN string,
	resourceType string,
) iamCanPerformBoundaryDecision {
	sawAllow := false
	sawConditionedDeny := false
	sawConditionedAllow := false
	sawNotActionResourceDeny := false
	sawNotActionResourceAllow := false

	for _, env := range statements {
		if env.FactKind != facts.AWSIAMPermissionFactKind || env.IsTombstone {
			continue
		}
		effect := payloadString(env.Payload, "effect")
		if effect != "Allow" && effect != "Deny" {
			continue
		}
		actions := payloadStringSlice(env.Payload, "actions")
		hasNotActions := len(payloadStringSlice(env.Payload, "not_actions")) > 0
		hasNotResources := len(payloadStringSlice(env.Payload, "not_resources")) > 0
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
		if !iamCanPerformBoundaryCoversResource(payloadStringSlice(env.Payload, "resources"), resourceARN, resourceType) {
			continue
		}
		if payloadBool(env.Payload, "has_conditions") {
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
