// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package egress

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	collectorEgressModeRestricted = "restricted"
	collectorEgressModeBroad      = "broad"

	// CollectorActionAllow means scheduled collector work may be planned.
	CollectorActionAllow = "allow"
	// CollectorActionDeny means scheduled collector work must be skipped.
	CollectorActionDeny = "deny"

	// CollectorReasonAllowed reports an explicit or broad-mode allow.
	CollectorReasonAllowed = "egress_provider_allowed"
	// CollectorReasonDenied reports an explicit collector-kind denial.
	CollectorReasonDenied = "egress_provider_denied"
	// CollectorReasonMissing reports a missing restricted-mode allow rule.
	CollectorReasonMissing = "egress_policy_missing"
	// CollectorReasonNotConfigured reports local/no-policy no-op behavior.
	CollectorReasonNotConfigured = "egress_policy_not_configured"
)

// CollectorPolicy gates hosted active-mode collector scheduling before a
// claimable work row is created.
type CollectorPolicy struct {
	configured bool
	mode       string
	rules      []CollectorRule
}

// CollectorRule is one collector-kind allow or deny rule.
type CollectorRule struct {
	CollectorKind scope.CollectorKind
	Decision      string
}

// CollectorDecision is the allow/deny result for a collector kind.
type CollectorDecision struct {
	Action string
	Reason string
}

type collectorEgressPolicyConfig struct {
	Mode       string                      `json:"mode"`
	Collectors []collectorEgressRuleConfig `json:"collectors"`
}

type collectorEgressRuleConfig struct {
	CollectorKind string `json:"collector_kind"`
	Decision      string `json:"decision"`
}

// ParseCollectorPolicyJSON parses hosted collector egress policy JSON.
func ParseCollectorPolicyJSON(raw string) (CollectorPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CollectorPolicy{}, nil
	}
	var decoded collectorEgressPolicyConfig
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return CollectorPolicy{}, fmt.Errorf("parse collector egress policy: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(decoded.Mode))
	switch mode {
	case collectorEgressModeRestricted, collectorEgressModeBroad:
	default:
		return CollectorPolicy{}, fmt.Errorf("collector egress policy mode %q is not supported", decoded.Mode)
	}
	if mode == collectorEgressModeBroad && len(decoded.Collectors) > 0 {
		return CollectorPolicy{}, fmt.Errorf("broad collector egress policy must not include collector-specific rules")
	}
	rules := make([]CollectorRule, 0, len(decoded.Collectors))
	for index, candidate := range decoded.Collectors {
		kind := scope.CollectorKind(strings.TrimSpace(candidate.CollectorKind))
		if err := validateCollectorEgressKind(kind); err != nil {
			return CollectorPolicy{}, fmt.Errorf("collector egress policy collectors[%d]: %w", index, err)
		}
		decision := strings.ToLower(strings.TrimSpace(candidate.Decision))
		switch decision {
		case CollectorActionAllow, CollectorActionDeny:
		default:
			return CollectorPolicy{}, fmt.Errorf("collector egress policy collectors[%d] decision %q is not supported", index, candidate.Decision)
		}
		rules = append(rules, CollectorRule{
			CollectorKind: kind,
			Decision:      decision,
		})
	}
	return CollectorPolicy{
		configured: true,
		mode:       mode,
		rules:      rules,
	}, nil
}

// Decide returns the scheduling decision for collectorKind.
func (p CollectorPolicy) Decide(collectorKind scope.CollectorKind) CollectorDecision {
	if !p.configured {
		return CollectorDecision{
			Action: CollectorActionAllow,
			Reason: CollectorReasonNotConfigured,
		}
	}
	if p.mode == collectorEgressModeBroad {
		return CollectorDecision{
			Action: CollectorActionAllow,
			Reason: CollectorReasonAllowed,
		}
	}
	allowed := false
	for _, rule := range p.rules {
		if rule.CollectorKind != collectorKind {
			continue
		}
		if rule.Decision == CollectorActionDeny {
			return CollectorDecision{
				Action: CollectorActionDeny,
				Reason: CollectorReasonDenied,
			}
		}
		allowed = true
	}
	if allowed {
		return CollectorDecision{
			Action: CollectorActionAllow,
			Reason: CollectorReasonAllowed,
		}
	}
	return CollectorDecision{
		Action: CollectorActionDeny,
		Reason: CollectorReasonMissing,
	}
}

func validateCollectorEgressKind(kind scope.CollectorKind) error {
	instance := workflow.DesiredCollectorInstance{
		InstanceID:    "collector-egress-policy-validation",
		CollectorKind: kind,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       false,
	}
	if err := instance.Validate(); err != nil {
		return err
	}
	return nil
}
