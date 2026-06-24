// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslocation "github.com/aws/aws-sdk-go-v2/service/location"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	locationservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/location"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Location Service API the
// adapter calls. It is deliberately limited to the resource list reads, the
// per-resource describe reads (which also return resource tags inline), and the
// tracker consumer association list. It exposes no device-position read
// (ListDevicePositions, GetDevicePosition*, BatchGetDevicePosition), no geofence
// read (ListGeofences, GetGeofence), no place search (SearchPlaceIndexFor*,
// GetPlace), no route calculation (CalculateRoute*), no map-tile read
// (GetMapTile/Glyphs/Sprites/StyleDescriptor), and no Create/Update/Delete/Put/
// Associate mutation, so the adapter cannot read data-plane payloads or mutate
// Location Service state. The exclusion_test reflects over this interface to
// enforce that contract at build time.
type apiClient interface {
	ListMaps(
		context.Context, *awslocation.ListMapsInput, ...func(*awslocation.Options),
	) (*awslocation.ListMapsOutput, error)
	DescribeMap(
		context.Context, *awslocation.DescribeMapInput, ...func(*awslocation.Options),
	) (*awslocation.DescribeMapOutput, error)
	ListPlaceIndexes(
		context.Context, *awslocation.ListPlaceIndexesInput, ...func(*awslocation.Options),
	) (*awslocation.ListPlaceIndexesOutput, error)
	DescribePlaceIndex(
		context.Context, *awslocation.DescribePlaceIndexInput, ...func(*awslocation.Options),
	) (*awslocation.DescribePlaceIndexOutput, error)
	ListTrackers(
		context.Context, *awslocation.ListTrackersInput, ...func(*awslocation.Options),
	) (*awslocation.ListTrackersOutput, error)
	DescribeTracker(
		context.Context, *awslocation.DescribeTrackerInput, ...func(*awslocation.Options),
	) (*awslocation.DescribeTrackerOutput, error)
	ListTrackerConsumers(
		context.Context, *awslocation.ListTrackerConsumersInput, ...func(*awslocation.Options),
	) (*awslocation.ListTrackerConsumersOutput, error)
	ListGeofenceCollections(
		context.Context, *awslocation.ListGeofenceCollectionsInput, ...func(*awslocation.Options),
	) (*awslocation.ListGeofenceCollectionsOutput, error)
	DescribeGeofenceCollection(
		context.Context, *awslocation.DescribeGeofenceCollectionInput, ...func(*awslocation.Options),
	) (*awslocation.DescribeGeofenceCollectionOutput, error)
	ListRouteCalculators(
		context.Context, *awslocation.ListRouteCalculatorsInput, ...func(*awslocation.Options),
	) (*awslocation.ListRouteCalculatorsOutput, error)
	DescribeRouteCalculator(
		context.Context, *awslocation.DescribeRouteCalculatorInput, ...func(*awslocation.Options),
	) (*awslocation.DescribeRouteCalculatorOutput, error)
}

// Client adapts AWS SDK Location Service control-plane calls into scanner-owned
// metadata. It never reads device positions, geofence geometries, place-search
// results, route calculations, or map tiles, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Location Service SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awslocation.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Location Service map, place index, tracker, geofence
// collection, and route calculator metadata visible to the configured AWS
// credentials. Device positions, geofence geometries, place-search results,
// route calculations, and map tiles are never read.
func (c *Client) Snapshot(ctx context.Context) (locationservice.Snapshot, error) {
	maps, err := c.listMaps(ctx)
	if err != nil {
		return locationservice.Snapshot{}, err
	}
	indexes, err := c.listPlaceIndexes(ctx)
	if err != nil {
		return locationservice.Snapshot{}, err
	}
	trackers, err := c.listTrackers(ctx)
	if err != nil {
		return locationservice.Snapshot{}, err
	}
	collections, err := c.listGeofenceCollections(ctx)
	if err != nil {
		return locationservice.Snapshot{}, err
	}
	calculators, err := c.listRouteCalculators(ctx)
	if err != nil {
		return locationservice.Snapshot{}, err
	}
	return locationservice.Snapshot{
		Maps:                maps,
		PlaceIndexes:        indexes,
		Trackers:            trackers,
		GeofenceCollections: collections,
		RouteCalculators:    calculators,
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

var _ locationservice.Client = (*Client)(nil)

var _ apiClient = (*awslocation.Client)(nil)
