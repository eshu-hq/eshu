// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package egress

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	extensionEgressModeRestricted = "restricted"
	extensionEgressModeBroad      = "broad"

	// ExtensionActionAllow means component extension work may be planned.
	ExtensionActionAllow = "allow"
	// ExtensionActionDeny means component extension work must be skipped.
	ExtensionActionDeny = "deny"

	// ExtensionReasonAllowed reports an explicit or broad-mode allow.
	ExtensionReasonAllowed = "egress_extension_allowed"
	// ExtensionReasonDenied reports an explicit component extension denial.
	ExtensionReasonDenied = "egress_extension_denied"
	// ExtensionReasonMissing reports a missing restricted-mode allow rule.
	ExtensionReasonMissing = "egress_policy_missing"
)

// ExtensionPolicy gates hosted component extension scheduling before a
// claimable work row is created.
type ExtensionPolicy struct {
	configured bool
	mode       string
	rules      []ExtensionRule
}

// ExtensionRule is one component extension allow or deny rule.
type ExtensionRule struct {
	ComponentID   string
	InstanceID    string
	CollectorKind scope.CollectorKind
	Decision      string
}

// ExtensionRequest identifies one component extension scheduling request.
type ExtensionRequest struct {
	ComponentID   string
	InstanceID    string
	CollectorKind scope.CollectorKind
}

// ExtensionDecision is the allow/deny result for a component extension.
type ExtensionDecision struct {
	Action string
	Reason string
}

type extensionEgressPolicyConfig struct {
	Mode       string                      `json:"mode"`
	Extensions []extensionEgressRuleConfig `json:"extensions"`
}

type extensionEgressRuleConfig struct {
	ComponentID   string `json:"component_id"`
	InstanceID    string `json:"instance_id"`
	CollectorKind string `json:"collector_kind"`
	Decision      string `json:"decision"`
}

// ParseExtensionPolicyJSON parses hosted extension egress policy JSON.
func ParseExtensionPolicyJSON(raw string) (ExtensionPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ExtensionPolicy{}, nil
	}
	var decoded extensionEgressPolicyConfig
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return ExtensionPolicy{}, fmt.Errorf("parse extension egress policy: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(decoded.Mode))
	switch mode {
	case extensionEgressModeRestricted, extensionEgressModeBroad:
	default:
		return ExtensionPolicy{}, fmt.Errorf("extension egress policy mode is not supported")
	}
	if mode == extensionEgressModeBroad && len(decoded.Extensions) > 0 {
		return ExtensionPolicy{}, fmt.Errorf("broad extension egress policy must not include extension-specific rules")
	}
	rules := make([]ExtensionRule, 0, len(decoded.Extensions))
	for index, candidate := range decoded.Extensions {
		rule, err := parseExtensionEgressRule(candidate)
		if err != nil {
			return ExtensionPolicy{}, fmt.Errorf("extension egress policy extensions[%d]: %w", index, err)
		}
		rules = append(rules, rule)
	}
	return ExtensionPolicy{
		configured: true,
		mode:       mode,
		rules:      rules,
	}, nil
}

// Decide returns the scheduling decision for a component extension request.
func (p ExtensionPolicy) Decide(request ExtensionRequest) ExtensionDecision {
	if !p.configured {
		return ExtensionDecision{
			Action: ExtensionActionDeny,
			Reason: ExtensionReasonMissing,
		}
	}
	if p.mode == extensionEgressModeBroad {
		return ExtensionDecision{
			Action: ExtensionActionAllow,
			Reason: ExtensionReasonAllowed,
		}
	}
	allowed := false
	for _, rule := range p.rules {
		if !rule.matches(request) {
			continue
		}
		if rule.Decision == ExtensionActionDeny {
			return ExtensionDecision{
				Action: ExtensionActionDeny,
				Reason: ExtensionReasonDenied,
			}
		}
		allowed = true
	}
	if allowed {
		return ExtensionDecision{
			Action: ExtensionActionAllow,
			Reason: ExtensionReasonAllowed,
		}
	}
	return ExtensionDecision{
		Action: ExtensionActionDeny,
		Reason: ExtensionReasonMissing,
	}
}

func parseExtensionEgressRule(candidate extensionEgressRuleConfig) (ExtensionRule, error) {
	componentID := strings.TrimSpace(candidate.ComponentID)
	if err := validateRequiredExtensionEgressIdentifier("component_id", componentID); err != nil {
		return ExtensionRule{}, err
	}
	instanceID := strings.TrimSpace(candidate.InstanceID)
	if err := validateOptionalExtensionEgressIdentifier("instance_id", instanceID); err != nil {
		return ExtensionRule{}, err
	}
	collectorKind := scope.CollectorKind(strings.TrimSpace(candidate.CollectorKind))
	if err := validateOptionalExtensionEgressIdentifier("collector_kind", string(collectorKind)); err != nil {
		return ExtensionRule{}, err
	}
	decision := strings.ToLower(strings.TrimSpace(candidate.Decision))
	switch decision {
	case ExtensionActionAllow, ExtensionActionDeny:
	default:
		return ExtensionRule{}, fmt.Errorf("decision is not supported")
	}
	return ExtensionRule{
		ComponentID:   componentID,
		InstanceID:    instanceID,
		CollectorKind: collectorKind,
		Decision:      decision,
	}, nil
}

func (r ExtensionRule) matches(request ExtensionRequest) bool {
	if r.ComponentID != strings.TrimSpace(request.ComponentID) {
		return false
	}
	if r.InstanceID != "" && r.InstanceID != strings.TrimSpace(request.InstanceID) {
		return false
	}
	if r.CollectorKind != "" && r.CollectorKind != scope.CollectorKind(strings.TrimSpace(string(request.CollectorKind))) {
		return false
	}
	return true
}

func validateRequiredExtensionEgressIdentifier(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be blank", field)
	}
	return contract.ValidateSafePlanKey(field, value)
}

func validateOptionalExtensionEgressIdentifier(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return contract.ValidateSafePlanKey(field, value)
}
