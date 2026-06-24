// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsga "github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	gaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/globalaccelerator"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// globalAcceleratorRegion is the single AWS Region that hosts the Global
// Accelerator control plane. Accelerators are global, but every ListXxx call
// must target us-west-2 regardless of the claim region, so the adapter pins the
// SDK client to it.
const globalAcceleratorRegion = "us-west-2"

const (
	listAcceleratorsLimit   int32 = 100
	listListenersLimit      int32 = 100
	listEndpointGroupsLimit int32 = 100
)

type apiClient interface {
	ListAccelerators(
		context.Context,
		*awsga.ListAcceleratorsInput,
		...func(*awsga.Options),
	) (*awsga.ListAcceleratorsOutput, error)
	ListListeners(
		context.Context,
		*awsga.ListListenersInput,
		...func(*awsga.Options),
	) (*awsga.ListListenersOutput, error)
	ListEndpointGroups(
		context.Context,
		*awsga.ListEndpointGroupsInput,
		...func(*awsga.Options),
	) (*awsga.ListEndpointGroupsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsga.ListTagsForResourceInput,
		...func(*awsga.Options),
	) (*awsga.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Global Accelerator control-plane reads into
// scanner-owned metadata. It never mutates a Global Accelerator resource and
// exposes only List operations.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Global Accelerator SDK adapter for one claimed AWS
// boundary. The client region is pinned to us-west-2 because the Global
// Accelerator control plane is reachable only there.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client: awsga.NewFromConfig(config, func(o *awsga.Options) {
			o.Region = globalAcceleratorRegion
		}),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListAccelerators returns Global Accelerator topology metadata, walking
// accelerators, their listeners, and each listener's endpoint groups so the
// scanner sees one nested snapshot.
func (c *Client) ListAccelerators(ctx context.Context) ([]gaservice.Accelerator, error) {
	var accelerators []gaservice.Accelerator
	var token *string
	for {
		var page *awsga.ListAcceleratorsOutput
		err := c.recordAPICall(ctx, "ListAccelerators", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAccelerators(callCtx, &awsga.ListAcceleratorsInput{
				NextToken:  token,
				MaxResults: aws.Int32(listAcceleratorsLimit),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return accelerators, nil
		}
		for _, accelerator := range page.Accelerators {
			mapped, err := c.acceleratorMetadata(ctx, accelerator)
			if err != nil {
				return nil, err
			}
			accelerators = append(accelerators, mapped)
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return accelerators, nil
		}
	}
}

func (c *Client) acceleratorMetadata(
	ctx context.Context,
	accelerator awsgaAccelerator,
) (gaservice.Accelerator, error) {
	acceleratorARN := aws.ToString(accelerator.AcceleratorArn)
	tags, err := c.listTags(ctx, acceleratorARN)
	if err != nil {
		return gaservice.Accelerator{}, err
	}
	listeners, err := c.listListeners(ctx, acceleratorARN)
	if err != nil {
		return gaservice.Accelerator{}, err
	}
	return mapAccelerator(accelerator, listeners, tags), nil
}

func (c *Client) listListeners(ctx context.Context, acceleratorARN string) ([]gaservice.Listener, error) {
	acceleratorARN = strings.TrimSpace(acceleratorARN)
	if acceleratorARN == "" {
		return nil, nil
	}
	var listeners []gaservice.Listener
	var token *string
	for {
		var page *awsga.ListListenersOutput
		err := c.recordAPICall(ctx, "ListListeners", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListListeners(callCtx, &awsga.ListListenersInput{
				AcceleratorArn: aws.String(acceleratorARN),
				NextToken:      token,
				MaxResults:     aws.Int32(listListenersLimit),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return listeners, nil
		}
		for _, listener := range page.Listeners {
			groups, err := c.listEndpointGroups(ctx, aws.ToString(listener.ListenerArn))
			if err != nil {
				return nil, err
			}
			listeners = append(listeners, mapListener(listener, groups))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return listeners, nil
		}
	}
}

func (c *Client) listEndpointGroups(ctx context.Context, listenerARN string) ([]gaservice.EndpointGroup, error) {
	listenerARN = strings.TrimSpace(listenerARN)
	if listenerARN == "" {
		return nil, nil
	}
	var groups []gaservice.EndpointGroup
	var token *string
	for {
		var page *awsga.ListEndpointGroupsOutput
		err := c.recordAPICall(ctx, "ListEndpointGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListEndpointGroups(callCtx, &awsga.ListEndpointGroupsInput{
				ListenerArn: aws.String(listenerARN),
				NextToken:   token,
				MaxResults:  aws.Int32(listEndpointGroupsLimit),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, group := range page.EndpointGroups {
			groups = append(groups, mapEndpointGroup(group))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return groups, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsga.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsga.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return mapTags(output.Tags), nil
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
