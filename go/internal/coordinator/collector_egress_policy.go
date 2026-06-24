// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

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

	// CollectorEgressActionAllow means scheduled collector work may be planned.
	CollectorEgressActionAllow = "allow"
	// CollectorEgressActionDeny means scheduled collector work must be skipped.
	CollectorEgressActionDeny = "deny"

	// CollectorEgressReasonAllowed reports an explicit or broad-mode allow.
	CollectorEgressReasonAllowed = "egress_provider_allowed"
	// CollectorEgressReasonDenied reports an explicit collector-kind denial.
	CollectorEgressReasonDenied = "egress_provider_denied"
	// CollectorEgressReasonMissing reports a missing restricted-mode allow rule.
	CollectorEgressReasonMissing = "egress_policy_missing"
	// CollectorEgressReasonNotConfigured reports local/no-policy no-op behavior.
	CollectorEgressReasonNotConfigured = "egress_policy_not_configured"
)

// CollectorEgressPolicy gates hosted active-mode collector scheduling before a
// claimable work row is created.
type CollectorEgressPolicy struct {
	configured bool
	mode       string
	rules      []CollectorEgressRule
}

// CollectorEgressRule is one collector-kind allow or deny rule.
type CollectorEgressRule struct {
	CollectorKind scope.CollectorKind
	Decision      string
}

// CollectorEgressDecision is the allow/deny result for a collector kind.
type CollectorEgressDecision struct {
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

// ParseCollectorEgressPolicyJSON parses hosted collector egress policy JSON.
func ParseCollectorEgressPolicyJSON(raw string) (CollectorEgressPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CollectorEgressPolicy{}, nil
	}
	var decoded collectorEgressPolicyConfig
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return CollectorEgressPolicy{}, fmt.Errorf("parse collector egress policy: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(decoded.Mode))
	switch mode {
	case collectorEgressModeRestricted, collectorEgressModeBroad:
	default:
		return CollectorEgressPolicy{}, fmt.Errorf("collector egress policy mode %q is not supported", decoded.Mode)
	}
	if mode == collectorEgressModeBroad && len(decoded.Collectors) > 0 {
		return CollectorEgressPolicy{}, fmt.Errorf("broad collector egress policy must not include collector-specific rules")
	}
	rules := make([]CollectorEgressRule, 0, len(decoded.Collectors))
	for index, candidate := range decoded.Collectors {
		kind := scope.CollectorKind(strings.TrimSpace(candidate.CollectorKind))
		if err := validateCollectorEgressKind(kind); err != nil {
			return CollectorEgressPolicy{}, fmt.Errorf("collector egress policy collectors[%d]: %w", index, err)
		}
		decision := strings.ToLower(strings.TrimSpace(candidate.Decision))
		switch decision {
		case CollectorEgressActionAllow, CollectorEgressActionDeny:
		default:
			return CollectorEgressPolicy{}, fmt.Errorf("collector egress policy collectors[%d] decision %q is not supported", index, candidate.Decision)
		}
		rules = append(rules, CollectorEgressRule{
			CollectorKind: kind,
			Decision:      decision,
		})
	}
	return CollectorEgressPolicy{
		configured: true,
		mode:       mode,
		rules:      rules,
	}, nil
}

// Decide returns the scheduling decision for collectorKind.
func (p CollectorEgressPolicy) Decide(collectorKind scope.CollectorKind) CollectorEgressDecision {
	if !p.configured {
		return CollectorEgressDecision{
			Action: CollectorEgressActionAllow,
			Reason: CollectorEgressReasonNotConfigured,
		}
	}
	if p.mode == collectorEgressModeBroad {
		return CollectorEgressDecision{
			Action: CollectorEgressActionAllow,
			Reason: CollectorEgressReasonAllowed,
		}
	}
	allowed := false
	for _, rule := range p.rules {
		if rule.CollectorKind != collectorKind {
			continue
		}
		if rule.Decision == CollectorEgressActionDeny {
			return CollectorEgressDecision{
				Action: CollectorEgressActionDeny,
				Reason: CollectorEgressReasonDenied,
			}
		}
		allowed = true
	}
	if allowed {
		return CollectorEgressDecision{
			Action: CollectorEgressActionAllow,
			Reason: CollectorEgressReasonAllowed,
		}
	}
	return CollectorEgressDecision{
		Action: CollectorEgressActionDeny,
		Reason: CollectorEgressReasonMissing,
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
