// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cloudtrailservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudtrail"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal CloudTrail SDK surface the adapter consumes. It
// deliberately excludes `LookupEvents` (event payload extraction), Lake query
// data-plane APIs (`StartQuery`, `GetQueryResults`, `CancelQuery`,
// `DescribeQuery`), and every mutation API (CreateTrail, UpdateTrail,
// DeleteTrail, StartLogging, StopLogging, PutEventSelectors,
// PutInsightSelectors, Create/Update/Delete EventDataStore/Channel/Dashboard,
// Start/Stop EventDataStoreIngestion, StartDashboardRefresh).
//
// Adding any forbidden method here weakens the security contract; the
// scanner-level guard test
// `TestClientInterfaceExcludesEventPayloadAndMutationAPIs` exists to catch
// such regressions on the scanner-owned `Client` interface.
type apiClient interface {
	ListTrails(context.Context, *awscloudtrail.ListTrailsInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.ListTrailsOutput, error)
	GetTrail(context.Context, *awscloudtrail.GetTrailInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.GetTrailOutput, error)
	GetTrailStatus(context.Context, *awscloudtrail.GetTrailStatusInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.GetTrailStatusOutput, error)
	GetEventSelectors(context.Context, *awscloudtrail.GetEventSelectorsInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.GetEventSelectorsOutput, error)
	GetInsightSelectors(context.Context, *awscloudtrail.GetInsightSelectorsInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.GetInsightSelectorsOutput, error)
	ListEventDataStores(context.Context, *awscloudtrail.ListEventDataStoresInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.ListEventDataStoresOutput, error)
	GetEventDataStore(context.Context, *awscloudtrail.GetEventDataStoreInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.GetEventDataStoreOutput, error)
	ListChannels(context.Context, *awscloudtrail.ListChannelsInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.ListChannelsOutput, error)
	GetChannel(context.Context, *awscloudtrail.GetChannelInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.GetChannelOutput, error)
	ListDashboards(context.Context, *awscloudtrail.ListDashboardsInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.ListDashboardsOutput, error)
	GetDashboard(context.Context, *awscloudtrail.GetDashboardInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.GetDashboardOutput, error)
	ListTags(context.Context, *awscloudtrail.ListTagsInput, ...func(*awscloudtrail.Options)) (*awscloudtrail.ListTagsOutput, error)
}

// Client adapts AWS SDK CloudTrail control-plane calls into metadata-only
// scanner records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CloudTrail SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscloudtrail.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListTrails returns CloudTrail trail configuration snapshots visible to the
// configured AWS credentials. It paginates `ListTrails`, then calls
// `GetTrail`, `GetTrailStatus`, `GetEventSelectors`, and
// `GetInsightSelectors` for each trail. It never calls `LookupEvents` or
// any mutation API.
func (c *Client) ListTrails(ctx context.Context) ([]cloudtrailservice.Trail, error) {
	infos, err := c.listTrailInfos(ctx)
	if err != nil {
		return nil, err
	}
	trails := make([]cloudtrailservice.Trail, 0, len(infos))
	for _, info := range infos {
		trail, err := c.trailMetadata(ctx, info)
		if err != nil {
			return nil, err
		}
		trails = append(trails, trail)
	}
	return trails, nil
}

