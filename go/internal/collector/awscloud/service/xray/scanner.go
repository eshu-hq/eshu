// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package xray

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS X-Ray configuration facts for one claimed account and
// region. It is configuration-only: it observes X-Ray groups, sampling rules,
// and the account-region encryption configuration. It never reads or persists
// X-Ray observability payload — traces, trace summaries, segments, or
// service-graph (service-map) data — and never calls a mutation API.
type Scanner struct {
	Client Client
}

// Scan observes X-Ray groups, sampling rules, and the account-region
// encryption configuration through the configured client, emitting one
// resource fact per configuration object plus the encryption-config-to-KMS-key
// and sampling-rule-to-service correlation relationships.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("xray scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceXRay:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceXRay
	default:
		return nil, fmt.Errorf("xray scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	groups, err := s.Client.GetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("get X-Ray groups: %w", err)
	}
	for _, group := range groups {
		envelope, err := awscloud.NewResourceEnvelope(groupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	rules, err := s.Client.GetSamplingRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("get X-Ray sampling rules: %w", err)
	}
	for _, rule := range rules {
		ruleEnvelopes, err := samplingRuleEnvelopes(boundary, rule)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, ruleEnvelopes...)
	}

	config, err := s.Client.GetEncryptionConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("get X-Ray encryption config: %w", err)
	}
	if config != nil {
		configEnvelopes, err := encryptionConfigEnvelopes(boundary, *config)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, configEnvelopes...)
	}

	return envelopes, nil
}

func samplingRuleEnvelopes(
	boundary awscloud.Boundary,
	rule SamplingRule,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(samplingRuleObservation(boundary, rule))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := samplingRuleServiceRelationship(boundary, rule); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func encryptionConfigEnvelopes(
	boundary awscloud.Boundary,
	config EncryptionConfig,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(encryptionConfigObservation(boundary, config))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := encryptionConfigKMSRelationship(boundary, config); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func groupObservation(boundary awscloud.Boundary, group Group) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	groupName := strings.TrimSpace(group.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   firstNonEmpty(groupARN, groupName),
		ResourceType: awscloud.ResourceTypeXRayGroup,
		Name:         groupName,
		Attributes: map[string]any{
			// The filter expression is group configuration (which traces the
			// group includes), not trace data. Traces selected by it are never
			// read.
			"filter_expression":     strings.TrimSpace(group.FilterExpression),
			"insights_enabled":      boolOrNil(group.InsightsEnabled),
			"notifications_enabled": boolOrNil(group.NotificationsEnabled),
		},
		CorrelationAnchors: []string{groupARN, groupName},
		SourceRecordID:     firstNonEmpty(groupARN, groupName),
	}
}

func samplingRuleObservation(boundary awscloud.Boundary, rule SamplingRule) awscloud.ResourceObservation {
	ruleARN := strings.TrimSpace(rule.ARN)
	ruleName := strings.TrimSpace(rule.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          ruleARN,
		ResourceID:   firstNonEmpty(ruleARN, ruleName),
		ResourceType: awscloud.ResourceTypeXRaySamplingRule,
		Name:         ruleName,
		Attributes: map[string]any{
			"priority":       int32OrNil(rule.Priority),
			"reservoir_size": rule.ReservoirSize,
			"fixed_rate":     rule.FixedRate,
			"service_name":   strings.TrimSpace(rule.ServiceName),
			"service_type":   strings.TrimSpace(rule.ServiceType),
			"host":           strings.TrimSpace(rule.Host),
			"http_method":    strings.TrimSpace(rule.HTTPMethod),
			"url_path":       strings.TrimSpace(rule.URLPath),
			"resource_arn":   strings.TrimSpace(rule.ResourceARN),
			"version":        int32OrNil(rule.Version),
		},
		CorrelationAnchors: []string{ruleARN, ruleName},
		SourceRecordID:     firstNonEmpty(ruleARN, ruleName),
	}
}

func encryptionConfigObservation(boundary awscloud.Boundary, config EncryptionConfig) awscloud.ResourceObservation {
	resourceID := encryptionConfigResourceID(boundary)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeXRayEncryptionConfig,
		Name:         resourceID,
		State:        strings.TrimSpace(config.Status),
		Attributes: map[string]any{
			"encryption_type": strings.TrimSpace(config.Type),
			"status":          strings.TrimSpace(config.Status),
			"kms_key_id":      strings.TrimSpace(config.KeyID),
		},
		CorrelationAnchors: []string{resourceID},
		SourceRecordID:     resourceID,
	}
}

func boolOrNil(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}
