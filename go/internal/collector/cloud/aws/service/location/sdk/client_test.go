// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awslocation "github.com/aws/aws-sdk-go-v2/service/location"
	awslocationtypes "github.com/aws/aws-sdk-go-v2/service/location/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientSnapshotsLocationMetadataOnly(t *testing.T) {
	mapARN := "arn:aws:geo:us-east-1:123456789012:map/store-map"
	indexARN := "arn:aws:geo:us-east-1:123456789012:place-index/store-index"
	trackerARN := "arn:aws:geo:us-east-1:123456789012:tracker/fleet-tracker"
	collectionARN := "arn:aws:geo:us-east-1:123456789012:geofence-collection/zones"
	routeARN := "arn:aws:geo:us-east-1:123456789012:route-calculator/routes"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeLocationAPI{
		maps: map[string]*awslocation.DescribeMapOutput{
			"store-map": {
				MapArn:     awsv2.String(mapARN),
				MapName:    awsv2.String("store-map"),
				DataSource: awsv2.String("Esri"),
				CreateTime: awsv2.Time(createdAt),
				Configuration: &awslocationtypes.MapConfiguration{
					Style:         awsv2.String("VectorEsriStreets"),
					PoliticalView: awsv2.String("FRA"),
					CustomLayers:  []string{"POI"},
				},
				Tags: map[string]string{"Environment": "prod"},
			},
		},
		indexes: map[string]*awslocation.DescribePlaceIndexOutput{
			"store-index": {
				IndexArn:   awsv2.String(indexARN),
				IndexName:  awsv2.String("store-index"),
				DataSource: awsv2.String("Here"),
				DataSourceConfiguration: &awslocationtypes.DataSourceConfiguration{
					IntendedUse: awslocationtypes.IntendedUse("SingleUse"),
				},
			},
		},
		trackers: map[string]*awslocation.DescribeTrackerOutput{
			"fleet-tracker": {
				TrackerArn:                    awsv2.String(trackerARN),
				TrackerName:                   awsv2.String("fleet-tracker"),
				KmsKeyId:                      awsv2.String(kmsARN),
				KmsKeyEnableGeospatialQueries: awsv2.Bool(true),
				EventBridgeEnabled:            awsv2.Bool(true),
				PositionFiltering:             awslocationtypes.PositionFiltering("TimeBased"),
			},
		},
		trackerConsumers: map[string][]string{"fleet-tracker": {collectionARN}},
		collections: map[string]*awslocation.DescribeGeofenceCollectionOutput{
			"zones": {
				CollectionArn:  awsv2.String(collectionARN),
				CollectionName: awsv2.String("zones"),
				KmsKeyId:       awsv2.String(kmsARN),
				GeofenceCount:  awsv2.Int32(7),
			},
		},
		calculators: map[string]*awslocation.DescribeRouteCalculatorOutput{
			"routes": {
				CalculatorArn:  awsv2.String(routeARN),
				CalculatorName: awsv2.String("routes"),
				DataSource:     awsv2.String("Esri"),
			},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Maps) != 1 || snapshot.Maps[0].ARN != mapARN {
		t.Fatalf("Maps = %#v, want one map keyed by %q", snapshot.Maps, mapARN)
	}
	if snapshot.Maps[0].Style != "VectorEsriStreets" || snapshot.Maps[0].PoliticalView != "FRA" {
		t.Fatalf("map config = %#v, want style/political view mapped", snapshot.Maps[0])
	}
	if len(snapshot.PlaceIndexes) != 1 || snapshot.PlaceIndexes[0].IntendedUse != "SingleUse" {
		t.Fatalf("PlaceIndexes = %#v, want intended use SingleUse", snapshot.PlaceIndexes)
	}
	if len(snapshot.Trackers) != 1 {
		t.Fatalf("len(Trackers) = %d, want 1", len(snapshot.Trackers))
	}
	tracker := snapshot.Trackers[0]
	if tracker.ARN != trackerARN || tracker.KMSKeyID != kmsARN {
		t.Fatalf("tracker = %#v, want ARN %q and KMS %q", tracker, trackerARN, kmsARN)
	}
	if tracker.PositionFiltering != "TimeBased" || !tracker.EventBridgeEnabled {
		t.Fatalf("tracker filtering/eventbridge = %#v", tracker)
	}
	if len(tracker.ConsumerCollectionARNs) != 1 || tracker.ConsumerCollectionARNs[0] != collectionARN {
		t.Fatalf("tracker consumers = %#v, want [%q]", tracker.ConsumerCollectionARNs, collectionARN)
	}
	if len(snapshot.GeofenceCollections) != 1 || snapshot.GeofenceCollections[0].GeofenceCount != 7 {
		t.Fatalf("GeofenceCollections = %#v, want count 7", snapshot.GeofenceCollections)
	}
	if len(snapshot.RouteCalculators) != 1 || snapshot.RouteCalculators[0].ARN != routeARN {
		t.Fatalf("RouteCalculators = %#v, want one keyed by %q", snapshot.RouteCalculators, routeARN)
	}
}

func TestClientSnapshotEmptyAccountReturnsNil(t *testing.T) {
	client := &Client{client: &fakeLocationAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Maps) != 0 || len(snapshot.PlaceIndexes) != 0 || len(snapshot.Trackers) != 0 ||
		len(snapshot.GeofenceCollections) != 0 || len(snapshot.RouteCalculators) != 0 {
		t.Fatalf("Snapshot() = %#v, want all empty for empty account", snapshot)
	}
}

