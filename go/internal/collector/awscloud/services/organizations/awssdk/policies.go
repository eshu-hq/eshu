// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsorg "github.com/aws/aws-sdk-go-v2/service/organizations"
	awsorgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"

	organizationsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations"
)

func (c *Client) listPolicySummaries(ctx context.Context) ([]organizationsservice.Policy, error) {
	var policies []organizationsservice.Policy
	for _, policyType := range organizationsPolicyTypes() {
		pagePolicies, err := c.listPoliciesByType(ctx, policyType)
		if err != nil {
			return nil, err
		}
		policies = append(policies, pagePolicies...)
	}
	return policies, nil
}

func (c *Client) listPoliciesByType(
	ctx context.Context,
	policyType awsorgtypes.PolicyType,
) ([]organizationsservice.Policy, error) {
	var policies []organizationsservice.Policy
	var nextToken *string
	for {
		var output *awsorg.ListPoliciesOutput
		err := c.recordAPICall(ctx, "ListPolicies", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListPolicies(callCtx, &awsorg.ListPoliciesInput{
				Filter:    policyType,
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			if isPolicyTypeUnavailableError(err) {
				return nil, nil
			}
			return nil, err
		}
		if output == nil {
			return policies, nil
		}
		for _, policy := range output.Policies {
			mapped, err := c.mapPolicy(ctx, policy)
			if err != nil {
				return nil, err
			}
			policies = append(policies, mapped)
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return policies, nil
		}
	}
}

func (c *Client) mapPolicy(ctx context.Context, policy awsorgtypes.PolicySummary) (organizationsservice.Policy, error) {
	policyID := aws.ToString(policy.Id)
	targets, err := c.listTargetsForPolicy(ctx, policyID)
	if err != nil {
		return organizationsservice.Policy{}, err
	}
	tags, err := c.listTags(ctx, policyID)
	if err != nil {
		return organizationsservice.Policy{}, err
	}
	return organizationsservice.Policy{
		ARN:         strings.TrimSpace(aws.ToString(policy.Arn)),
		AWSManaged:  policy.AwsManaged,
		Description: strings.TrimSpace(aws.ToString(policy.Description)),
		ID:          strings.TrimSpace(policyID),
		Name:        strings.TrimSpace(aws.ToString(policy.Name)),
		Type:        strings.TrimSpace(string(policy.Type)),
		Targets:     targets,
		Tags:        tags,
	}, nil
}

func (c *Client) listTargetsForPolicy(ctx context.Context, policyID string) ([]organizationsservice.PolicyTarget, error) {
	policyID = strings.TrimSpace(policyID)
	if policyID == "" {
		return nil, nil
	}
	var targets []organizationsservice.PolicyTarget
	var nextToken *string
	for {
		var output *awsorg.ListTargetsForPolicyOutput
		err := c.recordAPICall(ctx, "ListTargetsForPolicy", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTargetsForPolicy(callCtx, &awsorg.ListTargetsForPolicyInput{
				NextToken: nextToken,
				PolicyId:  aws.String(policyID),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return targets, nil
		}
		for _, target := range output.Targets {
			targets = append(targets, organizationsservice.PolicyTarget{
				ARN:  strings.TrimSpace(aws.ToString(target.Arn)),
				ID:   strings.TrimSpace(aws.ToString(target.TargetId)),
				Name: strings.TrimSpace(aws.ToString(target.Name)),
				Type: strings.TrimSpace(string(target.Type)),
			})
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return targets, nil
		}
	}
}

func organizationsPolicyTypes() []awsorgtypes.PolicyType {
	return []awsorgtypes.PolicyType{
		awsorgtypes.PolicyTypeServiceControlPolicy,
		awsorgtypes.PolicyTypeResourceControlPolicy,
		awsorgtypes.PolicyTypeTagPolicy,
		awsorgtypes.PolicyTypeAiservicesOptOutPolicy,
		awsorgtypes.PolicyTypeBackupPolicy,
	}
}
