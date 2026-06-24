// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsvpclattice "github.com/aws/aws-sdk-go-v2/service/vpclattice"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	vpclatticeservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/vpclattice"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS VPC Lattice API the adapter
// calls. It is deliberately limited to control-plane list reads, plus the
// per-service GetService (for the ACM certificate ARN and DNS entry) and
// per-target-group GetTargetGroup (for the backing VPC and protocol) reads, and
// resource-tag reads. It exposes no GetAuthPolicy, GetResourcePolicy, or any
// Create/Update/Delete/Put/Register/Deregister mutation, so the adapter cannot
// read policy bodies or mutate VPC Lattice state. The exclusion_test reflects
// over this interface to enforce that contract at build time.
type apiClient interface {
	ListServiceNetworks(
		context.Context,
		*awsvpclattice.ListServiceNetworksInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListServiceNetworksOutput, error)
	ListServiceNetworkVpcAssociations(
		context.Context,
		*awsvpclattice.ListServiceNetworkVpcAssociationsInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListServiceNetworkVpcAssociationsOutput, error)
	ListServiceNetworkServiceAssociations(
		context.Context,
		*awsvpclattice.ListServiceNetworkServiceAssociationsInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListServiceNetworkServiceAssociationsOutput, error)
	ListServices(
		context.Context,
		*awsvpclattice.ListServicesInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListServicesOutput, error)
	GetService(
		context.Context,
		*awsvpclattice.GetServiceInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.GetServiceOutput, error)
	ListListeners(
		context.Context,
		*awsvpclattice.ListListenersInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListListenersOutput, error)
	ListTargetGroups(
		context.Context,
		*awsvpclattice.ListTargetGroupsInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListTargetGroupsOutput, error)
	GetTargetGroup(
		context.Context,
		*awsvpclattice.GetTargetGroupInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.GetTargetGroupOutput, error)
	ListTargets(
		context.Context,
		*awsvpclattice.ListTargetsInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListTargetsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsvpclattice.ListTagsForResourceInput,
		...func(*awsvpclattice.Options),
	) (*awsvpclattice.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK VPC Lattice control-plane calls into scanner-owned
// metadata. It never reads auth-policy or resource-policy bodies and never
// calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a VPC Lattice SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsvpclattice.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns VPC Lattice service network, service, and target group
// metadata visible to the configured AWS credentials, each carrying the
// association and target evidence the scanner emits as edges. Auth-policy and
// resource-policy bodies are never read.
func (c *Client) Snapshot(ctx context.Context) (vpclatticeservice.Snapshot, error) {
	networks, err := c.listServiceNetworks(ctx)
	if err != nil {
		return vpclatticeservice.Snapshot{}, err
	}
	services, err := c.listServices(ctx)
	if err != nil {
		return vpclatticeservice.Snapshot{}, err
	}
	targetGroups, err := c.listTargetGroups(ctx)
	if err != nil {
		return vpclatticeservice.Snapshot{}, err
	}
	return vpclatticeservice.Snapshot{
		ServiceNetworks: networks,
		Services:        services,
		TargetGroups:    targetGroups,
	}, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsvpclattice.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListTagsForResource(callCtx, &awsvpclattice.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return callErr
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for key, value := range output.Tags {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		tags[trimmed] = value
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
		code == "TooManyRequestsException"
}

var _ vpclatticeservice.Client = (*Client)(nil)

var _ apiClient = (*awsvpclattice.Client)(nil)
