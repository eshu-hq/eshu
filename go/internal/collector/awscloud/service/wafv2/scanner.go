// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wafv2

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS WAFv2 metadata facts for one claimed account and scope. It
// never persists IP set address lists, regex pattern bodies, or rule Statement
// bodies, and it never calls any WAFv2 mutation API.
type Scanner struct {
	Client Client
}

// Scan observes WAFv2 web ACLs, rule groups, IP sets, and regex pattern sets
// through the configured client and emits resource and relationship facts. The
// REGIONAL or CLOUDFRONT scope is selected by the SDK adapter from the claim
// boundary; the scanner records the reported scope on each resource.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("wafv2 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceWAFv2:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceWAFv2
	default:
		return nil, fmt.Errorf("wafv2 scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope
	webACLEnvelopes, err := s.scanWebACLs(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, webACLEnvelopes...)

	ruleGroupEnvelopes, err := s.scanRuleGroups(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, ruleGroupEnvelopes...)

	ipSetEnvelopes, err := s.scanIPSets(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, ipSetEnvelopes...)

	regexEnvelopes, err := s.scanRegexPatternSets(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, regexEnvelopes...)
	return envelopes, nil
}

func (s Scanner) scanWebACLs(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	webACLs, err := s.Client.ListWebACLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list WAFv2 web ACLs: %w", err)
	}
	var envelopes []facts.Envelope
	for _, webACL := range webACLs {
		resource, err := awscloud.NewResourceEnvelope(webACLObservation(boundary, webACL))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		relationships, err := webACLRelationshipEnvelopes(boundary, webACL)
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
		return nil, fmt.Errorf("list WAFv2 rule groups: %w", err)
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

func (s Scanner) scanIPSets(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	ipSets, err := s.Client.ListIPSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list WAFv2 IP sets: %w", err)
	}
	var envelopes []facts.Envelope
	for _, ipSet := range ipSets {
		resource, err := awscloud.NewResourceEnvelope(ipSetObservation(boundary, ipSet))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func (s Scanner) scanRegexPatternSets(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	regexSets, err := s.Client.ListRegexPatternSets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list WAFv2 regex pattern sets: %w", err)
	}
	var envelopes []facts.Envelope
	for _, regexSet := range regexSets {
		resource, err := awscloud.NewResourceEnvelope(regexPatternSetObservation(boundary, regexSet))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func webACLObservation(boundary awscloud.Boundary, webACL WebACL) awscloud.ResourceObservation {
	arn := strings.TrimSpace(webACL.ARN)
	resourceID := firstNonEmpty(arn, webACL.ID, webACL.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeWAFv2WebACL,
		Name:         strings.TrimSpace(webACL.Name),
		Tags:         cloneStringMap(webACL.Tags),
		Attributes: map[string]any{
			"id":                       strings.TrimSpace(webACL.ID),
			"description":              strings.TrimSpace(webACL.Description),
			"scope":                    strings.TrimSpace(webACL.Scope),
			"rule_count":               webACL.RuleCount,
			"capacity":                 webACL.Capacity,
			"default_action":           strings.TrimSpace(webACL.DefaultAction),
			"managed_by_firewall":      webACL.ManagedByFirewall,
			"managed_rule_set_refs":    managedRuleSetRefAttributes(webACL.ManagedRuleSetRefs),
			"rule_group_ref_count":     len(webACL.RuleGroupRefARNs),
			"ip_set_ref_count":         len(webACL.IPSetRefARNs),
			"regex_set_ref_count":      len(webACL.RegexSetRefARNs),
			"protected_resource_count": len(webACL.ProtectedResources),
		},
		CorrelationAnchors: []string{arn, webACL.ID, webACL.Name},
		SourceRecordID:     resourceID,
	}
}

func managedRuleSetRefAttributes(refs []ManagedRuleSetRef) []map[string]any {
	if len(refs) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		vendor := strings.TrimSpace(ref.VendorName)
		name := strings.TrimSpace(ref.Name)
		if vendor == "" && name == "" {
			continue
		}
		output = append(output, map[string]any{
			"vendor_name": vendor,
			"name":        name,
			"version":     strings.TrimSpace(ref.Version),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func webACLRelationshipEnvelopes(boundary awscloud.Boundary, webACL WebACL) ([]facts.Envelope, error) {
	arn := strings.TrimSpace(webACL.ARN)
	sourceID := firstNonEmpty(arn, webACL.ID, webACL.Name)
	if sourceID == "" {
		return nil, nil
	}
	var envelopes []facts.Envelope
	add := func(relationshipType, targetARN, targetType string) error {
		targetARN = strings.TrimSpace(targetARN)
		if targetARN == "" {
			return nil
		}
		envelope, err := awscloud.NewRelationshipEnvelope(awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: relationshipType,
			SourceResourceID: sourceID,
			SourceARN:        arn,
			TargetResourceID: targetARN,
			TargetARN:        targetARN,
			TargetType:       targetType,
		})
		if err != nil {
			return err
		}
		envelopes = append(envelopes, envelope)
		return nil
	}

	for _, resource := range webACL.ProtectedResources {
		if err := addProtectedResource(boundary, sourceID, arn, resource, &envelopes); err != nil {
			return nil, err
		}
	}
	for _, ruleGroupARN := range dedupeStrings(webACL.RuleGroupRefARNs) {
		if err := add(awscloud.RelationshipWAFv2WebACLUsesRuleGroup, ruleGroupARN, awscloud.ResourceTypeWAFv2RuleGroup); err != nil {
			return nil, err
		}
	}
	for _, ipSetARN := range dedupeStrings(webACL.IPSetRefARNs) {
		if err := add(awscloud.RelationshipWAFv2WebACLUsesIPSet, ipSetARN, awscloud.ResourceTypeWAFv2IPSet); err != nil {
			return nil, err
		}
	}
	for _, regexARN := range dedupeStrings(webACL.RegexSetRefARNs) {
		if err := add(awscloud.RelationshipWAFv2WebACLUsesRegexPatternSet, regexARN, awscloud.ResourceTypeWAFv2RegexPatternSet); err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func addProtectedResource(
	boundary awscloud.Boundary,
	sourceID string,
	sourceARN string,
	resource ProtectedResource,
	envelopes *[]facts.Envelope,
) error {
	targetARN := strings.TrimSpace(resource.ARN)
	if targetARN == "" {
		return nil
	}
	envelope, err := awscloud.NewRelationshipEnvelope(awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipWAFv2WebACLProtectsResource,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       protectedResourceTargetType(resource.ResourceType, targetARN),
		Attributes: map[string]any{
			"protected_resource_type": strings.TrimSpace(resource.ResourceType),
		},
	})
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

// protectedResourceTargetType resolves the Eshu resource type for a web ACL's
// protected resource so downstream correlation can attach the relationship to a
// concrete target node. The AWS-reported WAFv2 enum (ListResourcesForWebACL
// ResourceType, or empty for CloudFront associations) is the primary signal;
// the ARN service prefix is the fallback when the enum is empty or unknown.
// Targets without a canonical Eshu constant (Cognito, AppSync, App Runner,
// Amplify, Verified Access) fall back to the generic "aws_resource" type, which
// mirrors the ACM scanner so a relationship is still emitted and correlation can
// resolve the precise node later.
func protectedResourceTargetType(awsResourceType, targetARN string) string {
	switch strings.ToUpper(strings.TrimSpace(awsResourceType)) {
	case "APPLICATION_LOAD_BALANCER":
		return awscloud.ResourceTypeELBv2LoadBalancer
	case "API_GATEWAY":
		return awscloud.ResourceTypeAPIGatewayStage
	}
	return targetTypeForProtectedARN(targetARN)
}

// targetTypeForProtectedARN maps a protected-resource ARN service prefix to an
// Eshu resource type constant. It is the fallback path when the WAFv2 enum is
// empty (CloudFront associations) or names a service Eshu does not yet have a
// canonical constant for. Unknown services resolve to the generic
// "aws_resource" type.
func targetTypeForProtectedARN(arn string) string {
	switch {
	case strings.Contains(arn, ":elasticloadbalancing:"):
		return awscloud.ResourceTypeELBv2LoadBalancer
	case strings.Contains(arn, ":cloudfront:"):
		return awscloud.ResourceTypeCloudFrontDistribution
	case strings.Contains(arn, ":apigateway:"):
		return awscloud.ResourceTypeAPIGatewayStage
	default:
		return "aws_resource"
	}
}

func ruleGroupObservation(boundary awscloud.Boundary, ruleGroup RuleGroup) awscloud.ResourceObservation {
	arn := strings.TrimSpace(ruleGroup.ARN)
	resourceID := firstNonEmpty(arn, ruleGroup.ID, ruleGroup.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeWAFv2RuleGroup,
		Name:         strings.TrimSpace(ruleGroup.Name),
		Tags:         cloneStringMap(ruleGroup.Tags),
		Attributes: map[string]any{
			"id":          strings.TrimSpace(ruleGroup.ID),
			"description": strings.TrimSpace(ruleGroup.Description),
			"scope":       strings.TrimSpace(ruleGroup.Scope),
			"rule_count":  ruleGroup.RuleCount,
			"capacity":    ruleGroup.Capacity,
		},
		CorrelationAnchors: []string{arn, ruleGroup.ID, ruleGroup.Name},
		SourceRecordID:     resourceID,
	}
}

func ipSetObservation(boundary awscloud.Boundary, ipSet IPSet) awscloud.ResourceObservation {
	arn := strings.TrimSpace(ipSet.ARN)
	resourceID := firstNonEmpty(arn, ipSet.ID, ipSet.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeWAFv2IPSet,
		Name:         strings.TrimSpace(ipSet.Name),
		Tags:         cloneStringMap(ipSet.Tags),
		Attributes: map[string]any{
			"id":            strings.TrimSpace(ipSet.ID),
			"description":   strings.TrimSpace(ipSet.Description),
			"scope":         strings.TrimSpace(ipSet.Scope),
			"ip_version":    strings.TrimSpace(ipSet.IPVersion),
			"address_count": ipSet.AddressCount,
		},
		CorrelationAnchors: []string{arn, ipSet.ID, ipSet.Name},
		SourceRecordID:     resourceID,
	}
}

func regexPatternSetObservation(boundary awscloud.Boundary, regexSet RegexPatternSet) awscloud.ResourceObservation {
	arn := strings.TrimSpace(regexSet.ARN)
	resourceID := firstNonEmpty(arn, regexSet.ID, regexSet.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeWAFv2RegexPatternSet,
		Name:         strings.TrimSpace(regexSet.Name),
		Tags:         cloneStringMap(regexSet.Tags),
		Attributes: map[string]any{
			"id":            strings.TrimSpace(regexSet.ID),
			"description":   strings.TrimSpace(regexSet.Description),
			"scope":         strings.TrimSpace(regexSet.Scope),
			"pattern_count": regexSet.PatternCount,
		},
		CorrelationAnchors: []string{arn, regexSet.ID, regexSet.Name},
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
