// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarctypes "github.com/aws/aws-sdk-go-v2/service/route53recoverycontrolconfig/types"

	arcservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53recoverycontrolconfig"
)

// mapSafetyRule maps one ListSafetyRules entry, which is a union of an assertion
// rule or a gating rule, into the scanner-owned safety rule model. A rule that
// carries neither shape is skipped (returns nil) rather than emitting an empty
// resource.
func (c *Client) mapSafetyRule(
	ctx context.Context,
	rule awsarctypes.Rule,
) (*arcservice.SafetyRule, error) {
	switch {
	case rule.ASSERTION != nil:
		return c.mapAssertionRule(ctx, rule.ASSERTION)
	case rule.GATING != nil:
		return c.mapGatingRule(ctx, rule.GATING)
	default:
		return nil, nil
	}
}

func (c *Client) mapAssertionRule(
	ctx context.Context,
	rule *awsarctypes.AssertionRule,
) (*arcservice.SafetyRule, error) {
	arn := strings.TrimSpace(aws.ToString(rule.SafetyRuleArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return nil, err
	}
	mapped := arcservice.SafetyRule{
		ARN:                  arn,
		ControlPanelARN:      strings.TrimSpace(aws.ToString(rule.ControlPanelArn)),
		Name:                 strings.TrimSpace(aws.ToString(rule.Name)),
		RuleKind:             "ASSERTION",
		Status:               strings.TrimSpace(string(rule.Status)),
		WaitPeriodMs:         aws.ToInt32(rule.WaitPeriodMs),
		AssertedControlCount: len(rule.AssertedControls),
		Tags:                 tags,
	}
	applyRuleConfig(&mapped, rule.RuleConfig)
	return &mapped, nil
}

func (c *Client) mapGatingRule(
	ctx context.Context,
	rule *awsarctypes.GatingRule,
) (*arcservice.SafetyRule, error) {
	arn := strings.TrimSpace(aws.ToString(rule.SafetyRuleArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return nil, err
	}
	mapped := arcservice.SafetyRule{
		ARN:                arn,
		ControlPanelARN:    strings.TrimSpace(aws.ToString(rule.ControlPanelArn)),
		Name:               strings.TrimSpace(aws.ToString(rule.Name)),
		RuleKind:           "GATING",
		Status:             strings.TrimSpace(string(rule.Status)),
		WaitPeriodMs:       aws.ToInt32(rule.WaitPeriodMs),
		GatingControlCount: len(rule.GatingControls),
		TargetControlCount: len(rule.TargetControls),
		Tags:               tags,
	}
	applyRuleConfig(&mapped, rule.RuleConfig)
	return &mapped, nil
}

// applyRuleConfig copies the rule-config logic (type, threshold, inverted flag)
// onto the safety rule. It records the evaluation logic only, never application
// traffic or routing control state.
func applyRuleConfig(rule *arcservice.SafetyRule, config *awsarctypes.RuleConfig) {
	if config == nil {
		return
	}
	rule.RuleConfigType = strings.TrimSpace(string(config.Type))
	rule.RuleConfigThreshold = aws.ToInt32(config.Threshold)
	rule.RuleConfigInverted = aws.ToBool(config.Inverted)
}

// endpointRegions returns the AWS Region names of a cluster's regional
// endpoints. Endpoint URLs are intentionally dropped so the adapter never
// surfaces a handle to the routing control state data plane.
func endpointRegions(endpoints []awsarctypes.ClusterEndpoint) []string {
	if len(endpoints) == 0 {
		return nil
	}
	regions := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if region := strings.TrimSpace(aws.ToString(endpoint.Region)); region != "" {
			regions = append(regions, region)
		}
	}
	if len(regions) == 0 {
		return nil
	}
	return regions
}
