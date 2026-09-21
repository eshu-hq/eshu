// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Config metadata facts for one claimed account and region.
// It never reads recorded configuration item bodies, never reads per-resource
// compliance evaluation result bodies, never fetches custom-rule Lambda code,
// and never mutates Config resources.
type Scanner struct {
	Client Client
}

// Scan observes AWS Config configuration recorders, delivery channels, config
// rules, conformance packs, configuration aggregators, and retention
// configurations through the configured client. It emits one aws_resource fact
// per Config object plus conformance-pack-to-rule, custom-rule-to-Lambda, and
// aggregator-to-source-account relationship facts. The rule resource-type scope
// is carried as an attribute on the rule resource rather than as a relationship
// to a synthetic resource-type node that no scanner emits.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("config scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceConfig:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceConfig
	default:
		return nil, fmt.Errorf("config scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	recorders, err := s.Client.ConfigurationRecorders(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Config configuration recorders: %w", err)
	}
	for _, recorder := range recorders {
		envelope, err := awscloud.NewResourceEnvelope(recorderObservation(boundary, recorder))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	channels, err := s.Client.DeliveryChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Config delivery channels: %w", err)
	}
	for _, channel := range channels {
		envelope, err := awscloud.NewResourceEnvelope(deliveryChannelObservation(boundary, channel))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	rules, err := s.Client.ConfigRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Config rules: %w", err)
	}
	for _, rule := range rules {
		envelope, err := awscloud.NewResourceEnvelope(ruleObservation(boundary, rule))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		if rel, ok := ruleLambdaRelationship(boundary, rule); ok {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
	}

	packs, err := s.Client.ConformancePacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Config conformance packs: %w", err)
	}
	for _, pack := range packs {
		envelope, err := awscloud.NewResourceEnvelope(conformancePackObservation(boundary, pack))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		for _, ruleName := range pack.RuleNames {
			rel, ok := conformancePackRuleRelationship(boundary, pack, ruleName)
			if !ok {
				continue
			}
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
	}

	aggregators, err := s.Client.ConfigurationAggregators(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Config configuration aggregators: %w", err)
	}
	for _, aggregator := range aggregators {
		envelope, err := awscloud.NewResourceEnvelope(aggregatorObservation(boundary, aggregator))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		for _, accountID := range aggregator.SourceAccountIDs {
			rel, ok := aggregatorAccountRelationship(boundary, aggregator, accountID)
			if !ok {
				continue
			}
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
	}

	retentions, err := s.Client.RetentionConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Config retention configurations: %w", err)
	}
	for _, retention := range retentions {
		envelope, err := awscloud.NewResourceEnvelope(retentionObservation(boundary, retention))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func recorderObservation(boundary awscloud.Boundary, recorder ConfigurationRecorder) awscloud.ResourceObservation {
	name := strings.TrimSpace(recorder.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   recorderResourceID(name),
		ResourceType: awscloud.ResourceTypeConfigConfigurationRecorder,
		Name:         name,
		Attributes: map[string]any{
			"all_supported":                 recorder.AllSupported,
			"include_global_resource_types": recorder.IncludeGlobalResourceTypes,
			"recording_strategy":            strings.TrimSpace(recorder.RecordingStrategy),
			"resource_types":                cloneStrings(recorder.ResourceTypes),
			"resource_type_count":           len(recorder.ResourceTypes),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     recorderResourceID(name),
	}
}

func deliveryChannelObservation(boundary awscloud.Boundary, channel DeliveryChannel) awscloud.ResourceObservation {
	name := strings.TrimSpace(channel.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   deliveryChannelResourceID(name),
		ResourceType: awscloud.ResourceTypeConfigDeliveryChannel,
		Name:         name,
		Attributes: map[string]any{
			"s3_bucket_name":             strings.TrimSpace(channel.S3BucketName),
			"s3_key_prefix":              strings.TrimSpace(channel.S3KeyPrefix),
			"s3_kms_key_arn":             strings.TrimSpace(channel.S3KMSKeyARN),
			"sns_topic_arn":              strings.TrimSpace(channel.SNSTopicARN),
			"snapshot_delivery_interval": strings.TrimSpace(channel.SnapshotDeliveryInterval),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     deliveryChannelResourceID(name),
	}
}

func ruleObservation(boundary awscloud.Boundary, rule ConfigRule) awscloud.ResourceObservation {
	ruleARN := strings.TrimSpace(rule.ARN)
	name := strings.TrimSpace(rule.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          ruleARN,
		ResourceID:   ruleResourceID(name),
		ResourceType: awscloud.ResourceTypeConfigRule,
		Name:         name,
		State:        strings.TrimSpace(rule.State),
		Attributes: map[string]any{
			"config_rule_id":       strings.TrimSpace(rule.ID),
			"owner":                strings.TrimSpace(rule.Owner),
			"source_identifier":    strings.TrimSpace(rule.SourceIdentifier),
			"lambda_function_arn":  strings.TrimSpace(rule.LambdaFunctionARN),
			"scope_resource_types": cloneStrings(rule.ScopeResourceTypes),
		},
		CorrelationAnchors: []string{name, ruleARN},
		SourceRecordID:     firstNonEmpty(ruleARN, ruleResourceID(name)),
	}
}

func conformancePackObservation(boundary awscloud.Boundary, pack ConformancePack) awscloud.ResourceObservation {
	packARN := strings.TrimSpace(pack.ARN)
	name := strings.TrimSpace(pack.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          packARN,
		ResourceID:   firstNonEmpty(packARN, conformancePackResourceID(name)),
		ResourceType: awscloud.ResourceTypeConfigConformancePack,
		Name:         name,
		State:        strings.TrimSpace(pack.Status),
		Attributes: map[string]any{
			"conformance_pack_id": strings.TrimSpace(pack.ID),
			"status":              strings.TrimSpace(pack.Status),
			"created_by":          strings.TrimSpace(pack.CreatedBy),
			"rule_count":          len(pack.RuleNames),
		},
		CorrelationAnchors: []string{name, packARN},
		SourceRecordID:     firstNonEmpty(packARN, conformancePackResourceID(name)),
	}
}

func aggregatorObservation(boundary awscloud.Boundary, aggregator ConfigurationAggregator) awscloud.ResourceObservation {
	aggregatorARN := strings.TrimSpace(aggregator.ARN)
	name := strings.TrimSpace(aggregator.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          aggregatorARN,
		ResourceID:   firstNonEmpty(aggregatorARN, aggregatorResourceID(name)),
		ResourceType: awscloud.ResourceTypeConfigConfigurationAggregator,
		Name:         name,
		Attributes: map[string]any{
			"created_by":                   strings.TrimSpace(aggregator.CreatedBy),
			"source_account_ids":           cloneStrings(aggregator.SourceAccountIDs),
			"source_account_count":         len(aggregator.SourceAccountIDs),
			"source_regions":               cloneStrings(aggregator.SourceRegions),
			"all_aws_regions":              aggregator.AllAWSRegions,
			"organization_role_arn":        strings.TrimSpace(aggregator.OrganizationRoleARN),
			"organization_all_aws_regions": aggregator.OrganizationAllAWSRegions,
		},
		CorrelationAnchors: []string{name, aggregatorARN},
		SourceRecordID:     firstNonEmpty(aggregatorARN, aggregatorResourceID(name)),
	}
}

func retentionObservation(boundary awscloud.Boundary, retention RetentionConfiguration) awscloud.ResourceObservation {
	name := strings.TrimSpace(retention.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   retentionResourceID(name),
		ResourceType: awscloud.ResourceTypeConfigRetentionConfiguration,
		Name:         name,
		Attributes: map[string]any{
			"retention_period_in_days": retention.RetentionPeriodInDays,
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     retentionResourceID(name),
	}
}

func recorderResourceID(name string) string {
	return "config-recorder/" + strings.TrimSpace(name)
}

func deliveryChannelResourceID(name string) string {
	return "config-delivery-channel/" + strings.TrimSpace(name)
}

func ruleResourceID(name string) string {
	return "config-rule/" + strings.TrimSpace(name)
}

func conformancePackResourceID(name string) string {
	return "config-conformance-pack/" + strings.TrimSpace(name)
}

func aggregatorResourceID(name string) string {
	return "config-aggregator/" + strings.TrimSpace(name)
}

func retentionResourceID(name string) string {
	return "config-retention/" + strings.TrimSpace(name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
