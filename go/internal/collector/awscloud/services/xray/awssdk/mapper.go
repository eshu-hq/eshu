// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	awsxraytypes "github.com/aws/aws-sdk-go-v2/service/xray/types"

	xrayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/xray"
)

// mapGroup converts an X-Ray GroupSummary into the scanner-owned Group model.
// Only configuration identity, the trace filter expression, and the insights
// enablement flags are carried; no trace selected by the filter is read.
func mapGroup(raw awsxraytypes.GroupSummary) xrayservice.Group {
	group := xrayservice.Group{
		ARN:              aws.ToString(raw.GroupARN),
		Name:             aws.ToString(raw.GroupName),
		FilterExpression: aws.ToString(raw.FilterExpression),
	}
	if raw.InsightsConfiguration != nil {
		group.InsightsEnabled = raw.InsightsConfiguration.InsightsEnabled
		group.NotificationsEnabled = raw.InsightsConfiguration.NotificationsEnabled
	}
	return group
}

// mapSamplingRule converts an X-Ray SamplingRuleRecord into the scanner-owned
// SamplingRule model. It reports ok=false when the record carries no embedded
// rule so the caller skips an empty entry rather than emitting a nameless rule.
func mapSamplingRule(raw awsxraytypes.SamplingRuleRecord) (xrayservice.SamplingRule, bool) {
	if raw.SamplingRule == nil {
		return xrayservice.SamplingRule{}, false
	}
	rule := raw.SamplingRule
	return xrayservice.SamplingRule{
		ARN:           aws.ToString(rule.RuleARN),
		Name:          aws.ToString(rule.RuleName),
		Priority:      rule.Priority,
		ReservoirSize: rule.ReservoirSize,
		FixedRate:     rule.FixedRate,
		ServiceName:   aws.ToString(rule.ServiceName),
		ServiceType:   aws.ToString(rule.ServiceType),
		Host:          aws.ToString(rule.Host),
		HTTPMethod:    aws.ToString(rule.HTTPMethod),
		URLPath:       aws.ToString(rule.URLPath),
		ResourceARN:   aws.ToString(rule.ResourceARN),
		Version:       rule.Version,
	}, true
}

// mapEncryptionConfig converts an X-Ray EncryptionConfig into the scanner-owned
// EncryptionConfig model. The enum type and status are carried as their string
// values; the KMS key reference is carried verbatim for the downstream KMS edge.
func mapEncryptionConfig(raw *awsxraytypes.EncryptionConfig) xrayservice.EncryptionConfig {
	return xrayservice.EncryptionConfig{
		Type:   string(raw.Type),
		Status: string(raw.Status),
		KeyID:  aws.ToString(raw.KeyId),
	}
}