func (c *Client) listTrailInfos(ctx context.Context) ([]cttypes.TrailInfo, error) {
	var infos []cttypes.TrailInfo
	var nextToken *string
	for {
		var page *awscloudtrail.ListTrailsOutput
		err := c.recordAPICall(ctx, "ListTrails", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTrails(callCtx, &awscloudtrail.ListTrailsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return infos, nil
		}
		infos = append(infos, page.Trails...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return infos, nil
		}
	}
}

func (c *Client) trailMetadata(ctx context.Context, info cttypes.TrailInfo) (cloudtrailservice.Trail, error) {
	trailARN := strings.TrimSpace(aws.ToString(info.TrailARN))
	detail, err := c.getTrail(ctx, trailARN)
	if err != nil {
		return cloudtrailservice.Trail{}, err
	}
	loggingEnabled, latestDeliveryError, latestNotificationError, err := c.getTrailStatus(ctx, trailARN)
	if err != nil {
		return cloudtrailservice.Trail{}, err
	}
	selectorSummary, err := c.eventSelectorSummary(ctx, trailARN)
	if err != nil {
		return cloudtrailservice.Trail{}, err
	}
	insightSelectors, err := c.insightSelectorTypes(ctx, trailARN)
	if err != nil {
		return cloudtrailservice.Trail{}, err
	}
	tags, err := c.tagsFor(ctx, trailARN)
	if err != nil {
		return cloudtrailservice.Trail{}, err
	}
	return mapTrail(detail, trailARN, info, loggingEnabled, latestDeliveryError, latestNotificationError, selectorSummary, insightSelectors, tags), nil
}

func (c *Client) getTrail(ctx context.Context, trailARN string) (*cttypes.Trail, error) {
	var output *awscloudtrail.GetTrailOutput
	err := c.recordAPICall(ctx, "GetTrail", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetTrail(callCtx, &awscloudtrail.GetTrailInput{
			Name: aws.String(trailARN),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.Trail, nil
}

// getTrailStatus reads delivery and logging metadata only. The function
// intentionally drops every field that could reveal event payload content;
// it returns the boolean logging status, the most recent S3 delivery error
// string (operator-visible config drift signal), and the most recent SNS
// notification error string. Event records and CloudWatch Logs delivery
// records are not part of the contract.
func (c *Client) getTrailStatus(ctx context.Context, trailARN string) (bool, string, string, error) {
	var output *awscloudtrail.GetTrailStatusOutput
	err := c.recordAPICall(ctx, "GetTrailStatus", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetTrailStatus(callCtx, &awscloudtrail.GetTrailStatusInput{
			Name: aws.String(trailARN),
		})
		return err
	})
	if err != nil {
		return false, "", "", err
	}
	if output == nil {
		return false, "", "", nil
	}
	return aws.ToBool(output.IsLogging),
		strings.TrimSpace(aws.ToString(output.LatestDeliveryError)),
		strings.TrimSpace(aws.ToString(output.LatestNotificationError)),
		nil
}

func mapTrail(
	detail *cttypes.Trail,
	trailARN string,
	info cttypes.TrailInfo,
	loggingEnabled bool,
	latestDeliveryError string,
	latestNotificationError string,
	selectorSummary cloudtrailservice.EventSelectorSummary,
	insightSelectors []string,
	tags map[string]string,
) cloudtrailservice.Trail {
	trail := cloudtrailservice.Trail{
		ARN:                     trailARN,
		Name:                    strings.TrimSpace(aws.ToString(info.Name)),
		HomeRegion:              strings.TrimSpace(aws.ToString(info.HomeRegion)),
		LoggingEnabled:          loggingEnabled,
		LatestDeliveryError:     latestDeliveryError,
		LatestNotificationError: latestNotificationError,
		EventSelectorSummary:    selectorSummary,
		InsightSelectors:        insightSelectors,
		Tags:                    tags,
	}
	if detail == nil {
		return trail
	}
	trail.Name = firstNonEmpty(trail.Name, strings.TrimSpace(aws.ToString(detail.Name)))
	trail.HomeRegion = firstNonEmpty(trail.HomeRegion, strings.TrimSpace(aws.ToString(detail.HomeRegion)))
	trail.S3BucketName = strings.TrimSpace(aws.ToString(detail.S3BucketName))
	trail.S3KeyPrefix = strings.TrimSpace(aws.ToString(detail.S3KeyPrefix))
	trail.SNSTopicARN = strings.TrimSpace(aws.ToString(detail.SnsTopicARN))
	trail.CloudWatchLogsLogGroupARN = strings.TrimSpace(aws.ToString(detail.CloudWatchLogsLogGroupArn))
	trail.CloudWatchLogsRoleARN = strings.TrimSpace(aws.ToString(detail.CloudWatchLogsRoleArn))
	trail.KMSKeyID = strings.TrimSpace(aws.ToString(detail.KmsKeyId))
	trail.IncludeGlobalServiceEvents = aws.ToBool(detail.IncludeGlobalServiceEvents)
	trail.IsMultiRegionTrail = aws.ToBool(detail.IsMultiRegionTrail)
	trail.IsOrganizationTrail = aws.ToBool(detail.IsOrganizationTrail)
	trail.LogFileValidationEnabled = aws.ToBool(detail.LogFileValidationEnabled)
	trail.HasCustomEventSelectors = aws.ToBool(detail.HasCustomEventSelectors)
	trail.HasInsightSelectors = aws.ToBool(detail.HasInsightSelectors)
	return trail
}

// Compile-time guard: the adapter satisfies the scanner-owned interface and
// the real AWS SDK client matches the apiClient surface.
var _ cloudtrailservice.Client = (*Client)(nil)

var _ apiClient = (*awscloudtrail.Client)(nil)
