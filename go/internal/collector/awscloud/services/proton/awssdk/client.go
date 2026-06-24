// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsproton "github.com/aws/aws-sdk-go-v2/service/proton"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	protonservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/proton"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Proton API the adapter calls.
// It is deliberately limited to environment/service/template list reads, the
// per-service GetService detail read (for source-repository linkage by
// reference), the service-instance list read (for service-in-environment
// placement), and resource-tag reads. It exposes no Create/Update/Delete/Cancel
// mutation and no spec/schema/output reader, so the adapter cannot read or
// persist spec manifest bodies, template schema bodies, or deployment input
// parameter values. The exclusion_test reflects over this interface to enforce
// that contract at build time.
type apiClient interface {
	ListEnvironments(
		context.Context,
		*awsproton.ListEnvironmentsInput,
		...func(*awsproton.Options),
	) (*awsproton.ListEnvironmentsOutput, error)
	ListServices(
		context.Context,
		*awsproton.ListServicesInput,
		...func(*awsproton.Options),
	) (*awsproton.ListServicesOutput, error)
	GetService(
		context.Context,
		*awsproton.GetServiceInput,
		...func(*awsproton.Options),
	) (*awsproton.GetServiceOutput, error)
	ListEnvironmentTemplates(
		context.Context,
		*awsproton.ListEnvironmentTemplatesInput,
		...func(*awsproton.Options),
	) (*awsproton.ListEnvironmentTemplatesOutput, error)
	ListServiceTemplates(
		context.Context,
		*awsproton.ListServiceTemplatesInput,
		...func(*awsproton.Options),
	) (*awsproton.ListServiceTemplatesOutput, error)
	ListServiceInstances(
		context.Context,
		*awsproton.ListServiceInstancesInput,
		...func(*awsproton.Options),
	) (*awsproton.ListServiceInstancesOutput, error)
	ListTagsForResource(
		context.Context,
		*awsproton.ListTagsForResourceInput,
		...func(*awsproton.Options),
	) (*awsproton.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Proton control-plane calls into scanner-owned metadata.
// It never reads or persists spec manifest bodies, pipeline spec bodies,
// template schema bodies, or deployment input parameter values, and never calls
// a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Proton SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsproton.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Proton environment, service, and template metadata visible to
// the configured AWS credentials, plus the service-to-environment placements
// derived from service instances. Spec manifest bodies, template schema bodies,
// and deployment input parameter values are never read.
func (c *Client) Snapshot(ctx context.Context) (protonservice.Snapshot, error) {
	environments, err := c.listEnvironments(ctx)
	if err != nil {
		return protonservice.Snapshot{}, err
	}
	services, err := c.listServices(ctx)
	if err != nil {
		return protonservice.Snapshot{}, err
	}
	environmentTemplates, err := c.listEnvironmentTemplates(ctx)
	if err != nil {
		return protonservice.Snapshot{}, err
	}
	serviceTemplates, err := c.listServiceTemplates(ctx)
	if err != nil {
		return protonservice.Snapshot{}, err
	}
	placements, err := c.listServicePlacements(ctx)
	if err != nil {
		return protonservice.Snapshot{}, err
	}
	return protonservice.Snapshot{
		Environments:         environments,
		Services:             services,
		EnvironmentTemplates: environmentTemplates,
		ServiceTemplates:     serviceTemplates,
		ServicePlacements:    placements,
	}, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var nextToken *string
	for {
		var output *awsproton.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListTagsForResource(callCtx, &awsproton.ListTagsForResourceInput{
				ResourceArn: aws.String(resourceARN),
				NextToken:   nextToken,
			})
			return callErr
		})
		if err != nil || output == nil {
			return nilIfEmpty(tags), err
		}
		for _, tag := range output.Tags {
			key := strings.TrimSpace(aws.ToString(tag.Key))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return nilIfEmpty(tags), nil
		}
	}
}

// recordAPICall wraps each Proton API call in the shared pagination span and AWS
// API-call/throttle counters, mirroring the timestream adapter so Proton reuses
// the existing telemetry contract without adding bespoke metrics.
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

// nilIfEmpty returns nil for an empty tag map so the scanner-owned payload keeps
// omitempty-style behavior.
func nilIfEmpty(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	return tags
}

var _ protonservice.Client = (*Client)(nil)

var _ apiClient = (*awsproton.Client)(nil)
