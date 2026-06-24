// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskav2 "github.com/aws/aws-sdk-go-v2/service/kinesisanalyticsv2"
	awskav2types "github.com/aws/aws-sdk-go-v2/service/kinesisanalyticsv2/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	kav2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kinesisanalyticsv2"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Managed Service for Apache
// Flink (Kinesis Data Analytics v2) API the adapter calls. It is deliberately
// limited to the application list/describe reads, the snapshot list read, and
// the resource-tag read. It exposes no Create/Update/Delete/Start/Stop/Add/
// Rollback mutation and no code or run-configuration writer, so the adapter
// cannot mutate application state. The exclusion_test reflects over this
// interface to enforce that contract at build time.
type apiClient interface {
	ListApplications(
		context.Context,
		*awskav2.ListApplicationsInput,
		...func(*awskav2.Options),
	) (*awskav2.ListApplicationsOutput, error)
	DescribeApplication(
		context.Context,
		*awskav2.DescribeApplicationInput,
		...func(*awskav2.Options),
	) (*awskav2.DescribeApplicationOutput, error)
	ListApplicationSnapshots(
		context.Context,
		*awskav2.ListApplicationSnapshotsInput,
		...func(*awskav2.Options),
	) (*awskav2.ListApplicationSnapshotsOutput, error)
	ListTagsForResource(
		context.Context,
		*awskav2.ListTagsForResourceInput,
		...func(*awskav2.Options),
	) (*awskav2.ListTagsForResourceOutput, error)
}

// Client adapts the AWS SDK Managed Flink control-plane reads into the
// scanner-owned metadata-only Client interface. It lists applications, describes
// each application, lists each application's snapshots (names and status only),
// and reads each application's tags. It never reads or persists application code
// bodies, SQL text, environment property values, run-configuration content, or
// record payloads, and never mutates application state.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Managed Flink SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awskav2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListApplications lists every Managed Flink application in the boundary with
// the paginated ListApplications API, then describes each application and reads
// its snapshots and tags, mapping the results into scanner-owned metadata.
func (c *Client) ListApplications(ctx context.Context) ([]kav2service.Application, error) {
	summaries, err := c.listApplicationSummaries(ctx)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, nil
	}
	applications := make([]kav2service.Application, 0, len(summaries))
	for _, summary := range summaries {
		name := strings.TrimSpace(aws.ToString(summary.ApplicationName))
		if name == "" {
			continue
		}
		detail, err := c.describeApplication(ctx, name)
		if err != nil {
			return nil, err
		}
		if detail == nil {
			continue
		}
		application := mapApplication(detail)
		snapshots, err := c.listSnapshots(ctx, name)
		if err != nil {
			return nil, err
		}
		application.Snapshots = snapshots
		tags, err := c.listTags(ctx, application.ARN)
		if err != nil {
			return nil, err
		}
		application.Tags = tags
		applications = append(applications, application)
	}
	return applications, nil
}

func (c *Client) listApplicationSummaries(ctx context.Context) ([]awskav2types.ApplicationSummary, error) {
	var summaries []awskav2types.ApplicationSummary
	var nextToken *string
	for {
		var page *awskav2.ListApplicationsOutput
		err := c.recordAPICall(ctx, "ListApplications", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListApplications(callCtx, &awskav2.ListApplicationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return summaries, nil
		}
		summaries = append(summaries, page.ApplicationSummaries...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

func (c *Client) describeApplication(ctx context.Context, name string) (*awskav2types.ApplicationDetail, error) {
	var output *awskav2.DescribeApplicationOutput
	err := c.recordAPICall(ctx, "DescribeApplication", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeApplication(callCtx, &awskav2.DescribeApplicationInput{
			ApplicationName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.ApplicationDetail, nil
}

func (c *Client) listSnapshots(ctx context.Context, name string) ([]kav2service.Snapshot, error) {
	var snapshots []kav2service.Snapshot
	var nextToken *string
	for {
		var page *awskav2.ListApplicationSnapshotsOutput
		err := c.recordAPICall(ctx, "ListApplicationSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListApplicationSnapshots(callCtx, &awskav2.ListApplicationSnapshotsInput{
				ApplicationName: aws.String(name),
				NextToken:       nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return snapshots, nil
		}
		for _, summary := range page.SnapshotSummaries {
			snapshotName := strings.TrimSpace(aws.ToString(summary.SnapshotName))
			if snapshotName == "" {
				continue
			}
			snapshots = append(snapshots, kav2service.Snapshot{
				Name:                 snapshotName,
				Status:               strings.TrimSpace(string(summary.SnapshotStatus)),
				ApplicationVersionID: aws.ToInt64(summary.ApplicationVersionId),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return snapshots, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awskav2.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awskav2.ListTagsForResourceInput{
			ResourceARN: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for _, tag := range output.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException" ||
		code == "LimitExceededException"
}

var _ kav2service.Client = (*Client)(nil)

var _ apiClient = (*awskav2.Client)(nil)
