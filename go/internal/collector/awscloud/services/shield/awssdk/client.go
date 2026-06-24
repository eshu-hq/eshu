// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsshield "github.com/aws/aws-sdk-go-v2/service/shield"
	awsshieldtypes "github.com/aws/aws-sdk-go-v2/service/shield/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	shieldservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/shield"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// shieldRegion is the single AWS Region that hosts the Shield Advanced control
// plane in the commercial partition. Shield is a global service, but its API is
// reachable only from us-east-1, so the adapter pins the SDK client there
// regardless of the claim region.
const shieldRegion = "us-east-1"

// listProtectionsLimit bounds each ListProtections page.
const listProtectionsLimit int32 = 100

// apiClient is the read-only Shield surface the adapter consumes. It exposes
// only List/Describe/Get reads. A reflection gate in exclusion_test.go fails the
// build path if any mutation method is added here.
type apiClient interface {
	ListProtections(
		context.Context,
		*awsshield.ListProtectionsInput,
		...func(*awsshield.Options),
	) (*awsshield.ListProtectionsOutput, error)
	DescribeSubscription(
		context.Context,
		*awsshield.DescribeSubscriptionInput,
		...func(*awsshield.Options),
	) (*awsshield.DescribeSubscriptionOutput, error)
	GetSubscriptionState(
		context.Context,
		*awsshield.GetSubscriptionStateInput,
		...func(*awsshield.Options),
	) (*awsshield.GetSubscriptionStateOutput, error)
}

// Client adapts AWS SDK for Go v2 Shield Advanced reads into scanner-owned
// metadata. It reads protections and the subscription state and auto-renew flag
// only; it never reads subscription limits, time commitment, or any other
// billing detail, and it never calls a Shield mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Shield Advanced SDK adapter for one claimed AWS boundary.
// The client region is pinned to us-east-1 because the Shield control plane is
// reachable only there.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client: awsshield.NewFromConfig(config, func(o *awsshield.Options) {
			o.Region = shieldRegion
		}),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListProtections returns every Shield Advanced protection visible to the
// claimed account, paginating on NextToken. Each record carries the protection
// ARN, id, name, and protected resource ARN.
func (c *Client) ListProtections(ctx context.Context) ([]shieldservice.Protection, error) {
	var protections []shieldservice.Protection
	var token *string
	for {
		var page *awsshield.ListProtectionsOutput
		err := c.recordAPICall(ctx, "ListProtections", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListProtections(callCtx, &awsshield.ListProtectionsInput{
				NextToken:  token,
				MaxResults: aws.Int32(listProtectionsLimit),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return protections, nil
		}
		for _, protection := range page.Protections {
			protections = append(protections, mapProtection(protection))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return protections, nil
		}
	}
}

// DescribeSubscription returns the per-account Shield Advanced subscription
// summary, or nil when the account has no active subscription
// (ResourceNotFoundException). Only the state and auto-renew fields are
// populated; subscription limits and billing detail are intentionally dropped.
func (c *Client) DescribeSubscription(ctx context.Context) (*shieldservice.Subscription, error) {
	var output *awsshield.DescribeSubscriptionOutput
	err := c.recordAPICall(ctx, "DescribeSubscription", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeSubscription(callCtx, &awsshield.DescribeSubscriptionInput{})
		return err
	})
	if err != nil {
		if isResourceNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if output == nil || output.Subscription == nil {
		return nil, nil
	}
	state, err := c.subscriptionState(ctx)
	if err != nil {
		return nil, err
	}
	return mapSubscription(output.Subscription, state), nil
}

// subscriptionState reads the canonical subscription state through
// GetSubscriptionState, which returns the state enum only with no billing data.
func (c *Client) subscriptionState(ctx context.Context) (string, error) {
	var output *awsshield.GetSubscriptionStateOutput
	err := c.recordAPICall(ctx, "GetSubscriptionState", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetSubscriptionState(callCtx, &awsshield.GetSubscriptionStateInput{})
		return err
	})
	if err != nil {
		return "", err
	}
	if output == nil {
		return "", nil
	}
	return string(output.SubscriptionState), nil
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

func isResourceNotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound *awsshieldtypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

func isThrottleError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := strings.ToLower(apiErr.ErrorCode())
	return strings.Contains(code, "throttl") || strings.Contains(code, "rate")
}

var _ shieldservice.Client = (*Client)(nil)
