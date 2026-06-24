// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	extensionEgressModeRestricted = "restricted"
	extensionEgressModeBroad      = "broad"

	// ExtensionEgressActionAllow means component extension work may be planned.
	ExtensionEgressActionAllow = "allow"
	// ExtensionEgressActionDeny means component extension work must be skipped.
	ExtensionEgressActionDeny = "deny"

	// ExtensionEgressReasonAllowed reports an explicit or broad-mode allow.
	ExtensionEgressReasonAllowed = "egress_extension_allowed"
	// ExtensionEgressReasonDenied reports an explicit component extension denial.
	ExtensionEgressReasonDenied = "egress_extension_denied"
	// ExtensionEgressReasonMissing reports a missing restricted-mode allow rule.
	ExtensionEgressReasonMissing = "egress_policy_missing"
)

// ExtensionEgressPolicy gates hosted component extension scheduling before a
// claimable work row is created.
type ExtensionEgressPolicy struct {
	configured bool
	mode       string
	rules      []ExtensionEgressRule
}

// ExtensionEgressRule is one component extension allow or deny rule.
type ExtensionEgressRule struct {
	ComponentID   string
	InstanceID    string
	CollectorKind scope.CollectorKind
	Decision      string
}

// ExtensionEgressRequest identifies one component extension scheduling request.
type ExtensionEgressRequest struct {
	ComponentID   string
	InstanceID    string
	CollectorKind scope.CollectorKind
}

// ExtensionEgressDecision is the allow/deny result for a component extension.
type ExtensionEgressDecision struct {
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

// ParseExtensionEgressPolicyJSON parses hosted extension egress policy JSON.
func ParseExtensionEgressPolicyJSON(raw string) (ExtensionEgressPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ExtensionEgressPolicy{}, nil
	}
	var decoded extensionEgressPolicyConfig
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return ExtensionEgressPolicy{}, fmt.Errorf("parse extension egress policy: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(decoded.Mode))
	switch mode {
	case extensionEgressModeRestricted, extensionEgressModeBroad:
	default:
		return ExtensionEgressPolicy{}, fmt.Errorf("extension egress policy mode is not supported")
	}
	if mode == extensionEgressModeBroad && len(decoded.Extensions) > 0 {
		return ExtensionEgressPolicy{}, fmt.Errorf("broad extension egress policy must not include extension-specific rules")
	}
	rules := make([]ExtensionEgressRule, 0, len(decoded.Extensions))
	for index, candidate := range decoded.Extensions {
		rule, err := parseExtensionEgressRule(candidate)
		if err != nil {
			return ExtensionEgressPolicy{}, fmt.Errorf("extension egress policy extensions[%d]: %w", index, err)
		}
		rules = append(rules, rule)
	}
	return ExtensionEgressPolicy{
		configured: true,
		mode:       mode,
		rules:      rules,
	}, nil
}

// Decide returns the scheduling decision for a component extension request.
func (p ExtensionEgressPolicy) Decide(request ExtensionEgressRequest) ExtensionEgressDecision {
	if !p.configured {
		return ExtensionEgressDecision{
			Action: ExtensionEgressActionDeny,
			Reason: ExtensionEgressReasonMissing,
		}
	}
	if p.mode == extensionEgressModeBroad {
		return ExtensionEgressDecision{
			Action: ExtensionEgressActionAllow,
			Reason: ExtensionEgressReasonAllowed,
		}
	}
	allowed := false
	for _, rule := range p.rules {
		if !rule.matches(request) {
			continue
		}
		if rule.Decision == ExtensionEgressActionDeny {
			return ExtensionEgressDecision{
				Action: ExtensionEgressActionDeny,
				Reason: ExtensionEgressReasonDenied,
			}
		}
		allowed = true
	}
	if allowed {
		return ExtensionEgressDecision{
			Action: ExtensionEgressActionAllow,
			Reason: ExtensionEgressReasonAllowed,
		}
	}
	return ExtensionEgressDecision{
		Action: ExtensionEgressActionDeny,
		Reason: ExtensionEgressReasonMissing,
	}
}

func parseExtensionEgressRule(candidate extensionEgressRuleConfig) (ExtensionEgressRule, error) {
	componentID := strings.TrimSpace(candidate.ComponentID)
	if err := validateRequiredExtensionEgressIdentifier("component_id", componentID); err != nil {
		return ExtensionEgressRule{}, err
	}
	instanceID := strings.TrimSpace(candidate.InstanceID)
	if err := validateOptionalExtensionEgressIdentifier("instance_id", instanceID); err != nil {
		return ExtensionEgressRule{}, err
	}
	collectorKind := scope.CollectorKind(strings.TrimSpace(candidate.CollectorKind))
	if err := validateOptionalExtensionEgressIdentifier("collector_kind", string(collectorKind)); err != nil {
		return ExtensionEgressRule{}, err
	}
	decision := strings.ToLower(strings.TrimSpace(candidate.Decision))
	switch decision {
	case ExtensionEgressActionAllow, ExtensionEgressActionDeny:
	default:
		return ExtensionEgressRule{}, fmt.Errorf("decision is not supported")
	}
	return ExtensionEgressRule{
		ComponentID:   componentID,
		InstanceID:    instanceID,
		CollectorKind: collectorKind,
		Decision:      decision,
	}, nil
}

func (r ExtensionEgressRule) matches(request ExtensionEgressRequest) bool {
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
	return validateSafePlanKey(field, value)
}

func validateOptionalExtensionEgressIdentifier(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return validateSafePlanKey(field, value)
}
