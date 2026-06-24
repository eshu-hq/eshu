// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkfirewall

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Network Firewall metadata facts for one claimed account and
// region. It never persists rule group rule sources (Suricata signature
// bodies), TLS inspection certificate bodies, or any firewall policy rule body,
// and it never calls any Network Firewall mutation API.
type Scanner struct {
	Client Client
}

// Scan observes Network Firewall firewalls, firewall policies, rule groups, and
// TLS inspection configurations through the configured client and emits
// resource and relationship facts for one regional boundary.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("networkfirewall scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceNetworkFirewall:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceNetworkFirewall
	default:
		return nil, fmt.Errorf("networkfirewall scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope
	firewallEnvelopes, err := s.scanFirewalls(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, firewallEnvelopes...)

	policyEnvelopes, err := s.scanFirewallPolicies(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, policyEnvelopes...)

	ruleGroupEnvelopes, err := s.scanRuleGroups(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, ruleGroupEnvelopes...)

	tlsEnvelopes, err := s.scanTLSInspectionConfigurations(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, tlsEnvelopes...)
	return envelopes, nil
}

func (s Scanner) scanFirewalls(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	firewalls, err := s.Client.ListFirewalls(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Network Firewall firewalls: %w", err)
	}
	var envelopes []facts.Envelope
	for _, firewall := range firewalls {
		resource, err := awscloud.NewResourceEnvelope(firewallObservation(boundary, firewall))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		relationships, err := relationshipEnvelopes(firewallRelationships(boundary, firewall))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	return envelopes, nil
}

func (s Scanner) scanFirewallPolicies(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	policies, err := s.Client.ListFirewallPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Network Firewall firewall policies: %w", err)
	}
	var envelopes []facts.Envelope
	for _, policy := range policies {
		resource, err := awscloud.NewResourceEnvelope(policyObservation(boundary, policy))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		relationships, err := relationshipEnvelopes(policyRelationships(boundary, policy))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationships...)
	}
	return envelopes, nil
}

func (s Scanner) scanRuleGroups(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	ruleGroups, err := s.Client.ListRuleGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Network Firewall rule groups: %w", err)
	}
	var envelopes []facts.Envelope
	for _, ruleGroup := range ruleGroups {
		resource, err := awscloud.NewResourceEnvelope(ruleGroupObservation(boundary, ruleGroup))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func (s Scanner) scanTLSInspectionConfigurations(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	tlsConfigs, err := s.Client.ListTLSInspectionConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Network Firewall TLS inspection configurations: %w", err)
	}
	var envelopes []facts.Envelope
	for _, tlsConfig := range tlsConfigs {
		resource, err := awscloud.NewResourceEnvelope(tlsInspectionObservation(boundary, tlsConfig))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func firewallObservation(boundary awscloud.Boundary, firewall Firewall) awscloud.ResourceObservation {
	arn := strings.TrimSpace(firewall.ARN)
	resourceID := firstNonEmpty(arn, firewall.ID, firewall.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeNetworkFirewallFirewall,
		Name:         strings.TrimSpace(firewall.Name),
		State:        strings.TrimSpace(firewall.Status),
		Tags:         cloneStringMap(firewall.Tags),
		Attributes: map[string]any{
			"id":                                strings.TrimSpace(firewall.ID),
			"description":                       strings.TrimSpace(firewall.Description),
			"vpc_id":                            strings.TrimSpace(firewall.VPCID),
			"firewall_policy_arn":               strings.TrimSpace(firewall.FirewallPolicyARN),
			"status":                            strings.TrimSpace(firewall.Status),
			"configuration_sync_state":          strings.TrimSpace(firewall.ConfigurationSyncState),
			"delete_protection":                 firewall.DeleteProtection,
			"subnet_change_protection":          firewall.SubnetChangeProtection,
			"firewall_policy_change_protection": firewall.FirewallPolicyChangeProtection,
			"subnet_count":                      len(dedupeStrings(firewall.SubnetIDs)),
			"number_of_associations":            firewall.NumberOfAssociations,
		},
		CorrelationAnchors: []string{arn, firewall.ID, firewall.Name},
		SourceRecordID:     resourceID,
	}
}

func policyObservation(boundary awscloud.Boundary, policy FirewallPolicy) awscloud.ResourceObservation {
	arn := strings.TrimSpace(policy.ARN)
	resourceID := firstNonEmpty(arn, policy.ID, policy.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeNetworkFirewallPolicy,
		Name:         strings.TrimSpace(policy.Name),
		State:        strings.TrimSpace(policy.Status),
		Tags:         cloneStringMap(policy.Tags),
		Attributes: map[string]any{
			"id":                                 strings.TrimSpace(policy.ID),
			"description":                        strings.TrimSpace(policy.Description),
			"status":                             strings.TrimSpace(policy.Status),
			"stateless_default_actions":          dedupeStrings(policy.StatelessDefaultActions),
			"stateless_fragment_default_actions": dedupeStrings(policy.StatelessFragmentDefaultActions),
			"stateful_default_actions":           dedupeStrings(policy.StatefulDefaultActions),
			"rule_group_ref_count":               len(dedupeStrings(policy.RuleGroupARNs)),
			"tls_inspection_configuration_arn":   strings.TrimSpace(policy.TLSInspectionConfigurationARN),
			"number_of_associations":             policy.NumberOfAssociations,
			"consumed_stateful_rule_capacity":    policy.ConsumedStatefulRuleCapacity,
			"consumed_stateless_rule_capacity":   policy.ConsumedStatelessRuleCapacity,
		},
		CorrelationAnchors: []string{arn, policy.ID, policy.Name},
		SourceRecordID:     resourceID,
	}
}

func ruleGroupObservation(boundary awscloud.Boundary, ruleGroup RuleGroup) awscloud.ResourceObservation {
	arn := strings.TrimSpace(ruleGroup.ARN)
	resourceID := firstNonEmpty(arn, ruleGroup.ID, ruleGroup.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeNetworkFirewallRuleGroup,
		Name:         strings.TrimSpace(ruleGroup.Name),
		Tags:         cloneStringMap(ruleGroup.Tags),
		Attributes: map[string]any{
			"id":                     strings.TrimSpace(ruleGroup.ID),
			"description":            strings.TrimSpace(ruleGroup.Description),
			"type":                   strings.TrimSpace(ruleGroup.Type),
			"capacity":               ruleGroup.Capacity,
			"number_of_associations": ruleGroup.NumberOfAssociations,
		},
		CorrelationAnchors: []string{arn, ruleGroup.ID, ruleGroup.Name},
		SourceRecordID:     resourceID,
	}
}

func tlsInspectionObservation(boundary awscloud.Boundary, tlsConfig TLSInspectionConfiguration) awscloud.ResourceObservation {
	arn := strings.TrimSpace(tlsConfig.ARN)
	resourceID := firstNonEmpty(arn, tlsConfig.ID, tlsConfig.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeNetworkFirewallTLSInspectionConfiguration,
		Name:         strings.TrimSpace(tlsConfig.Name),
		State:        strings.TrimSpace(tlsConfig.Status),
		Tags:         cloneStringMap(tlsConfig.Tags),
		Attributes: map[string]any{
			"id":                     strings.TrimSpace(tlsConfig.ID),
			"description":            strings.TrimSpace(tlsConfig.Description),
			"status":                 strings.TrimSpace(tlsConfig.Status),
			"number_of_associations": tlsConfig.NumberOfAssociations,
		},
		CorrelationAnchors: []string{arn, tlsConfig.ID, tlsConfig.Name},
		SourceRecordID:     resourceID,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
