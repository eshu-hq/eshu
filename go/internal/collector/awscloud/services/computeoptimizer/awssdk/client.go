// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsco "github.com/aws/aws-sdk-go-v2/service/computeoptimizer"
	awscotypes "github.com/aws/aws-sdk-go-v2/service/computeoptimizer/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	coservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/computeoptimizer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Compute Optimizer API the
// adapter calls. It is deliberately limited to the recommendation get reads:
// recommendation summaries plus per-resource recommendations for EC2 instances,
// Auto Scaling groups, EBS volumes, and Lambda functions. It exposes no
// enrollment mutation (UpdateEnrollmentStatus), no preference mutation
// (PutRecommendationPreferences / DeleteRecommendationPreferences), and no
// export start, so the adapter cannot mutate Compute Optimizer state or read the
// CloudWatch utilization metric data points behind a recommendation. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type apiClient interface {
	GetRecommendationSummaries(
		context.Context,
		*awsco.GetRecommendationSummariesInput,
		...func(*awsco.Options),
	) (*awsco.GetRecommendationSummariesOutput, error)
	GetEC2InstanceRecommendations(
		context.Context,
		*awsco.GetEC2InstanceRecommendationsInput,
		...func(*awsco.Options),
	) (*awsco.GetEC2InstanceRecommendationsOutput, error)
	GetAutoScalingGroupRecommendations(
		context.Context,
		*awsco.GetAutoScalingGroupRecommendationsInput,
		...func(*awsco.Options),
	) (*awsco.GetAutoScalingGroupRecommendationsOutput, error)
	GetEBSVolumeRecommendations(
		context.Context,
		*awsco.GetEBSVolumeRecommendationsInput,
		...func(*awsco.Options),
	) (*awsco.GetEBSVolumeRecommendationsOutput, error)
	GetLambdaFunctionRecommendations(
		context.Context,
		*awsco.GetLambdaFunctionRecommendationsInput,
		...func(*awsco.Options),
	) (*awsco.GetLambdaFunctionRecommendationsOutput, error)
}

// Client adapts AWS SDK Compute Optimizer control-plane get calls into
// scanner-owned metadata. It never mutates Compute Optimizer state, never
// changes enrollment, and never persists CloudWatch utilization metric data
// points.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Compute Optimizer SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsco.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Compute Optimizer recommendation summaries and per-resource
// recommendations visible to the configured AWS credentials. When the account is
// not opted in to Compute Optimizer (OptInRequiredException or an access-denied
// enrollment failure), Snapshot returns an empty snapshot instead of an error so
// the scan completes cleanly for not-enrolled accounts.
func (c *Client) Snapshot(ctx context.Context) (coservice.Snapshot, error) {
	var snapshot coservice.Snapshot

	summaries, optedOut, err := c.getSummaries(ctx)
	if err != nil {
		return coservice.Snapshot{}, err
	}
	if optedOut {
		return coservice.Snapshot{}, nil
	}
	snapshot.Summaries = summaries

	if snapshot.InstanceRecommendations, err = c.getInstanceRecommendations(ctx); err != nil {
		return coservice.Snapshot{}, err
	}
	if snapshot.AutoScalingGroupRecommendations, err = c.getAutoScalingGroupRecommendations(ctx); err != nil {
		return coservice.Snapshot{}, err
	}
	if snapshot.VolumeRecommendations, err = c.getVolumeRecommendations(ctx); err != nil {
		return coservice.Snapshot{}, err
	}
	if snapshot.LambdaFunctionRecommendations, err = c.getLambdaFunctionRecommendations(ctx); err != nil {
		return coservice.Snapshot{}, err
	}
	return snapshot, nil
}

