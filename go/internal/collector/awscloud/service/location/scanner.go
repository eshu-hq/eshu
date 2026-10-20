// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package location

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Location Service metadata-only facts for one claimed
// account and region. It never reads device positions, geofence geometries,
// place-search results, route calculations, or map tiles, and never mutates
// Location Service state. It reports maps, place indexes, trackers, geofence
// collections, and route calculators plus the tracker-to-KMS-key,
// geofence-collection-to-KMS-key, and tracker-consumes-geofence-collection
// relationships.
type Scanner struct {
	// Client is the metadata-only Location Service snapshot source.
	Client Client
}

// Scan observes Location Service maps, place indexes, trackers, geofence
// collections, and route calculators plus their direct KMS and consumer
// dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("location scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceLocation:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceLocation
	default:
		return nil, fmt.Errorf("location scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Location Service metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, resource := range snapshot.Maps {
		next, err := mapEnvelopes(boundary, resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, resource := range snapshot.PlaceIndexes {
		envelope, err := awscloud.NewResourceEnvelope(placeIndexObservation(boundary, resource))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, resource := range snapshot.Trackers {
		next, err := trackerEnvelopes(boundary, resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, resource := range snapshot.GeofenceCollections {
		next, err := geofenceCollectionEnvelopes(boundary, resource)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, resource := range snapshot.RouteCalculators {
		envelope, err := awscloud.NewResourceEnvelope(routeCalculatorObservation(boundary, resource))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func mapEnvelopes(boundary awscloud.Boundary, resource Map) ([]facts.Envelope, error) {
	envelope, err := awscloud.NewResourceEnvelope(mapObservation(boundary, resource))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{envelope}, nil
}

func trackerEnvelopes(boundary awscloud.Boundary, tracker Tracker) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(trackerObservation(boundary, tracker))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	relationships := []*awscloud.RelationshipObservation{trackerKMSRelationship(boundary, tracker)}
	for _, consumerARN := range tracker.ConsumerCollectionARNs {
		relationships = append(relationships, trackerConsumerRelationship(boundary, tracker, consumerARN))
	}
	next, err := relationshipEnvelopes(relationships)
	if err != nil {
		return nil, err
	}
	return append(envelopes, next...), nil
}

func geofenceCollectionEnvelopes(
	boundary awscloud.Boundary,
	collection GeofenceCollection,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(geofenceCollectionObservation(boundary, collection))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	next, err := relationshipEnvelopes([]*awscloud.RelationshipObservation{
		geofenceCollectionKMSRelationship(boundary, collection),
	})
	if err != nil {
		return nil, err
	}
	return append(envelopes, next...), nil
}

// relationshipEnvelopes builds envelopes for every non-nil relationship,
// skipping nil entries so callers can pass optional edges directly.
func relationshipEnvelopes(relationships []*awscloud.RelationshipObservation) ([]facts.Envelope, error) {
	var envelopes []facts.Envelope
	for _, relationship := range relationships {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func mapObservation(boundary awscloud.Boundary, resource Map) awscloud.ResourceObservation {
	arn := strings.TrimSpace(resource.ARN)
	name := strings.TrimSpace(resource.Name)
	resourceID := mapResourceID(resource)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeLocationMap,
		Name:         name,
		Tags:         cloneStringMap(resource.Tags),
		Attributes: map[string]any{
			"data_source":    strings.TrimSpace(resource.DataSource),
			"description":    strings.TrimSpace(resource.Description),
			"style":          strings.TrimSpace(resource.Style),
			"political_view": strings.TrimSpace(resource.PoliticalView),
			"custom_layers":  cloneStrings(resource.CustomLayers),
			"create_time":    timeOrNil(resource.CreateTime),
			"update_time":    timeOrNil(resource.UpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func placeIndexObservation(boundary awscloud.Boundary, resource PlaceIndex) awscloud.ResourceObservation {
	arn := strings.TrimSpace(resource.ARN)
	name := strings.TrimSpace(resource.Name)
	resourceID := placeIndexResourceID(resource)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeLocationPlaceIndex,
		Name:         name,
		Tags:         cloneStringMap(resource.Tags),
		Attributes: map[string]any{
			"data_source":  strings.TrimSpace(resource.DataSource),
			"description":  strings.TrimSpace(resource.Description),
			"intended_use": strings.TrimSpace(resource.IntendedUse),
			"create_time":  timeOrNil(resource.CreateTime),
			"update_time":  timeOrNil(resource.UpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func trackerObservation(boundary awscloud.Boundary, tracker Tracker) awscloud.ResourceObservation {
	arn := strings.TrimSpace(tracker.ARN)
	name := strings.TrimSpace(tracker.Name)
	resourceID := trackerResourceID(tracker)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeLocationTracker,
		Name:         name,
		Tags:         cloneStringMap(tracker.Tags),
		Attributes: map[string]any{
			"description":                        strings.TrimSpace(tracker.Description),
			"kms_key_id":                         strings.TrimSpace(tracker.KMSKeyID),
			"kms_key_geospatial_queries":         tracker.KMSKeyEnableGeospatialQueries,
			"event_bridge_enabled":               tracker.EventBridgeEnabled,
			"position_filtering":                 strings.TrimSpace(tracker.PositionFiltering),
			"consumer_geofence_collection_count": len(tracker.ConsumerCollectionARNs),
			"create_time":                        timeOrNil(tracker.CreateTime),
			"update_time":                        timeOrNil(tracker.UpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func geofenceCollectionObservation(
	boundary awscloud.Boundary,
	collection GeofenceCollection,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(collection.ARN)
	name := strings.TrimSpace(collection.Name)
	resourceID := geofenceCollectionResourceID(collection)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeLocationGeofenceCollection,
		Name:         name,
		Tags:         cloneStringMap(collection.Tags),
		Attributes: map[string]any{
			"description":    strings.TrimSpace(collection.Description),
			"kms_key_id":     strings.TrimSpace(collection.KMSKeyID),
			"geofence_count": collection.GeofenceCount,
			"create_time":    timeOrNil(collection.CreateTime),
			"update_time":    timeOrNil(collection.UpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func routeCalculatorObservation(
	boundary awscloud.Boundary,
	calculator RouteCalculator,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(calculator.ARN)
	name := strings.TrimSpace(calculator.Name)
	resourceID := routeCalculatorResourceID(calculator)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeLocationRouteCalculator,
		Name:         name,
		Tags:         cloneStringMap(calculator.Tags),
		Attributes: map[string]any{
			"data_source": strings.TrimSpace(calculator.DataSource),
			"description": strings.TrimSpace(calculator.Description),
			"create_time": timeOrNil(calculator.CreateTime),
			"update_time": timeOrNil(calculator.UpdateTime),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
