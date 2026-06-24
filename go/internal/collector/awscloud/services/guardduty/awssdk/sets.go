// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsguardduty "github.com/aws/aws-sdk-go-v2/service/guardduty"

	guarddutyservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/guardduty"
)

func (c *Client) listPublishingDestinations(
	ctx context.Context,
	detectorID string,
) ([]guarddutyservice.PublishingDestination, error) {
	var destinations []guarddutyservice.PublishingDestination
	var nextToken *string
	for {
		var page *awsguardduty.ListPublishingDestinationsOutput
		err := c.recordAPICall(ctx, "ListPublishingDestinations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPublishingDestinations(callCtx, &awsguardduty.ListPublishingDestinationsInput{
				DetectorId: aws.String(detectorID),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return destinations, nil
		}
		for _, destination := range page.Destinations {
			mapped, err := c.describePublishingDestination(ctx, detectorID, aws.ToString(destination.DestinationId))
			if err != nil {
				return nil, err
			}
			if mapped.ID == "" {
				mapped.ID = strings.TrimSpace(aws.ToString(destination.DestinationId))
			}
			if mapped.DestinationType == "" {
				mapped.DestinationType = string(destination.DestinationType)
			}
			if mapped.Status == "" {
				mapped.Status = string(destination.Status)
			}
			destinations = append(destinations, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return destinations, nil
		}
	}
}

func (c *Client) describePublishingDestination(
	ctx context.Context,
	detectorID string,
	destinationID string,
) (guarddutyservice.PublishingDestination, error) {
	var output *awsguardduty.DescribePublishingDestinationOutput
	err := c.recordAPICall(ctx, "DescribePublishingDestination", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribePublishingDestination(callCtx, &awsguardduty.DescribePublishingDestinationInput{
			DestinationId: aws.String(destinationID),
			DetectorId:    aws.String(detectorID),
		})
		return err
	})
	if err != nil {
		return guarddutyservice.PublishingDestination{}, err
	}
	if output == nil {
		return guarddutyservice.PublishingDestination{}, nil
	}
	destinationARN := ""
	if output.DestinationProperties != nil {
		destinationARN = aws.ToString(output.DestinationProperties.DestinationArn)
	}
	return guarddutyservice.PublishingDestination{
		ID:              strings.TrimSpace(aws.ToString(output.DestinationId)),
		DestinationType: string(output.DestinationType),
		Status:          string(output.Status),
		DestinationARN:  strings.TrimSpace(destinationARN),
		Tags:            cloneStringMap(output.Tags),
	}, nil
}

func (c *Client) listThreatIntelSets(ctx context.Context, detectorID string) ([]guarddutyservice.ThreatIntelSet, error) {
	var sets []guarddutyservice.ThreatIntelSet
	var nextToken *string
	for {
		var page *awsguardduty.ListThreatIntelSetsOutput
		err := c.recordAPICall(ctx, "ListThreatIntelSets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListThreatIntelSets(callCtx, &awsguardduty.ListThreatIntelSetsInput{
				DetectorId: aws.String(detectorID),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return sets, nil
		}
		for _, id := range page.ThreatIntelSetIds {
			mapped, err := c.getThreatIntelSet(ctx, detectorID, id)
			if err != nil {
				return nil, err
			}
			sets = append(sets, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return sets, nil
		}
	}
}

func (c *Client) getThreatIntelSet(
	ctx context.Context,
	detectorID string,
	setID string,
) (guarddutyservice.ThreatIntelSet, error) {
	var output *awsguardduty.GetThreatIntelSetOutput
	err := c.recordAPICall(ctx, "GetThreatIntelSet", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetThreatIntelSet(callCtx, &awsguardduty.GetThreatIntelSetInput{
			DetectorId:       aws.String(detectorID),
			ThreatIntelSetId: aws.String(setID),
		})
		return err
	})
	if err != nil {
		return guarddutyservice.ThreatIntelSet{}, err
	}
	if output == nil {
		return guarddutyservice.ThreatIntelSet{ID: strings.TrimSpace(setID)}, nil
	}
	return guarddutyservice.ThreatIntelSet{
		ID:          strings.TrimSpace(setID),
		Name:        strings.TrimSpace(aws.ToString(output.Name)),
		Format:      string(output.Format),
		Status:      string(output.Status),
		LocationARN: strings.TrimSpace(aws.ToString(output.Location)),
		Tags:        cloneStringMap(output.Tags),
	}, nil
}
