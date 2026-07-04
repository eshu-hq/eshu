// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeComputeSecurityPolicy (compute.googleapis.com/SecurityPolicy) is
// declared in extractor_backend_service.go — the Backend Service extractor's
// backend_service_uses_security_policy and
// backend_service_uses_edge_security_policy edges already resolve toward this
// asset type as their target — and reused here; this file is the SecurityPolicy
// resource's own typed depth, never redeclaring the constant.

func init() {
	RegisterAssetExtractor(assetTypeComputeSecurityPolicy, extractSecurityPolicy)
}

// securityPolicyRuleData is the bounded view of one CAI SecurityPolicy rule
// entry (Compute API schema SecurityPolicyRule). Only Priority and Action are
// decoded: Description is free-text operator commentary, Match/NetworkMatch
// carry raw match expressions and CIDR/IP-range values, RateLimitOptions and
// RedirectOptions carry threshold/target configuration, and Preview/HeaderAction
// carry no Terraform/drift/monitoring value at this typed-depth boundary. None
// of those fields declare a struct tag here, so encoding/json never decodes them
// into Go memory in the first place — mirroring the Router extractor's
// omission of BGP peer/NAT IP fields.
type securityPolicyRuleData struct {
	Priority int32  `json:"priority"`
	Action   string `json:"action"`
}

// securityPolicyLayer7DdosDefenseConfigData is the bounded view of the nested
// Cloud Armor Adaptive Protection layer-7 DDoS defense config. RuleVisibility
// and ThresholdConfigs carry no typed-depth value beyond the enable posture and
// are not decoded.
type securityPolicyLayer7DdosDefenseConfigData struct {
	Enable *bool `json:"enable"`
}

// securityPolicyAdaptiveProtectionConfigData is the bounded view of the CAI
// SecurityPolicy adaptiveProtectionConfig object.
type securityPolicyAdaptiveProtectionConfigData struct {
	Layer7DdosDefenseConfig *securityPolicyLayer7DdosDefenseConfigData `json:"layer7DdosDefenseConfig"`
}

// securityPolicyData is the bounded view of a CAI
// compute.googleapis.com/SecurityPolicy resource.data blob (Compute API schema
// SecurityPolicy). Region is present only for a regional security policy; a
// global security policy reports no region field. Description, Labels,
// UserDefinedFields (CLOUD_ARMOR_NETWORK packet field definitions),
// AdvancedOptionsConfig, DdosProtectionConfig, RecaptchaOptionsConfig,
// Associations, Fingerprint, and LabelFingerprint carry either free-text
// operator commentary, no Terraform/drift/monitoring value at this typed-depth
// boundary, or an opaque optimistic-locking token, and are not decoded here;
// resource labels are already captured by the shared envelope label path (see
// envelope.go), not re-declared as a typed attribute.
type securityPolicyData struct {
	Type                     string                                      `json:"type"`
	Region                   string                                      `json:"region"`
	CreationTimestamp        string                                      `json:"creationTimestamp"`
	AdaptiveProtectionConfig *securityPolicyAdaptiveProtectionConfigData `json:"adaptiveProtectionConfig"`
	Rules                    []securityPolicyRuleData                    `json:"rules"`
}

// extractSecurityPolicy extracts bounded, redaction-safe typed depth for one
// CAI Cloud Armor SecurityPolicy asset. It surfaces the Terraform/drift/
// monitoring attribute set: policy type (CLOUD_ARMOR, CLOUD_ARMOR_EDGE, or
// CLOUD_ARMOR_NETWORK per the Compute SecurityPolicy schema), region (present
// only for a regional policy), a bounded per-rule priority/action summary and
// rule count, the Adaptive Protection (Cloud Armor layer-7 DDoS defense)
// enabled posture, and creation time.
//
// The policy's graph edge is inbound: a BackendService references it through
// its own securityPolicy/edgeSecurityPolicy fields and resolves the edge from
// that side (extractor_backend_service.go), the same inbound-only edge shape
// as the Custom IAM Role and SSL Certificate extractors. This extractor
// therefore derives no outbound relationships or anchors from the resource's
// own data.
//
// No rule match expression, network-match packet field, rate-limit threshold,
// redirect target, description, or IP/CIDR value ever reaches the output —
// only the rule's priority and action string, both small Google-controlled
// vocabulary values, never user-supplied match data.
func extractSecurityPolicy(ctx ExtractContext) (AttributeExtraction, error) {
	var data securityPolicyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode security policy data: %w", err)
	}

	attrs := securityPolicyAttributes(data)

	return AttributeExtraction{Attributes: attrs}, nil
}

// securityPolicyAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture (for example a false
// adaptive_protection_enabled when the config block is simply absent).
func securityPolicyAttributes(data securityPolicyData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Type); v != "" {
		attrs["type"] = v
	}
	if v := computeRegionName(data.Region); v != "" {
		attrs["region"] = v
	}
	if rules := securityPolicyRuleSummaries(data.Rules); len(rules) > 0 {
		attrs["rules"] = rules
		attrs["rule_count"] = len(rules)
	}
	if enable := securityPolicyAdaptiveProtectionEnabled(data.AdaptiveProtectionConfig); enable != nil {
		attrs["adaptive_protection_enabled"] = *enable
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// securityPolicyRuleSummaries builds the bounded per-rule summary list:
// priority and action only. securityPolicyRuleData never decodes a rule's
// match condition, description, or rate-limit/redirect configuration in the
// first place, so none of those values ever reach the summary.
func securityPolicyRuleSummaries(rules []securityPolicyRuleData) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	summaries := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		summary := map[string]any{}
		// Priority is always emitted, including the highest-priority value 0: the
		// Compute SecurityPolicyRule schema defines priority as "a positive value
		// between 0 and 2147483647" (0 is highest priority, not an absent-field
		// sentinel), so gating on a non-zero value would silently drop the
		// priority of the single most urgent rule in a policy.
		summary["priority"] = rule.Priority
		if v := strings.TrimSpace(rule.Action); v != "" {
			summary["action"] = v
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

// securityPolicyAdaptiveProtectionEnabled reads the Cloud Armor Adaptive
// Protection layer-7 DDoS defense enable posture. It returns nil when the
// config block or its nested layer7DdosDefenseConfig.enable field is absent,
// distinguishing "not configured" from an explicit false, mirroring the
// Router extractor's EncryptedInterconnectRouter pointer handling.
func securityPolicyAdaptiveProtectionEnabled(config *securityPolicyAdaptiveProtectionConfigData) *bool {
	if config == nil || config.Layer7DdosDefenseConfig == nil {
		return nil
	}
	return config.Layer7DdosDefenseConfig.Enable
}
