// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsguardduty "github.com/aws/aws-sdk-go-v2/service/guardduty"
	gdtypes "github.com/aws/aws-sdk-go-v2/service/guardduty/types"

	guarddutyservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/guardduty"
)

func (c *Client) findingCounts(
	ctx context.Context,
	detectorID string,
	groupBy gdtypes.GroupByType,
) (map[string]int64, error) {
	var output *awsguardduty.GetFindingsStatisticsOutput
	err := c.recordAPICall(ctx, "GetFindingsStatistics", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetFindingsStatistics(callCtx, &awsguardduty.GetFindingsStatisticsInput{
			DetectorId: aws.String(detectorID),
			GroupBy:    groupBy,
			MaxResults: aws.Int32(findingStatisticsLimit),
			OrderBy:    gdtypes.OrderByDesc,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.FindingStatistics == nil {
		return nil, nil
	}
	switch groupBy {
	case gdtypes.GroupByTypeSeverity:
		return severityCounts(output.FindingStatistics.GroupedBySeverity), nil
	case gdtypes.GroupByTypeFindingType:
		return findingTypeCounts(output.FindingStatistics.GroupedByFindingType), nil
	default:
		return nil, nil
	}
}

func severityCounts(values []gdtypes.SeverityStatistics) map[string]int64 {
	if len(values) == 0 {
		return nil
	}
	output := make(map[string]int64, len(values))
	for _, value := range values {
		if value.Severity == nil || value.TotalFindings == nil {
			continue
		}
		key := strconv.FormatFloat(*value.Severity, 'f', -1, 64)
		output[key] = int64(*value.TotalFindings)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func findingTypeCounts(values []gdtypes.FindingTypeStatistics) map[string]int64 {
	if len(values) == 0 {
		return nil
	}
	output := make(map[string]int64, len(values))
	for _, value := range values {
		key := strings.TrimSpace(aws.ToString(value.FindingType))
		if key == "" || value.TotalFindings == nil {
			continue
		}
		output[key] = int64(*value.TotalFindings)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func (c *Client) listMembers(ctx context.Context, detectorID string) ([]guarddutyservice.MemberAccount, error) {
	var members []guarddutyservice.MemberAccount
	var nextToken *string
	for {
		var page *awsguardduty.ListMembersOutput
		err := c.recordAPICall(ctx, "ListMembers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListMembers(callCtx, &awsguardduty.ListMembersInput{
				DetectorId: aws.String(detectorID),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return members, nil
		}
		for _, member := range page.Members {
			members = append(members, guarddutyservice.MemberAccount{
				AccountID:          strings.TrimSpace(aws.ToString(member.AccountId)),
				AdministratorID:    strings.TrimSpace(firstNonEmpty(aws.ToString(member.AdministratorId), aws.ToString(member.MasterId))),
				DetectorID:         strings.TrimSpace(aws.ToString(member.DetectorId)),
				RelationshipStatus: strings.TrimSpace(aws.ToString(member.RelationshipStatus)),
				InvitedAt:          strings.TrimSpace(aws.ToString(member.InvitedAt)),
				UpdatedAt:          strings.TrimSpace(aws.ToString(member.UpdatedAt)),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return members, nil
		}
	}
}

func (c *Client) listFilters(ctx context.Context, detectorID string) ([]guarddutyservice.FilterSummary, error) {
	var filters []guarddutyservice.FilterSummary
	var nextToken *string
	for {
		var page *awsguardduty.ListFiltersOutput
		err := c.recordAPICall(ctx, "ListFilters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFilters(callCtx, &awsguardduty.ListFiltersInput{
				DetectorId: aws.String(detectorID),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return filters, nil
		}
		for _, name := range page.FilterNames {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				filters = append(filters, guarddutyservice.FilterSummary{Name: trimmed})
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return filters, nil
		}
	}
}
