// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsprofiler "github.com/aws/aws-sdk-go-v2/service/codeguruprofiler"
	awsreviewer "github.com/aws/aws-sdk-go-v2/service/codegurureviewer"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	codeguruservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codeguru"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reviewerAPIClient is the metadata-only subset of the AWS CodeGuru Reviewer API
// the adapter calls. It is limited to the repository-association list/describe
// reads and resource-tag reads. It exposes no ListCodeReviews,
// DescribeCodeReview, ListRecommendations, ListRecommendationFeedback, or any
// Associate/Disassociate/Put mutation, so the adapter cannot read code-review
// findings or recommendation content or mutate CodeGuru Reviewer state. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type reviewerAPIClient interface {
	ListRepositoryAssociations(
		context.Context,
		*awsreviewer.ListRepositoryAssociationsInput,
		...func(*awsreviewer.Options),
	) (*awsreviewer.ListRepositoryAssociationsOutput, error)
	DescribeRepositoryAssociation(
		context.Context,
		*awsreviewer.DescribeRepositoryAssociationInput,
		...func(*awsreviewer.Options),
	) (*awsreviewer.DescribeRepositoryAssociationOutput, error)
}

// profilerAPIClient is the metadata-only subset of the AWS CodeGuru Profiler API
// the adapter calls. It is limited to the profiling-group list read with
// descriptions. It exposes no GetProfile, no ListFindingsReports, no
// ListProfileTimes, no BatchGetFrameMetricData, and no Create/Update/Delete or
// Configure mutation, so the adapter cannot read profiling sample data,
// findings, or flame graphs, or mutate CodeGuru Profiler state.
type profilerAPIClient interface {
	ListProfilingGroups(
		context.Context,
		*awsprofiler.ListProfilingGroupsInput,
		...func(*awsprofiler.Options),
	) (*awsprofiler.ListProfilingGroupsOutput, error)
}

// Client adapts AWS SDK CodeGuru Reviewer and Profiler control-plane calls into
// scanner-owned metadata. It never reads code-review findings, recommendation
// content, profiling samples, flame graphs, or agent telemetry, and never calls
// a mutation API.
type Client struct {
	reviewer    reviewerAPIClient
	profiler    profilerAPIClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CodeGuru SDK adapter for one claimed AWS boundary. It wires
// both the CodeGuru Reviewer and CodeGuru Profiler clients from the shared
// config so the single "codeguru" service_kind reads both control planes.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		reviewer:    awsreviewer.NewFromConfig(config),
		profiler:    awsprofiler.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns CodeGuru Reviewer repository association metadata and CodeGuru
// Profiler profiling-group metadata visible to the configured AWS credentials.
// Findings, recommendations, profiling samples, and flame graphs are never read.
func (c *Client) Snapshot(ctx context.Context) (codeguruservice.Snapshot, error) {
	associations, err := c.listRepositoryAssociations(ctx)
	if err != nil {
		return codeguruservice.Snapshot{}, err
	}
	groups, err := c.listProfilingGroups(ctx)
	if err != nil {
		return codeguruservice.Snapshot{}, err
	}
	return codeguruservice.Snapshot{
		RepositoryAssociations: associations,
		ProfilingGroups:        groups,
	}, nil
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
		code == "TooManyRequestsException"
}

var (
	_ codeguruservice.Client = (*Client)(nil)
	_ reviewerAPIClient      = (*awsreviewer.Client)(nil)
	_ profilerAPIClient      = (*awsprofiler.Client)(nil)
)
