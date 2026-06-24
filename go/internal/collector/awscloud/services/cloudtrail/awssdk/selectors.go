// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"

	cloudtrailservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudtrail"
)

// eventSelectorSummary collects per-trail event selector totals without
// persisting selector bodies. Selector documents (field selectors, equality
// matchers, exclude lists) reveal classification logic for audit events and
// are intentionally excluded from the persisted contract.
func (c *Client) eventSelectorSummary(
	ctx context.Context,
	trailARN string,
) (cloudtrailservice.EventSelectorSummary, error) {
	var output *awscloudtrail.GetEventSelectorsOutput
	err := c.recordAPICall(ctx, "GetEventSelectors", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetEventSelectors(callCtx, &awscloudtrail.GetEventSelectorsInput{
			TrailName: aws.String(trailARN),
		})
		return err
	})
	if err != nil {
		return cloudtrailservice.EventSelectorSummary{}, err
	}
	if output == nil {
		return cloudtrailservice.EventSelectorSummary{}, nil
	}
	return summarizeSelectors(output.EventSelectors, output.AdvancedEventSelectors), nil
}

func summarizeSelectors(
	basic []cttypes.EventSelector,
	advanced []cttypes.AdvancedEventSelector,
) cloudtrailservice.EventSelectorSummary {
	summary := cloudtrailservice.EventSelectorSummary{
		EventSelectorCount:         len(basic),
		AdvancedEventSelectorCount: len(advanced),
	}
	counts := map[string]int{}
	for _, selector := range basic {
		for _, resource := range selector.DataResources {
			rtype := strings.TrimSpace(aws.ToString(resource.Type))
			if rtype == "" {
				continue
			}
			counts[rtype]++
		}
	}
	for _, selector := range advanced {
		for _, field := range selector.FieldSelectors {
			if strings.TrimSpace(aws.ToString(field.Field)) != "resources.type" {
				continue
			}
			for _, value := range field.Equals {
				rtype := strings.TrimSpace(value)
				if rtype == "" {
					continue
				}
				counts[rtype]++
			}
		}
	}
	if len(counts) > 0 {
		summary.ResourceTypeCounts = counts
	}
	return summary
}

// insightSelectorTypes returns the insight type names enabled for a trail.
// Only the insight type name is persisted; CloudTrail reports just a fixed
// enum (ApiCallRateInsight, ApiErrorRateInsight) plus event categories, so
// there is no payload-bearing surface to redact.
func (c *Client) insightSelectorTypes(ctx context.Context, trailARN string) ([]string, error) {
	var output *awscloudtrail.GetInsightSelectorsOutput
	err := c.recordAPICall(ctx, "GetInsightSelectors", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetInsightSelectors(callCtx, &awscloudtrail.GetInsightSelectorsInput{
			TrailName: aws.String(trailARN),
		})
		return err
	})
	if err != nil {
		// Trails without insight selectors return an
		// InsightNotEnabledException, which is normal. Treat the absence as
		// "no insight selectors" rather than a hard error.
		if isInsightNotEnabled(err) {
			return nil, nil
		}
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	if len(output.InsightSelectors) == 0 {
		return nil, nil
	}
	types := make([]string, 0, len(output.InsightSelectors))
	seen := map[string]struct{}{}
	for _, selector := range output.InsightSelectors {
		insightType := strings.TrimSpace(string(selector.InsightType))
		if insightType == "" {
			continue
		}
		if _, ok := seen[insightType]; ok {
			continue
		}
		seen[insightType] = struct{}{}
		types = append(types, insightType)
	}
	if len(types) == 0 {
		return nil, nil
	}
	return types, nil
}

func isInsightNotEnabled(err error) bool {
	if err == nil {
		return false
	}
	var notEnabled *cttypes.InsightNotEnabledException
	return asErr(err, &notEnabled)
}
