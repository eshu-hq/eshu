// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	verifiedaccessservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/verifiedaccess"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS EC2 API the adapter calls for
// Verified Access. It is deliberately limited to the four Verified Access
// Describe reads. It exposes no Create/Modify/Delete mutation, no policy-body
// read (GetVerifiedAccessGroupPolicy / GetVerifiedAccessEndpointPolicy are
// intentionally excluded), and no data-plane operation, so the adapter cannot
// mutate Verified Access state or read policy documents or trust-provider
// secrets. The exclusion_test reflects over this interface to enforce that
// contract at build time.
type apiClient interface {
	DescribeVerifiedAccessInstances(
		context.Context,
		*awsec2.DescribeVerifiedAccessInstancesInput,
		...func(*awsec2.Options),
	) (*awsec2.DescribeVerifiedAccessInstancesOutput, error)
	DescribeVerifiedAccessGroups(
		context.Context,
		*awsec2.DescribeVerifiedAccessGroupsInput,
		...func(*awsec2.Options),
	) (*awsec2.DescribeVerifiedAccessGroupsOutput, error)
	DescribeVerifiedAccessEndpoints(
		context.Context,
		*awsec2.DescribeVerifiedAccessEndpointsInput,
		...func(*awsec2.Options),
	) (*awsec2.DescribeVerifiedAccessEndpointsOutput, error)
	DescribeVerifiedAccessTrustProviders(
		context.Context,
		*awsec2.DescribeVerifiedAccessTrustProvidersInput,
		...func(*awsec2.Options),
	) (*awsec2.DescribeVerifiedAccessTrustProvidersOutput, error)
}

// Client adapts AWS SDK EC2 Verified Access control-plane calls into
// scanner-owned metadata. It never mutates Verified Access state, never reads
// group/endpoint policy documents, and never reads or persists trust-provider
// client secrets.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Verified Access SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsec2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Verified Access instance, group, endpoint, and trust-provider
// metadata visible to the configured AWS credentials in the boundary region.
func (c *Client) Snapshot(ctx context.Context) (verifiedaccessservice.Snapshot, error) {
	instances, err := c.listInstances(ctx)
	if err != nil {
		return verifiedaccessservice.Snapshot{}, err
	}
	trustProviders, err := c.listTrustProviders(ctx)
	if err != nil {
		return verifiedaccessservice.Snapshot{}, err
	}
	groups, err := c.listGroups(ctx)
	if err != nil {
		return verifiedaccessservice.Snapshot{}, err
	}
	endpoints, err := c.listEndpoints(ctx)
	if err != nil {
		return verifiedaccessservice.Snapshot{}, err
	}
	return verifiedaccessservice.Snapshot{
		Instances:      instances,
		Groups:         groups,
		Endpoints:      endpoints,
		TrustProviders: trustProviders,
	}, nil
}

func (c *Client) listInstances(ctx context.Context) ([]verifiedaccessservice.Instance, error) {
	var instances []verifiedaccessservice.Instance
	var nextToken *string
	for {
		var page *awsec2.DescribeVerifiedAccessInstancesOutput
		err := c.recordAPICall(ctx, "DescribeVerifiedAccessInstances", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeVerifiedAccessInstances(callCtx, &awsec2.DescribeVerifiedAccessInstancesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return instances, nil
		}
		for _, instance := range page.VerifiedAccessInstances {
			instances = append(instances, mapInstance(instance))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return instances, nil
		}
	}
}

func (c *Client) listTrustProviders(ctx context.Context) ([]verifiedaccessservice.TrustProvider, error) {
	var trustProviders []verifiedaccessservice.TrustProvider
	var nextToken *string
	for {
		var page *awsec2.DescribeVerifiedAccessTrustProvidersOutput
		err := c.recordAPICall(ctx, "DescribeVerifiedAccessTrustProviders", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeVerifiedAccessTrustProviders(callCtx, &awsec2.DescribeVerifiedAccessTrustProvidersInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return trustProviders, nil
		}
		for _, trustProvider := range page.VerifiedAccessTrustProviders {
			trustProviders = append(trustProviders, mapTrustProvider(trustProvider))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return trustProviders, nil
		}
	}
}

func (c *Client) listGroups(ctx context.Context) ([]verifiedaccessservice.Group, error) {
	var groups []verifiedaccessservice.Group
	var nextToken *string
	for {
		var page *awsec2.DescribeVerifiedAccessGroupsOutput
		err := c.recordAPICall(ctx, "DescribeVerifiedAccessGroups", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeVerifiedAccessGroups(callCtx, &awsec2.DescribeVerifiedAccessGroupsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, group := range page.VerifiedAccessGroups {
			groups = append(groups, mapGroup(group))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return groups, nil
		}
	}
}

func (c *Client) listEndpoints(ctx context.Context) ([]verifiedaccessservice.Endpoint, error) {
	var endpoints []verifiedaccessservice.Endpoint
	var nextToken *string
	for {
		var page *awsec2.DescribeVerifiedAccessEndpointsOutput
		err := c.recordAPICall(ctx, "DescribeVerifiedAccessEndpoints", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeVerifiedAccessEndpoints(callCtx, &awsec2.DescribeVerifiedAccessEndpointsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return endpoints, nil
		}
		for _, endpoint := range page.VerifiedAccessEndpoints {
			endpoints = append(endpoints, mapEndpoint(endpoint))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return endpoints, nil
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

var _ verifiedaccessservice.Client = (*Client)(nil)

var _ apiClient = (*awsec2.Client)(nil)