func (c *Client) getSummaries(ctx context.Context) ([]coservice.RecommendationSummary, bool, error) {
	var summaries []coservice.RecommendationSummary
	var nextToken *string
	for {
		var page *awsco.GetRecommendationSummariesOutput
		err := c.recordAPICall(ctx, "GetRecommendationSummaries", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetRecommendationSummaries(callCtx, &awsco.GetRecommendationSummariesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			if isOptInRequired(err) {
				return nil, true, nil
			}
			return nil, false, err
		}
		if page == nil {
			return summaries, false, nil
		}
		for _, summary := range page.RecommendationSummaries {
			summaries = append(summaries, mapSummary(summary))
		}
		if nextToken = page.NextToken; aws.ToString(nextToken) == "" {
			return summaries, false, nil
		}
	}
}

func (c *Client) getInstanceRecommendations(ctx context.Context) ([]coservice.InstanceRecommendation, error) {
	var recs []coservice.InstanceRecommendation
	var nextToken *string
	for {
		var page *awsco.GetEC2InstanceRecommendationsOutput
		err := c.recordAPICall(ctx, "GetEC2InstanceRecommendations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetEC2InstanceRecommendations(callCtx, &awsco.GetEC2InstanceRecommendationsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			if isOptInRequired(err) {
				return nil, nil
			}
			return nil, err
		}
		if page == nil {
			return recs, nil
		}
		for _, rec := range page.InstanceRecommendations {
			recs = append(recs, mapInstanceRecommendation(rec))
		}
		if nextToken = page.NextToken; aws.ToString(nextToken) == "" {
			return recs, nil
		}
	}
}

func (c *Client) getAutoScalingGroupRecommendations(ctx context.Context) ([]coservice.AutoScalingGroupRecommendation, error) {
	var recs []coservice.AutoScalingGroupRecommendation
	var nextToken *string
	for {
		var page *awsco.GetAutoScalingGroupRecommendationsOutput
		err := c.recordAPICall(ctx, "GetAutoScalingGroupRecommendations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetAutoScalingGroupRecommendations(callCtx, &awsco.GetAutoScalingGroupRecommendationsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			if isOptInRequired(err) {
				return nil, nil
			}
			return nil, err
		}
		if page == nil {
			return recs, nil
		}
		for _, rec := range page.AutoScalingGroupRecommendations {
			recs = append(recs, mapAutoScalingGroupRecommendation(rec))
		}
		if nextToken = page.NextToken; aws.ToString(nextToken) == "" {
			return recs, nil
		}
	}
}

func (c *Client) getVolumeRecommendations(ctx context.Context) ([]coservice.VolumeRecommendation, error) {
	var recs []coservice.VolumeRecommendation
	var nextToken *string
	for {
		var page *awsco.GetEBSVolumeRecommendationsOutput
		err := c.recordAPICall(ctx, "GetEBSVolumeRecommendations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetEBSVolumeRecommendations(callCtx, &awsco.GetEBSVolumeRecommendationsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			if isOptInRequired(err) {
				return nil, nil
			}
			return nil, err
		}
		if page == nil {
			return recs, nil
		}
		for _, rec := range page.VolumeRecommendations {
			recs = append(recs, mapVolumeRecommendation(rec))
		}
		if nextToken = page.NextToken; aws.ToString(nextToken) == "" {
			return recs, nil
		}
	}
}

func (c *Client) getLambdaFunctionRecommendations(ctx context.Context) ([]coservice.LambdaFunctionRecommendation, error) {
	var recs []coservice.LambdaFunctionRecommendation
	var nextToken *string
	for {
		var page *awsco.GetLambdaFunctionRecommendationsOutput
		err := c.recordAPICall(ctx, "GetLambdaFunctionRecommendations", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.GetLambdaFunctionRecommendations(callCtx, &awsco.GetLambdaFunctionRecommendationsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			if isOptInRequired(err) {
				return nil, nil
			}
			return nil, err
		}
		if page == nil {
			return recs, nil
		}
		for _, rec := range page.LambdaFunctionRecommendations {
			recs = append(recs, mapLambdaFunctionRecommendation(rec))
		}
		if nextToken = page.NextToken; aws.ToString(nextToken) == "" {
			return recs, nil
		}
	}
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

// isOptInRequired reports whether err indicates the account is not enrolled in
// Compute Optimizer. AWS surfaces this as OptInRequiredException; an
// AccessDeniedException whose message names the opt-in requirement is treated the
// same so a not-enrolled scan completes cleanly instead of failing.
func isOptInRequired(err error) bool {
	if err == nil {
		return false
	}
	var optIn *awscotypes.OptInRequiredException
	if errors.As(err, &optIn) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "OptInRequiredException" {
			return true
		}
		if code == "AccessDeniedException" &&
			strings.Contains(strings.ToLower(apiErr.ErrorMessage()), "opt in") {
			return true
		}
	}
	return false
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

var _ coservice.Client = (*Client)(nil)

var _ apiClient = (*awsco.Client)(nil)
