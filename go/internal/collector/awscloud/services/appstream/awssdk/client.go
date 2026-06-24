// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappstream "github.com/aws/aws-sdk-go-v2/service/appstream"
	awsappstreamtypes "github.com/aws/aws-sdk-go-v2/service/appstream/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	appstreamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appstream"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS AppStream 2.0 API the adapter
// calls. It is deliberately limited to the describe/list control-plane reads for
// fleets, stacks, image builders, images, fleet-stack associations, and resource
// tags. It exposes no CreateFleet/DeleteStack/StartFleet/StopFleet or any other
// mutation, no CreateStreamingURL (which mints a session credential), and no
// session/user describe (DescribeSessions, DescribeUsers,
// DescribeUserStackAssociations), so the adapter cannot mutate AppStream state
// or read streaming-session, user, or credential content. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	DescribeFleets(
		context.Context,
		*awsappstream.DescribeFleetsInput,
		...func(*awsappstream.Options),
	) (*awsappstream.DescribeFleetsOutput, error)
	DescribeStacks(
		context.Context,
		*awsappstream.DescribeStacksInput,
		...func(*awsappstream.Options),
	) (*awsappstream.DescribeStacksOutput, error)
	DescribeImageBuilders(
		context.Context,
		*awsappstream.DescribeImageBuildersInput,
		...func(*awsappstream.Options),
	) (*awsappstream.DescribeImageBuildersOutput, error)
	DescribeImages(
		context.Context,
		*awsappstream.DescribeImagesInput,
		...func(*awsappstream.Options),
	) (*awsappstream.DescribeImagesOutput, error)
	ListAssociatedStacks(
		context.Context,
		*awsappstream.ListAssociatedStacksInput,
		...func(*awsappstream.Options),
	) (*awsappstream.ListAssociatedStacksOutput, error)
	ListTagsForResource(
		context.Context,
		*awsappstream.ListTagsForResourceInput,
		...func(*awsappstream.Options),
	) (*awsappstream.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK AppStream 2.0 control-plane calls into scanner-owned
// metadata. It never mutates AppStream state, never mints a streaming URL, and
// never reads streaming-session, user, or session-script content.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AppStream SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsappstream.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns AppStream fleet, stack, image builder, and image metadata
// plus the reported fleet-to-stack associations visible to the configured AWS
// credentials. Streaming sessions, user data, and session scripts are never
// read; no mutation API is ever called.
func (c *Client) Snapshot(ctx context.Context) (appstreamservice.Snapshot, error) {
	fleets, err := c.describeFleets(ctx)
	if err != nil {
		return appstreamservice.Snapshot{}, err
	}
	stacks, err := c.describeStacks(ctx)
	if err != nil {
		return appstreamservice.Snapshot{}, err
	}
	builders, err := c.describeImageBuilders(ctx)
	if err != nil {
		return appstreamservice.Snapshot{}, err
	}
	images, err := c.describeImages(ctx)
	if err != nil {
		return appstreamservice.Snapshot{}, err
	}
	associations, err := c.fleetStackAssociations(ctx, fleets)
	if err != nil {
		return appstreamservice.Snapshot{}, err
	}
	return appstreamservice.Snapshot{
		Fleets:                 fleets,
		Stacks:                 stacks,
		ImageBuilders:          builders,
		Images:                 images,
		FleetStackAssociations: associations,
	}, nil
}

func (c *Client) describeFleets(ctx context.Context) ([]appstreamservice.Fleet, error) {
	var fleets []appstreamservice.Fleet
	var nextToken *string
	for {
		var page *awsappstream.DescribeFleetsOutput
		err := c.recordAPICall(ctx, "DescribeFleets", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeFleets(callCtx, &awsappstream.DescribeFleetsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return fleets, nil
		}
		for _, fleet := range page.Fleets {
			mapped, mapErr := c.mapFleet(ctx, fleet)
			if mapErr != nil {
				return nil, mapErr
			}
			fleets = append(fleets, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return fleets, nil
		}
	}
}

func (c *Client) describeStacks(ctx context.Context) ([]appstreamservice.Stack, error) {
	var stacks []appstreamservice.Stack
	var nextToken *string
	for {
		var page *awsappstream.DescribeStacksOutput
		err := c.recordAPICall(ctx, "DescribeStacks", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeStacks(callCtx, &awsappstream.DescribeStacksInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return stacks, nil
		}
		for _, stack := range page.Stacks {
			mapped, mapErr := c.mapStack(ctx, stack)
			if mapErr != nil {
				return nil, mapErr
			}
			stacks = append(stacks, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return stacks, nil
		}
	}
}

func (c *Client) describeImageBuilders(ctx context.Context) ([]appstreamservice.ImageBuilder, error) {
	var builders []appstreamservice.ImageBuilder
	var nextToken *string
	for {
		var page *awsappstream.DescribeImageBuildersOutput
		err := c.recordAPICall(ctx, "DescribeImageBuilders", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.DescribeImageBuilders(callCtx, &awsappstream.DescribeImageBuildersInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return builders, nil
		}
		for _, builder := range page.ImageBuilders {
			mapped, mapErr := c.mapImageBuilder(ctx, builder)
			if mapErr != nil {
				return nil, mapErr
			}
			builders = append(builders, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return builders, nil
		}
	}
}

// describeImages reads PRIVATE and SHARED AppStream images only. The PUBLIC
// visibility space is the AWS-managed base-image catalog (thousands of entries
// the account does not own); scanning it would emit catalog noise and never
// correspond to a customer resource, so the adapter scopes the read to the two
// account-scoped visibilities.
func (c *Client) describeImages(ctx context.Context) ([]appstreamservice.Image, error) {
	var images []appstreamservice.Image
	for _, visibility := range []awsappstreamtypes.VisibilityType{
		awsappstreamtypes.VisibilityTypePrivate,
		awsappstreamtypes.VisibilityTypeShared,
	} {
		var nextToken *string
		for {
			var page *awsappstream.DescribeImagesOutput
			err := c.recordAPICall(ctx, "DescribeImages", func(callCtx context.Context) error {
				var callErr error
				page, callErr = c.client.DescribeImages(callCtx, &awsappstream.DescribeImagesInput{
					Type:      visibility,
					NextToken: nextToken,
				})
				return callErr
			})
			if err != nil {
				return nil, err
			}
			if page == nil {
				break
			}
			for _, image := range page.Images {
				mapped, mapErr := c.mapImage(ctx, image)
				if mapErr != nil {
					return nil, mapErr
				}
				images = append(images, mapped)
			}
			nextToken = page.NextToken
			if aws.ToString(nextToken) == "" {
				break
			}
		}
	}
	return images, nil
}

// fleetStackAssociations reads the stacks associated with each fleet via
// ListAssociatedStacks (fleet -> stack names). This single direction is
// sufficient: every association is a (fleet, stack) pair, so reading it once per
// fleet captures the full bipartite set without the redundant reverse
// ListAssociatedFleets pass.
func (c *Client) fleetStackAssociations(
	ctx context.Context,
	fleets []appstreamservice.Fleet,
) ([]appstreamservice.FleetStackAssociation, error) {
	var associations []appstreamservice.FleetStackAssociation
	for _, fleet := range fleets {
		fleetName := strings.TrimSpace(fleet.Name)
		if fleetName == "" {
			continue
		}
		var nextToken *string
		for {
			var page *awsappstream.ListAssociatedStacksOutput
			err := c.recordAPICall(ctx, "ListAssociatedStacks", func(callCtx context.Context) error {
				var callErr error
				page, callErr = c.client.ListAssociatedStacks(callCtx, &awsappstream.ListAssociatedStacksInput{
					FleetName: aws.String(fleetName),
					NextToken: nextToken,
				})
				return callErr
			})
			if err != nil {
				return nil, err
			}
			if page == nil {
				break
			}
			for _, stackName := range page.Names {
				if trimmed := strings.TrimSpace(stackName); trimmed != "" {
					associations = append(associations, appstreamservice.FleetStackAssociation{
						FleetName: fleetName,
						StackName: trimmed,
					})
				}
			}
			nextToken = page.NextToken
			if aws.ToString(nextToken) == "" {
				break
			}
		}
	}
	return associations, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsappstream.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListTagsForResource(callCtx, &awsappstream.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return callErr
	})
	if err != nil || output == nil || len(output.Tags) == 0 {
		return nil, err
	}
	tags := make(map[string]string, len(output.Tags))
	for key, value := range output.Tags {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			tags[trimmed] = value
		}
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

var _ appstreamservice.Client = (*Client)(nil)

var _ apiClient = (*awsappstream.Client)(nil)
