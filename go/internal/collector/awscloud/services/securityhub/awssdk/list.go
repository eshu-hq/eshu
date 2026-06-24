// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecurityhub "github.com/aws/aws-sdk-go-v2/service/securityhub"

	securityhubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub"
)

func (c *Client) getAdministrator(ctx context.Context) (securityhubservice.Member, error) {
	var output *awssecurityhub.GetAdministratorAccountOutput
	err := c.recordAPICall(ctx, "GetAdministratorAccount", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetAdministratorAccount(callCtx, &awssecurityhub.GetAdministratorAccountInput{})
		return err
	})
	if err != nil {
		return securityhubservice.Member{}, fmt.Errorf("get Security Hub administrator account: %w", err)
	}
	if output == nil || output.Administrator == nil {
		return securityhubservice.Member{}, nil
	}
	administrator := output.Administrator
	return securityhubservice.Member{
		AccountID: aws.ToString(administrator.AccountId),
		Status:    aws.ToString(administrator.MemberStatus),
		InvitedAt: aws.ToTime(administrator.InvitedAt),
	}, nil
}

func (c *Client) listMembers(ctx context.Context) ([]securityhubservice.Member, error) {
	var members []securityhubservice.Member
	var nextToken *string
	for {
		var page *awssecurityhub.ListMembersOutput
		err := c.recordAPICall(ctx, "ListMembers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListMembers(callCtx, &awssecurityhub.ListMembersInput{
				MaxResults:     aws.Int32(defaultPageSize),
				NextToken:      nextToken,
				OnlyAssociated: aws.Bool(false),
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("list Security Hub member accounts: %w", err)
		}
		if page == nil {
			return members, nil
		}
		for _, member := range page.Members {
			members = append(members, mapMember(member))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return members, nil
		}
	}
}

func (c *Client) listStandards(ctx context.Context) ([]securityhubservice.Standard, error) {
	var standards []securityhubservice.Standard
	var nextToken *string
	for {
		var page *awssecurityhub.GetEnabledStandardsOutput
		err := c.recordAPICall(ctx, "GetEnabledStandards", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetEnabledStandards(callCtx, &awssecurityhub.GetEnabledStandardsInput{
				MaxResults: aws.Int32(defaultPageSize),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("get Security Hub enabled standards: %w", err)
		}
		if page == nil {
			return standards, nil
		}
		for _, raw := range page.StandardsSubscriptions {
			standard := mapStandard(raw)
			tags, err := c.listTags(ctx, standard.SubscriptionARN)
			if err != nil {
				return nil, err
			}
			standard.Tags = tags
			controls, err := c.listControls(ctx, standard.SubscriptionARN)
			if err != nil {
				return nil, err
			}
			standard.Controls = controls
			standards = append(standards, standard)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return standards, nil
		}
	}
}

func (c *Client) listControls(
	ctx context.Context,
	subscriptionARN string,
) ([]securityhubservice.Control, error) {
	subscriptionARN = strings.TrimSpace(subscriptionARN)
	if subscriptionARN == "" {
		return nil, nil
	}
	var controls []securityhubservice.Control
	var nextToken *string
	for {
		var page *awssecurityhub.DescribeStandardsControlsOutput
		err := c.recordAPICall(ctx, "DescribeStandardsControls", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeStandardsControls(callCtx, &awssecurityhub.DescribeStandardsControlsInput{
				MaxResults:               aws.Int32(defaultPageSize),
				NextToken:                nextToken,
				StandardsSubscriptionArn: aws.String(subscriptionARN),
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("describe Security Hub controls for %q: %w", subscriptionARN, err)
		}
		if page == nil {
			return controls, nil
		}
		for _, raw := range page.Controls {
			controls = append(controls, mapControl(raw))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return controls, nil
		}
	}
}

func (c *Client) listActionTargets(ctx context.Context) ([]securityhubservice.ActionTarget, error) {
	var targets []securityhubservice.ActionTarget
	var nextToken *string
	for {
		var page *awssecurityhub.DescribeActionTargetsOutput
		err := c.recordAPICall(ctx, "DescribeActionTargets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeActionTargets(callCtx, &awssecurityhub.DescribeActionTargetsInput{
				MaxResults: aws.Int32(defaultPageSize),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("describe Security Hub action targets: %w", err)
		}
		if page == nil {
			return targets, nil
		}
		for _, raw := range page.ActionTargets {
			targets = append(targets, mapActionTarget(raw))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return targets, nil
		}
	}
}

func (c *Client) listInsights(ctx context.Context) ([]securityhubservice.Insight, error) {
	var insights []securityhubservice.Insight
	var nextToken *string
	for {
		var page *awssecurityhub.GetInsightsOutput
		err := c.recordAPICall(ctx, "GetInsights", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetInsights(callCtx, &awssecurityhub.GetInsightsInput{
				MaxResults: aws.Int32(defaultPageSize),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("get Security Hub insights: %w", err)
		}
		if page == nil {
			return insights, nil
		}
		for _, raw := range page.Insights {
			insight := mapInsight(raw)
			if groupsByControl(insight.GroupByAttribute) {
				controlIDs, err := c.insightControlIDs(ctx, insight.ARN)
				if err != nil {
					return nil, err
				}
				insight.ControlIDs = controlIDs
			}
			insights = append(insights, insight)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return insights, nil
		}
	}
}

func (c *Client) insightControlIDs(ctx context.Context, insightARN string) ([]string, error) {
	insightARN = strings.TrimSpace(insightARN)
	if insightARN == "" {
		return nil, nil
	}
	var output *awssecurityhub.GetInsightResultsOutput
	err := c.recordAPICall(ctx, "GetInsightResults", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetInsightResults(callCtx, &awssecurityhub.GetInsightResultsInput{
			InsightArn: aws.String(insightARN),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("get Security Hub insight results for %q: %w", insightARN, err)
	}
	if output == nil || output.InsightResults == nil ||
		!groupsByControl(aws.ToString(output.InsightResults.GroupByAttribute)) {
		return nil, nil
	}
	controlIDs := make([]string, 0, len(output.InsightResults.ResultValues))
	for _, value := range output.InsightResults.ResultValues {
		controlID := strings.TrimSpace(aws.ToString(value.GroupByAttributeValue))
		if controlID != "" {
			controlIDs = append(controlIDs, controlID)
		}
	}
	sort.Strings(controlIDs)
	return controlIDs, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awssecurityhub.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awssecurityhub.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list Security Hub tags for %q: %w", resourceARN, err)
	}
	if output == nil {
		return nil, nil
	}
	return cloneStringMap(output.Tags), nil
}