type fakeLocationAPI struct {
	maps             map[string]*awslocation.DescribeMapOutput
	indexes          map[string]*awslocation.DescribePlaceIndexOutput
	trackers         map[string]*awslocation.DescribeTrackerOutput
	trackerConsumers map[string][]string
	collections      map[string]*awslocation.DescribeGeofenceCollectionOutput
	calculators      map[string]*awslocation.DescribeRouteCalculatorOutput
}

func (f *fakeLocationAPI) ListMaps(
	_ context.Context, _ *awslocation.ListMapsInput, _ ...func(*awslocation.Options),
) (*awslocation.ListMapsOutput, error) {
	var entries []awslocationtypes.ListMapsResponseEntry
	for name := range f.maps {
		entries = append(entries, awslocationtypes.ListMapsResponseEntry{MapName: awsv2.String(name)})
	}
	return &awslocation.ListMapsOutput{Entries: entries}, nil
}

func (f *fakeLocationAPI) DescribeMap(
	_ context.Context, in *awslocation.DescribeMapInput, _ ...func(*awslocation.Options),
) (*awslocation.DescribeMapOutput, error) {
	return f.maps[awsv2.ToString(in.MapName)], nil
}

func (f *fakeLocationAPI) ListPlaceIndexes(
	_ context.Context, _ *awslocation.ListPlaceIndexesInput, _ ...func(*awslocation.Options),
) (*awslocation.ListPlaceIndexesOutput, error) {
	var entries []awslocationtypes.ListPlaceIndexesResponseEntry
	for name := range f.indexes {
		entries = append(entries, awslocationtypes.ListPlaceIndexesResponseEntry{IndexName: awsv2.String(name)})
	}
	return &awslocation.ListPlaceIndexesOutput{Entries: entries}, nil
}

func (f *fakeLocationAPI) DescribePlaceIndex(
	_ context.Context, in *awslocation.DescribePlaceIndexInput, _ ...func(*awslocation.Options),
) (*awslocation.DescribePlaceIndexOutput, error) {
	return f.indexes[awsv2.ToString(in.IndexName)], nil
}

func (f *fakeLocationAPI) ListTrackers(
	_ context.Context, _ *awslocation.ListTrackersInput, _ ...func(*awslocation.Options),
) (*awslocation.ListTrackersOutput, error) {
	var entries []awslocationtypes.ListTrackersResponseEntry
	for name := range f.trackers {
		entries = append(entries, awslocationtypes.ListTrackersResponseEntry{TrackerName: awsv2.String(name)})
	}
	return &awslocation.ListTrackersOutput{Entries: entries}, nil
}

func (f *fakeLocationAPI) DescribeTracker(
	_ context.Context, in *awslocation.DescribeTrackerInput, _ ...func(*awslocation.Options),
) (*awslocation.DescribeTrackerOutput, error) {
	return f.trackers[awsv2.ToString(in.TrackerName)], nil
}

func (f *fakeLocationAPI) ListTrackerConsumers(
	_ context.Context, in *awslocation.ListTrackerConsumersInput, _ ...func(*awslocation.Options),
) (*awslocation.ListTrackerConsumersOutput, error) {
	return &awslocation.ListTrackerConsumersOutput{
		ConsumerArns: f.trackerConsumers[awsv2.ToString(in.TrackerName)],
	}, nil
}

func (f *fakeLocationAPI) ListGeofenceCollections(
	_ context.Context, _ *awslocation.ListGeofenceCollectionsInput, _ ...func(*awslocation.Options),
) (*awslocation.ListGeofenceCollectionsOutput, error) {
	var entries []awslocationtypes.ListGeofenceCollectionsResponseEntry
	for name := range f.collections {
		entries = append(entries, awslocationtypes.ListGeofenceCollectionsResponseEntry{CollectionName: awsv2.String(name)})
	}
	return &awslocation.ListGeofenceCollectionsOutput{Entries: entries}, nil
}

func (f *fakeLocationAPI) DescribeGeofenceCollection(
	_ context.Context, in *awslocation.DescribeGeofenceCollectionInput, _ ...func(*awslocation.Options),
) (*awslocation.DescribeGeofenceCollectionOutput, error) {
	return f.collections[awsv2.ToString(in.CollectionName)], nil
}

func (f *fakeLocationAPI) ListRouteCalculators(
	_ context.Context, _ *awslocation.ListRouteCalculatorsInput, _ ...func(*awslocation.Options),
) (*awslocation.ListRouteCalculatorsOutput, error) {
	var entries []awslocationtypes.ListRouteCalculatorsResponseEntry
	for name := range f.calculators {
		entries = append(entries, awslocationtypes.ListRouteCalculatorsResponseEntry{CalculatorName: awsv2.String(name)})
	}
	return &awslocation.ListRouteCalculatorsOutput{Entries: entries}, nil
}

func (f *fakeLocationAPI) DescribeRouteCalculator(
	_ context.Context, in *awslocation.DescribeRouteCalculatorInput, _ ...func(*awslocation.Options),
) (*awslocation.DescribeRouteCalculatorOutput, error) {
	return f.calculators[awsv2.ToString(in.CalculatorName)], nil
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceLocation,
	}
}
