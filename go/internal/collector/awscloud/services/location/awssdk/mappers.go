// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslocation "github.com/aws/aws-sdk-go-v2/service/location"

	locationservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/location"
)

// listMaps pages ListMaps to exhaustion and describes each map for its full
// control-plane metadata (ARN, style, data source, tags). Map tiles, glyphs,
// sprites, and style descriptors are never read.
func (c *Client) listMaps(ctx context.Context) ([]locationservice.Map, error) {
	var names []string
	var nextToken *string
	for {
		var page *awslocation.ListMapsOutput
		err := c.recordAPICall(ctx, "ListMaps", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListMaps(callCtx, &awslocation.ListMapsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, entry := range page.Entries {
			if name := strings.TrimSpace(aws.ToString(entry.MapName)); name != "" {
				names = append(names, name)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	maps := make([]locationservice.Map, 0, len(names))
	for _, name := range names {
		mapped, err := c.describeMap(ctx, name)
		if err != nil {
			return nil, err
		}
		maps = append(maps, mapped)
	}
	return maps, nil
}

func (c *Client) describeMap(ctx context.Context, name string) (locationservice.Map, error) {
	var out *awslocation.DescribeMapOutput
	err := c.recordAPICall(ctx, "DescribeMap", func(callCtx context.Context) error {
		var err error
		out, err = c.client.DescribeMap(callCtx, &awslocation.DescribeMapInput{MapName: aws.String(name)})
		return err
	})
	if err != nil || out == nil {
		return locationservice.Map{Name: name}, err
	}
	mapped := locationservice.Map{
		ARN:         strings.TrimSpace(aws.ToString(out.MapArn)),
		Name:        strings.TrimSpace(aws.ToString(out.MapName)),
		DataSource:  strings.TrimSpace(aws.ToString(out.DataSource)),
		Description: strings.TrimSpace(aws.ToString(out.Description)),
		CreateTime:  aws.ToTime(out.CreateTime),
		UpdateTime:  aws.ToTime(out.UpdateTime),
		Tags:        cloneTags(out.Tags),
	}
	if cfg := out.Configuration; cfg != nil {
		mapped.Style = strings.TrimSpace(aws.ToString(cfg.Style))
		mapped.PoliticalView = strings.TrimSpace(aws.ToString(cfg.PoliticalView))
		mapped.CustomLayers = cfg.CustomLayers
	}
	if mapped.Name == "" {
		mapped.Name = name
	}
	return mapped, nil
}

// listPlaceIndexes pages ListPlaceIndexes to exhaustion and describes each index
// for its full control-plane metadata. Place-search queries and geocoding
// results are never read.
func (c *Client) listPlaceIndexes(ctx context.Context) ([]locationservice.PlaceIndex, error) {
	var names []string
	var nextToken *string
	for {
		var page *awslocation.ListPlaceIndexesOutput
		err := c.recordAPICall(ctx, "ListPlaceIndexes", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPlaceIndexes(callCtx, &awslocation.ListPlaceIndexesInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, entry := range page.Entries {
			if name := strings.TrimSpace(aws.ToString(entry.IndexName)); name != "" {
				names = append(names, name)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	indexes := make([]locationservice.PlaceIndex, 0, len(names))
	for _, name := range names {
		mapped, err := c.describePlaceIndex(ctx, name)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, mapped)
	}
	return indexes, nil
}

func (c *Client) describePlaceIndex(ctx context.Context, name string) (locationservice.PlaceIndex, error) {
	var out *awslocation.DescribePlaceIndexOutput
	err := c.recordAPICall(ctx, "DescribePlaceIndex", func(callCtx context.Context) error {
		var err error
		out, err = c.client.DescribePlaceIndex(callCtx, &awslocation.DescribePlaceIndexInput{IndexName: aws.String(name)})
		return err
	})
	if err != nil || out == nil {
		return locationservice.PlaceIndex{Name: name}, err
	}
	mapped := locationservice.PlaceIndex{
		ARN:         strings.TrimSpace(aws.ToString(out.IndexArn)),
		Name:        strings.TrimSpace(aws.ToString(out.IndexName)),
		DataSource:  strings.TrimSpace(aws.ToString(out.DataSource)),
		Description: strings.TrimSpace(aws.ToString(out.Description)),
		CreateTime:  aws.ToTime(out.CreateTime),
		UpdateTime:  aws.ToTime(out.UpdateTime),
		Tags:        cloneTags(out.Tags),
	}
	if cfg := out.DataSourceConfiguration; cfg != nil {
		mapped.IntendedUse = strings.TrimSpace(string(cfg.IntendedUse))
	}
	if mapped.Name == "" {
		mapped.Name = name
	}
	return mapped, nil
}

// listTrackers pages ListTrackers to exhaustion and describes each tracker plus
// its consumer geofence collection associations. Device positions and position
// history are never read.
func (c *Client) listTrackers(ctx context.Context) ([]locationservice.Tracker, error) {
	var names []string
	var nextToken *string
	for {
		var page *awslocation.ListTrackersOutput
		err := c.recordAPICall(ctx, "ListTrackers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTrackers(callCtx, &awslocation.ListTrackersInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, entry := range page.Entries {
			if name := strings.TrimSpace(aws.ToString(entry.TrackerName)); name != "" {
				names = append(names, name)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	trackers := make([]locationservice.Tracker, 0, len(names))
	for _, name := range names {
		mapped, err := c.describeTracker(ctx, name)
		if err != nil {
			return nil, err
		}
		consumers, err := c.listTrackerConsumers(ctx, name)
		if err != nil {
			return nil, err
		}
		mapped.ConsumerCollectionARNs = consumers
		trackers = append(trackers, mapped)
	}
	return trackers, nil
}

func (c *Client) describeTracker(ctx context.Context, name string) (locationservice.Tracker, error) {
	var out *awslocation.DescribeTrackerOutput
	err := c.recordAPICall(ctx, "DescribeTracker", func(callCtx context.Context) error {
		var err error
		out, err = c.client.DescribeTracker(callCtx, &awslocation.DescribeTrackerInput{TrackerName: aws.String(name)})
		return err
	})
	if err != nil || out == nil {
		return locationservice.Tracker{Name: name}, err
	}
	mapped := locationservice.Tracker{
		ARN:                           strings.TrimSpace(aws.ToString(out.TrackerArn)),
		Name:                          strings.TrimSpace(aws.ToString(out.TrackerName)),
		Description:                   strings.TrimSpace(aws.ToString(out.Description)),
		KMSKeyID:                      strings.TrimSpace(aws.ToString(out.KmsKeyId)),
		KMSKeyEnableGeospatialQueries: aws.ToBool(out.KmsKeyEnableGeospatialQueries),
		EventBridgeEnabled:            aws.ToBool(out.EventBridgeEnabled),
		PositionFiltering:             strings.TrimSpace(string(out.PositionFiltering)),
		CreateTime:                    aws.ToTime(out.CreateTime),
		UpdateTime:                    aws.ToTime(out.UpdateTime),
		Tags:                          cloneTags(out.Tags),
	}
	if mapped.Name == "" {
		mapped.Name = name
	}
	return mapped, nil
}

// listTrackerConsumers pages ListTrackerConsumers to exhaustion. The reported
// consumer ARNs are geofence collection ARNs; the API returns identities only,
// never geofence geometries.
func (c *Client) listTrackerConsumers(ctx context.Context, trackerName string) ([]string, error) {
	var consumers []string
	var nextToken *string
	for {
		var page *awslocation.ListTrackerConsumersOutput
		err := c.recordAPICall(ctx, "ListTrackerConsumers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTrackerConsumers(callCtx, &awslocation.ListTrackerConsumersInput{
				TrackerName: aws.String(trackerName),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, arn := range page.ConsumerArns {
			if trimmed := strings.TrimSpace(arn); trimmed != "" {
				consumers = append(consumers, trimmed)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	return consumers, nil
}

// listGeofenceCollections pages ListGeofenceCollections to exhaustion and
// describes each collection for its full control-plane metadata. Geofence
// geometries are never read.
func (c *Client) listGeofenceCollections(ctx context.Context) ([]locationservice.GeofenceCollection, error) {
	var names []string
	var nextToken *string
	for {
		var page *awslocation.ListGeofenceCollectionsOutput
		err := c.recordAPICall(ctx, "ListGeofenceCollections", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListGeofenceCollections(callCtx, &awslocation.ListGeofenceCollectionsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, entry := range page.Entries {
			if name := strings.TrimSpace(aws.ToString(entry.CollectionName)); name != "" {
				names = append(names, name)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	collections := make([]locationservice.GeofenceCollection, 0, len(names))
	for _, name := range names {
		mapped, err := c.describeGeofenceCollection(ctx, name)
		if err != nil {
			return nil, err
		}
		collections = append(collections, mapped)
	}
	return collections, nil
}

func (c *Client) describeGeofenceCollection(
	ctx context.Context,
	name string,
) (locationservice.GeofenceCollection, error) {
	var out *awslocation.DescribeGeofenceCollectionOutput
	err := c.recordAPICall(ctx, "DescribeGeofenceCollection", func(callCtx context.Context) error {
		var err error
		out, err = c.client.DescribeGeofenceCollection(callCtx, &awslocation.DescribeGeofenceCollectionInput{
			CollectionName: aws.String(name),
		})
		return err
	})
	if err != nil || out == nil {
		return locationservice.GeofenceCollection{Name: name}, err
	}
	mapped := locationservice.GeofenceCollection{
		ARN:           strings.TrimSpace(aws.ToString(out.CollectionArn)),
		Name:          strings.TrimSpace(aws.ToString(out.CollectionName)),
		Description:   strings.TrimSpace(aws.ToString(out.Description)),
		KMSKeyID:      strings.TrimSpace(aws.ToString(out.KmsKeyId)),
		GeofenceCount: aws.ToInt32(out.GeofenceCount),
		CreateTime:    aws.ToTime(out.CreateTime),
		UpdateTime:    aws.ToTime(out.UpdateTime),
		Tags:          cloneTags(out.Tags),
	}
	if mapped.Name == "" {
		mapped.Name = name
	}
	return mapped, nil
}

// listRouteCalculators pages ListRouteCalculators to exhaustion and describes
// each calculator for its full control-plane metadata. Route calculations and
// route matrices are never read.
func (c *Client) listRouteCalculators(ctx context.Context) ([]locationservice.RouteCalculator, error) {
	var names []string
	var nextToken *string
	for {
		var page *awslocation.ListRouteCalculatorsOutput
		err := c.recordAPICall(ctx, "ListRouteCalculators", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRouteCalculators(callCtx, &awslocation.ListRouteCalculatorsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, entry := range page.Entries {
			if name := strings.TrimSpace(aws.ToString(entry.CalculatorName)); name != "" {
				names = append(names, name)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	calculators := make([]locationservice.RouteCalculator, 0, len(names))
	for _, name := range names {
		mapped, err := c.describeRouteCalculator(ctx, name)
		if err != nil {
			return nil, err
		}
		calculators = append(calculators, mapped)
	}
	return calculators, nil
}

func (c *Client) describeRouteCalculator(
	ctx context.Context,
	name string,
) (locationservice.RouteCalculator, error) {
	var out *awslocation.DescribeRouteCalculatorOutput
	err := c.recordAPICall(ctx, "DescribeRouteCalculator", func(callCtx context.Context) error {
		var err error
		out, err = c.client.DescribeRouteCalculator(callCtx, &awslocation.DescribeRouteCalculatorInput{
			CalculatorName: aws.String(name),
		})
		return err
	})
	if err != nil || out == nil {
		return locationservice.RouteCalculator{Name: name}, err
	}
	mapped := locationservice.RouteCalculator{
		ARN:         strings.TrimSpace(aws.ToString(out.CalculatorArn)),
		Name:        strings.TrimSpace(aws.ToString(out.CalculatorName)),
		DataSource:  strings.TrimSpace(aws.ToString(out.DataSource)),
		Description: strings.TrimSpace(aws.ToString(out.Description)),
		CreateTime:  aws.ToTime(out.CreateTime),
		UpdateTime:  aws.ToTime(out.UpdateTime),
		Tags:        cloneTags(out.Tags),
	}
	if mapped.Name == "" {
		mapped.Name = name
	}
	return mapped, nil
}

// cloneTags returns a trimmed-key copy of the AWS-reported tag map, or nil when
// nothing survives, keeping omitempty-style payload behavior consistent.
func cloneTags(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			output[trimmed] = value
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
