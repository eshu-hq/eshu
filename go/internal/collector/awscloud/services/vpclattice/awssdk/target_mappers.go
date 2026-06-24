// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsvpclattice "github.com/aws/aws-sdk-go-v2/service/vpclattice"
	awsvpclatticetypes "github.com/aws/aws-sdk-go-v2/service/vpclattice/types"

	vpclatticeservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/vpclattice"
)

func (c *Client) listTargetGroups(ctx context.Context) ([]vpclatticeservice.TargetGroup, error) {
	var groups []vpclatticeservice.TargetGroup
	var nextToken *string
	for {
		var page *awsvpclattice.ListTargetGroupsOutput
		err := c.recordAPICall(ctx, "ListTargetGroups", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListTargetGroups(callCtx, &awsvpclattice.ListTargetGroupsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, summary := range page.Items {
			mapped, err := c.mapTargetGroup(ctx, summary)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return groups, nil
		}
	}
}

func (c *Client) mapTargetGroup(
	ctx context.Context,
	summary awsvpclatticetypes.TargetGroupSummary,
) (vpclatticeservice.TargetGroup, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.Id))
	groupType := strings.TrimSpace(string(summary.Type))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return vpclatticeservice.TargetGroup{}, err
	}
	group := vpclatticeservice.TargetGroup{
		ARN:           arn,
		ID:            id,
		Name:          strings.TrimSpace(aws.ToString(summary.Name)),
		Type:          groupType,
		Protocol:      strings.TrimSpace(string(summary.Protocol)),
		Port:          aws.ToInt32(summary.Port),
		IPAddressType: strings.TrimSpace(string(summary.IpAddressType)),
		Status:        strings.TrimSpace(string(summary.Status)),
		VPCID:         strings.TrimSpace(aws.ToString(summary.VpcIdentifier)),
		ServiceARNs:   trimmedStrings(summary.ServiceArns),
		CreatedAt:     aws.ToTime(summary.CreatedAt),
		LastUpdatedAt: aws.ToTime(summary.LastUpdatedAt),
		Tags:          tags,
	}
	if err := c.enrichTargetGroup(ctx, &group, id); err != nil {
		return vpclatticeservice.TargetGroup{}, err
	}
	targets, err := c.listTargets(ctx, id)
	if err != nil {
		return vpclatticeservice.TargetGroup{}, err
	}
	group.Targets = targets
	return group, nil
}

// enrichTargetGroup reads GetTargetGroup for the backing VPC identifier and
// protocol the ListTargetGroups summary may omit. It never reads any data-plane
// payload.
func (c *Client) enrichTargetGroup(ctx context.Context, group *vpclatticeservice.TargetGroup, groupID string) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil
	}
	var output *awsvpclattice.GetTargetGroupOutput
	err := c.recordAPICall(ctx, "GetTargetGroup", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetTargetGroup(callCtx, &awsvpclattice.GetTargetGroupInput{
			TargetGroupIdentifier: aws.String(groupID),
		})
		return callErr
	})
	if err != nil || output == nil {
		return err
	}
	if len(output.ServiceArns) > 0 {
		group.ServiceARNs = trimmedStrings(output.ServiceArns)
	}
	if config := output.Config; config != nil {
		if vpc := strings.TrimSpace(aws.ToString(config.VpcIdentifier)); vpc != "" {
			group.VPCID = vpc
		}
		if group.Protocol == "" {
			group.Protocol = strings.TrimSpace(string(config.Protocol))
		}
		if group.IPAddressType == "" {
			group.IPAddressType = strings.TrimSpace(string(config.IpAddressType))
		}
		if group.Port == 0 {
			group.Port = aws.ToInt32(config.Port)
		}
	}
	return nil
}

func (c *Client) listTargets(ctx context.Context, groupID string) ([]vpclatticeservice.Target, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, nil
	}
	var targets []vpclatticeservice.Target
	var nextToken *string
	for {
		var page *awsvpclattice.ListTargetsOutput
		err := c.recordAPICall(ctx, "ListTargets", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListTargets(callCtx, &awsvpclattice.ListTargetsInput{
				TargetGroupIdentifier: aws.String(groupID),
				NextToken:             nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return targets, nil
		}
		for _, summary := range page.Items {
			targets = append(targets, vpclatticeservice.Target{
				ID:     strings.TrimSpace(aws.ToString(summary.Id)),
				Port:   aws.ToInt32(summary.Port),
				Status: strings.TrimSpace(string(summary.Status)),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return targets, nil
		}
	}
}

// trimmedStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func trimmedStrings(input []string) []string {
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
