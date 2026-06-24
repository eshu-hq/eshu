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

func (c *Client) listIPSets(ctx context.Context, detectorID string) ([]guarddutyservice.IPSet, error) {
	var sets []guarddutyservice.IPSet
	var nextToken *string
	for {
		var page *awsguardduty.ListIPSetsOutput
		err := c.recordAPICall(ctx, "ListIPSets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListIPSets(callCtx, &awsguardduty.ListIPSetsInput{
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
		for _, id := range page.IpSetIds {
			mapped, err := c.getIPSet(ctx, detectorID, id)
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

func (c *Client) getIPSet(ctx context.Context, detectorID string, setID string) (guarddutyservice.IPSet, error) {
	var output *awsguardduty.GetIPSetOutput
	err := c.recordAPICall(ctx, "GetIPSet", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetIPSet(callCtx, &awsguardduty.GetIPSetInput{
			DetectorId: aws.String(detectorID),
			IpSetId:    aws.String(setID),
		})
		return err
	})
	if err != nil {
		return guarddutyservice.IPSet{}, err
	}
	if output == nil {
		return guarddutyservice.IPSet{ID: strings.TrimSpace(setID)}, nil
	}
	return guarddutyservice.IPSet{
		ID:          strings.TrimSpace(setID),
		Name:        strings.TrimSpace(aws.ToString(output.Name)),
		Format:      string(output.Format),
		Status:      string(output.Status),
		LocationARN: strings.TrimSpace(aws.ToString(output.Location)),
		Tags:        cloneStringMap(output.Tags),
	}, nil
}
