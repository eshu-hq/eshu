// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsguardduty "github.com/aws/aws-sdk-go-v2/service/guardduty"
	gdtypes "github.com/aws/aws-sdk-go-v2/service/guardduty/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	guarddutyservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/guardduty"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const findingStatisticsLimit int32 = 50

type apiClient interface {
	ListDetectors(context.Context, *awsguardduty.ListDetectorsInput, ...func(*awsguardduty.Options)) (*awsguardduty.ListDetectorsOutput, error)
	GetDetector(context.Context, *awsguardduty.GetDetectorInput, ...func(*awsguardduty.Options)) (*awsguardduty.GetDetectorOutput, error)
	GetFindingsStatistics(context.Context, *awsguardduty.GetFindingsStatisticsInput, ...func(*awsguardduty.Options)) (*awsguardduty.GetFindingsStatisticsOutput, error)
	ListMembers(context.Context, *awsguardduty.ListMembersInput, ...func(*awsguardduty.Options)) (*awsguardduty.ListMembersOutput, error)
	ListFilters(context.Context, *awsguardduty.ListFiltersInput, ...func(*awsguardduty.Options)) (*awsguardduty.ListFiltersOutput, error)
	ListPublishingDestinations(context.Context, *awsguardduty.ListPublishingDestinationsInput, ...func(*awsguardduty.Options)) (*awsguardduty.ListPublishingDestinationsOutput, error)
	DescribePublishingDestination(context.Context, *awsguardduty.DescribePublishingDestinationInput, ...func(*awsguardduty.Options)) (*awsguardduty.DescribePublishingDestinationOutput, error)
	ListThreatIntelSets(context.Context, *awsguardduty.ListThreatIntelSetsInput, ...func(*awsguardduty.Options)) (*awsguardduty.ListThreatIntelSetsOutput, error)
	GetThreatIntelSet(context.Context, *awsguardduty.GetThreatIntelSetInput, ...func(*awsguardduty.Options)) (*awsguardduty.GetThreatIntelSetOutput, error)
	ListIPSets(context.Context, *awsguardduty.ListIPSetsInput, ...func(*awsguardduty.Options)) (*awsguardduty.ListIPSetsOutput, error)
	GetIPSet(context.Context, *awsguardduty.GetIPSetInput, ...func(*awsguardduty.Options)) (*awsguardduty.GetIPSetOutput, error)
}

// Client adapts AWS SDK GuardDuty control-plane calls into metadata-only
// scanner records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a GuardDuty SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsguardduty.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDetectors returns metadata-only GuardDuty detector snapshots visible to
// the configured AWS credentials.
func (c *Client) ListDetectors(ctx context.Context) ([]guarddutyservice.Detector, error) {
	ids, err := c.listDetectorIDs(ctx)
	if err != nil {
		return nil, err
	}
	detectors := make([]guarddutyservice.Detector, 0, len(ids))
	for _, id := range ids {
		detector, err := c.detectorMetadata(ctx, id)
		if err != nil {
			return nil, err
		}
		detectors = append(detectors, detector)
	}
	return detectors, nil
}

func (c *Client) listDetectorIDs(ctx context.Context) ([]string, error) {
	var ids []string
	var nextToken *string
	for {
		var page *awsguardduty.ListDetectorsOutput
		err := c.recordAPICall(ctx, "ListDetectors", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDetectors(callCtx, &awsguardduty.ListDetectorsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ids, nil
		}
		for _, id := range page.DetectorIds {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				ids = append(ids, trimmed)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return ids, nil
		}
	}
}

func (c *Client) detectorMetadata(ctx context.Context, detectorID string) (guarddutyservice.Detector, error) {
	detail, err := c.getDetector(ctx, detectorID)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	members, err := c.listMembers(ctx, detectorID)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	filters, err := c.listFilters(ctx, detectorID)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	destinations, err := c.listPublishingDestinations(ctx, detectorID)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	threatSets, err := c.listThreatIntelSets(ctx, detectorID)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	ipSets, err := c.listIPSets(ctx, detectorID)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	severityCounts, err := c.findingCounts(ctx, detectorID, gdtypes.GroupByTypeSeverity)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	typeCounts, err := c.findingCounts(ctx, detectorID, gdtypes.GroupByTypeFindingType)
	if err != nil {
		return guarddutyservice.Detector{}, err
	}
	return guarddutyservice.Detector{
		ID:                         strings.TrimSpace(detectorID),
		Status:                     string(detail.Status),
		FindingPublishingFrequency: string(detail.FindingPublishingFrequency),
		CreatedAt:                  strings.TrimSpace(aws.ToString(detail.CreatedAt)),
		UpdatedAt:                  strings.TrimSpace(aws.ToString(detail.UpdatedAt)),
		Tags:                       cloneStringMap(detail.Tags),
		Features:                   mapFeatures(detail.Features),
		FindingCountsBySeverity:    severityCounts,
		FindingCountsByType:        typeCounts,
		Members:                    members,
		Filters:                    filters,
		PublishingDestinations:     destinations,
		ThreatIntelSets:            threatSets,
		IPSets:                     ipSets,
	}, nil
}

func (c *Client) getDetector(ctx context.Context, detectorID string) (*awsguardduty.GetDetectorOutput, error) {
	var output *awsguardduty.GetDetectorOutput
	err := c.recordAPICall(ctx, "GetDetector", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetDetector(callCtx, &awsguardduty.GetDetectorInput{
			DetectorId: aws.String(detectorID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awsguardduty.GetDetectorOutput{}, nil
	}
	return output, nil
}

func mapFeatures(features []gdtypes.DetectorFeatureConfigurationResult) []guarddutyservice.FeatureConfiguration {
	if len(features) == 0 {
		return nil
	}
	output := make([]guarddutyservice.FeatureConfiguration, 0, len(features))
	for _, feature := range features {
		output = append(output, guarddutyservice.FeatureConfiguration{
			Name:                    string(feature.Name),
			Status:                  string(feature.Status),
			UpdatedAt:               timeToUnix(feature.UpdatedAt),
			AdditionalConfiguration: mapAdditionalFeatures(feature.AdditionalConfiguration),
		})
	}
	return output
}

func mapAdditionalFeatures(features []gdtypes.DetectorAdditionalConfigurationResult) []guarddutyservice.FeatureConfiguration {
	if len(features) == 0 {
		return nil
	}
	output := make([]guarddutyservice.FeatureConfiguration, 0, len(features))
	for _, feature := range features {
		output = append(output, guarddutyservice.FeatureConfiguration{
			Name:      string(feature.Name),
			Status:    string(feature.Status),
			UpdatedAt: timeToUnix(feature.UpdatedAt),
		})
	}
	return output
}

func timeToUnix(value *time.Time) int64 {
	if value == nil || value.IsZero() {
		return 0
	}
	return value.UTC().Unix()
}
