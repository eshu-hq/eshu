// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package location

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testMapARN        = "arn:aws:geo:us-east-1:123456789012:map/store-map"
	testIndexARN      = "arn:aws:geo:us-east-1:123456789012:place-index/store-index"
	testTrackerARN    = "arn:aws:geo:us-east-1:123456789012:tracker/fleet-tracker"
	testCollectionARN = "arn:aws:geo:us-east-1:123456789012:geofence-collection/zones"
	testRouteARN      = "arn:aws:geo:us-east-1:123456789012:route-calculator/routes"
	testKMSARN        = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		Maps: []Map{{
			ARN:           testMapARN,
			Name:          "store-map",
			DataSource:    "Esri",
			Description:   "retail map",
			Style:         "VectorEsriStreets",
			PoliticalView: "FRA",
			CustomLayers:  []string{"POI"},
			CreateTime:    time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:          map[string]string{"Environment": "prod"},
		}},
		PlaceIndexes: []PlaceIndex{{
			ARN:         testIndexARN,
			Name:        "store-index",
			DataSource:  "Here",
			IntendedUse: "SingleUse",
			CreateTime:  time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		}},
		Trackers: []Tracker{{
			ARN:                           testTrackerARN,
			Name:                          "fleet-tracker",
			KMSKeyID:                      testKMSARN,
			KMSKeyEnableGeospatialQueries: true,
			EventBridgeEnabled:            true,
			PositionFiltering:             "TimeBased",
			CreateTime:                    time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:                          map[string]string{"Team": "fleet"},
			ConsumerCollectionARNs:        []string{testCollectionARN},
		}},
		GeofenceCollections: []GeofenceCollection{{
			ARN:           testCollectionARN,
			Name:          "zones",
			KMSKeyID:      testKMSARN,
			GeofenceCount: 7,
			CreateTime:    time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		}},
		RouteCalculators: []RouteCalculator{{
			ARN:        testRouteARN,
			Name:       "routes",
			DataSource: "Esri",
			CreateTime: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		}},
	}
}

func TestScannerEmitsLocationMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: fullSnapshot()}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Each control-plane resource emits a node keyed by its ARN.
	for _, tc := range []struct {
		resourceType string
		wantID       string
	}{
		{awscloud.ResourceTypeLocationMap, testMapARN},
		{awscloud.ResourceTypeLocationPlaceIndex, testIndexARN},
		{awscloud.ResourceTypeLocationTracker, testTrackerARN},
		{awscloud.ResourceTypeLocationGeofenceCollection, testCollectionARN},
		{awscloud.ResourceTypeLocationRouteCalculator, testRouteARN},
	} {
		node := resourceByType(t, envelopes, tc.resourceType)
		if got := node.Payload["resource_id"]; got != tc.wantID {
			t.Fatalf("%s resource_id = %#v, want %q", tc.resourceType, got, tc.wantID)
		}
		if got := node.Payload["arn"]; got != tc.wantID {
			t.Fatalf("%s arn = %#v, want %q", tc.resourceType, got, tc.wantID)
		}
	}

	// Tracker attributes carry control-plane metadata only.
	tracker := resourceByType(t, envelopes, awscloud.ResourceTypeLocationTracker)
	trackerAttrs := attributesOf(t, tracker)
	assertAttribute(t, trackerAttrs, "position_filtering", "TimeBased")
	assertAttribute(t, trackerAttrs, "event_bridge_enabled", true)
	assertAttribute(t, trackerAttrs, "consumer_geofence_collection_count", 1)

	collection := resourceByType(t, envelopes, awscloud.ResourceTypeLocationGeofenceCollection)
	assertAttribute(t, attributesOf(t, collection), "geofence_count", int32(7))

	mapNode := resourceByType(t, envelopes, awscloud.ResourceTypeLocationMap)
	mapAttrs := attributesOf(t, mapNode)
	assertAttribute(t, mapAttrs, "style", "VectorEsriStreets")
	assertAttribute(t, mapAttrs, "custom_layers", []string{"POI"})

	// tracker -> KMS key edge.
	trackerKMS := relationshipByType(t, envelopes, awscloud.RelationshipLocationTrackerUsesKMSKey)
	assertEdgeTarget(t, trackerKMS, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := trackerKMS.Payload["source_resource_id"], testTrackerARN; got != want {
		t.Fatalf("tracker->kms source_resource_id = %#v, want %q", got, want)
	}
	if got, want := trackerKMS.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("tracker->kms target_arn = %#v, want %q", got, want)
	}

	// geofence collection -> KMS key edge.
	collectionKMS := relationshipByType(t, envelopes, awscloud.RelationshipLocationGeofenceCollectionUsesKMSKey)
	assertEdgeTarget(t, collectionKMS, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := collectionKMS.Payload["source_resource_id"], testCollectionARN; got != want {
		t.Fatalf("collection->kms source_resource_id = %#v, want %q", got, want)
	}

	// tracker -> geofence collection consumer edge, keyed by the collection ARN
	// the collection node publishes.
	consumer := relationshipByType(t, envelopes, awscloud.RelationshipLocationTrackerConsumesGeofenceCollection)
	assertEdgeTarget(t, consumer, awscloud.ResourceTypeLocationGeofenceCollection, testCollectionARN)
	if got, want := consumer.Payload["source_resource_id"], testTrackerARN; got != want {
		t.Fatalf("tracker->collection source_resource_id = %#v, want %q", got, want)
	}
	if got, want := consumer.Payload["target_arn"], testCollectionARN; got != want {
		t.Fatalf("tracker->collection target_arn = %#v, want %q", got, want)
	}

	// No device positions, geofence geometries, place results, or routes leak.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"device_positions", "positions", "position_history", "geofences",
			"geometry", "geometries", "places", "place_results", "search_results",
			"routes", "route_matrix", "legs", "map_tiles",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Location scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerOmitsKMSEdgeForNonARNKeyButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Trackers: []Tracker{{
		ARN:      testTrackerARN,
		Name:     "fleet-tracker",
		KMSKeyID: "alias/location-fleet",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	trackerKMS := relationshipByType(t, envelopes, awscloud.RelationshipLocationTrackerUsesKMSKey)
	if got, want := trackerKMS.Payload["target_resource_id"], "alias/location-fleet"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := trackerKMS.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for non-ARN key identifier", got)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Maps:             []Map{{ARN: testMapARN, Name: "store-map"}},
		PlaceIndexes:     []PlaceIndex{{ARN: testIndexARN, Name: "store-index"}},
		RouteCalculators: []RouteCalculator{{ARN: testRouteARN, Name: "routes"}},
		// Tracker and collection with no KMS key and no consumers.
		Trackers:            []Tracker{{ARN: testTrackerARN, Name: "fleet-tracker"}},
		GeofenceCollections: []GeofenceCollection{{ARN: testCollectionARN, Name: "zones"}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	tracker := Tracker{
		ARN:                    testTrackerARN,
		Name:                   "fleet-tracker",
		KMSKeyID:               testKMSARN,
		ConsumerCollectionARNs: []string{testCollectionARN},
	}
	collection := GeofenceCollection{ARN: testCollectionARN, Name: "zones", KMSKeyID: testKMSARN}
	var observations []awscloud.RelationshipObservation
	rels := []*awscloud.RelationshipObservation{
		trackerKMSRelationship(boundary, tracker),
		geofenceCollectionKMSRelationship(boundary, collection),
		trackerConsumerRelationship(boundary, tracker, testCollectionARN),
	}
	for _, rel := range rels {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerHandlesEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() returned %d envelopes for empty account, want 0", len(envelopes))
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Maps: []Map{{ARN: testMapARN, Name: "store-map"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Location ListTrackers throttled after SDK retries; tracker metadata omitted for this scan",
			SourceRecordID: "location_trackers_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceLocation,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:location:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
