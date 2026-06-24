// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsresiliencehub "github.com/aws/aws-sdk-go-v2/service/resiliencehub"
	awsresiliencehubtypes "github.com/aws/aws-sdk-go-v2/service/resiliencehub/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	resiliencehubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/resiliencehub"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// publishedAppVersion is the Resilience Hub published application version label.
// Version-scoped reads (input sources, components, physical resources) require a
// version; the published "release" version is the assessed snapshot, so the
// adapter reads it and records a warning when it is absent rather than failing.
const publishedAppVersion = "release"

// apiClient is the metadata-only subset of the AWS Resilience Hub API the
// adapter calls. It is deliberately limited to application/policy/component/
// input-source/assessment list reads, the per-application describe read, the
// published-version physical-resource list, and resource-tag reads. It exposes
// no create/update/delete mutation, no resource import, no assessment start, and
// no assessment result, drift, or recommendation reader, so the adapter cannot
// mutate Resilience Hub state or read assessment payloads. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	ListApps(
		context.Context,
		*awsresiliencehub.ListAppsInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.ListAppsOutput, error)
	DescribeApp(
		context.Context,
		*awsresiliencehub.DescribeAppInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.DescribeAppOutput, error)
	ListResiliencyPolicies(
		context.Context,
		*awsresiliencehub.ListResiliencyPoliciesInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.ListResiliencyPoliciesOutput, error)
	ListAppInputSources(
		context.Context,
		*awsresiliencehub.ListAppInputSourcesInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.ListAppInputSourcesOutput, error)
	ListAppVersionAppComponents(
		context.Context,
		*awsresiliencehub.ListAppVersionAppComponentsInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.ListAppVersionAppComponentsOutput, error)
	ListAppVersionResources(
		context.Context,
		*awsresiliencehub.ListAppVersionResourcesInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.ListAppVersionResourcesOutput, error)
	ListAppAssessments(
		context.Context,
		*awsresiliencehub.ListAppAssessmentsInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.ListAppAssessmentsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsresiliencehub.ListTagsForResourceInput,
		...func(*awsresiliencehub.Options),
	) (*awsresiliencehub.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Resilience Hub control-plane calls into scanner-owned
// metadata. It never reads assessment result bodies, drift detail, or
// recommendation contents, and never calls a mutation, import, or
// assessment-start API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Resilience Hub SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsresiliencehub.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Resilience Hub application and resiliency policy metadata
// visible to the configured AWS credentials. Assessment result bodies, drift
// detail, and recommendation contents are never read.
func (c *Client) Snapshot(ctx context.Context) (resiliencehubservice.Snapshot, error) {
	policies, err := c.listPolicies(ctx)
	if err != nil {
		return resiliencehubservice.Snapshot{}, err
	}
	apps, warnings, err := c.listApps(ctx)
	if err != nil {
		return resiliencehubservice.Snapshot{}, err
	}
	return resiliencehubservice.Snapshot{
		Apps:     apps,
		Policies: policies,
		Warnings: warnings,
	}, nil
}

func (c *Client) listPolicies(ctx context.Context) ([]resiliencehubservice.ResiliencyPolicy, error) {
	var policies []resiliencehubservice.ResiliencyPolicy
	var nextToken *string
	for {
		var page *awsresiliencehub.ListResiliencyPoliciesOutput
		err := c.recordAPICall(ctx, "ListResiliencyPolicies", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListResiliencyPolicies(callCtx, &awsresiliencehub.ListResiliencyPoliciesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return policies, nil
		}
		for _, policy := range page.ResiliencyPolicies {
			mapped, mapErr := c.mapPolicy(ctx, policy)
			if mapErr != nil {
				return nil, mapErr
			}
			policies = append(policies, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return policies, nil
		}
	}
}

func (c *Client) mapPolicy(
	ctx context.Context,
	policy awsresiliencehubtypes.ResiliencyPolicy,
) (resiliencehubservice.ResiliencyPolicy, error) {
	arn := strings.TrimSpace(aws.ToString(policy.PolicyArn))
	tags := cloneTags(policy.Tags)
	if len(tags) == 0 && arn != "" {
		fetched, err := c.listTags(ctx, arn)
		if err != nil {
			return resiliencehubservice.ResiliencyPolicy{}, err
		}
		tags = fetched
	}
	return resiliencehubservice.ResiliencyPolicy{
		ARN:                    arn,
		Name:                   strings.TrimSpace(aws.ToString(policy.PolicyName)),
		Description:            strings.TrimSpace(aws.ToString(policy.PolicyDescription)),
		Tier:                   strings.TrimSpace(string(policy.Tier)),
		EstimatedCostTier:      strings.TrimSpace(string(policy.EstimatedCostTier)),
		DataLocationConstraint: strings.TrimSpace(string(policy.DataLocationConstraint)),
		FailureTargets:         mapFailurePolicy(policy.Policy),
		CreationTime:           aws.ToTime(policy.CreationTime),
		Tags:                   tags,
	}, nil
}

func mapFailurePolicy(
	policy map[string]awsresiliencehubtypes.FailurePolicy,
) map[string]resiliencehubservice.FailureTarget {
	if len(policy) == 0 {
		return nil
	}
	targets := make(map[string]resiliencehubservice.FailureTarget, len(policy))
	for key, failure := range policy {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		targets[trimmed] = resiliencehubservice.FailureTarget{
			RPOInSecs: failure.RpoInSecs,
			RTOInSecs: failure.RtoInSecs,
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return targets
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsresiliencehub.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListTagsForResource(callCtx, &awsresiliencehub.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return callErr
	})
	if err != nil || output == nil {
		return nil, err
	}
	return cloneTags(output.Tags), nil
}

func cloneTags(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	tags := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		tags[trimmed] = value
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
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

func isResourceNotFound(err error) bool {
	var notFound *awsresiliencehubtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

var _ resiliencehubservice.Client = (*Client)(nil)

var _ apiClient = (*awsresiliencehub.Client)(nil)
