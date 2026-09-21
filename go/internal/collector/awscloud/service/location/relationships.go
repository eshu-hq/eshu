// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package location

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// trackerKMSRelationship records a Location Service tracker's reported KMS
// encryption key dependency. AWS may report a key id, key ARN, or alias; the
// edge keys the target by the reported identifier and sets target_arn only when
// the identifier is ARN-shaped, matching how the KMS scanner publishes its key
// resource_id (bare id or ARN) and carries the key ARN as a correlation anchor.
// It returns nil when no key is reported.
func trackerKMSRelationship(boundary awscloud.Boundary, tracker Tracker) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(tracker.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := trackerResourceID(tracker)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLocationTrackerUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(tracker.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipLocationTrackerUsesKMSKey + ":" + targetID,
	}
}

// geofenceCollectionKMSRelationship records a Location Service geofence
// collection's reported KMS encryption key dependency. It mirrors the tracker
// KMS edge: it keys the target by the reported identifier and sets target_arn
// only when the identifier is ARN-shaped. It returns nil when no key is
// reported.
func geofenceCollectionKMSRelationship(
	boundary awscloud.Boundary,
	collection GeofenceCollection,
) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(collection.KMSKeyID)
	if targetID == "" {
		return nil
	}
	sourceID := geofenceCollectionResourceID(collection)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLocationGeofenceCollectionUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(collection.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipLocationGeofenceCollectionUsesKMSKey + ":" + targetID,
	}
}

// trackerConsumerRelationship records a Location Service tracker's consumer
// association with a geofence collection. ListTrackerConsumers reports the
// geofence collection ARN, which the geofence-collection node also publishes as
// its resource_id, so the edge keys the target by that ARN and joins the
// collection node this scanner emits. It returns nil when either endpoint
// identity is missing. The consumer ARN is ARN-shaped, so target_arn is set.
func trackerConsumerRelationship(
	boundary awscloud.Boundary,
	tracker Tracker,
	consumerARN string,
) *awscloud.RelationshipObservation {
	consumerARN = strings.TrimSpace(consumerARN)
	if consumerARN == "" {
		return nil
	}
	sourceID := trackerResourceID(tracker)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(consumerARN) {
		targetARN = consumerARN
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLocationTrackerConsumesGeofenceCollection,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(tracker.ARN),
		TargetResourceID: consumerARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeLocationGeofenceCollection,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipLocationTrackerConsumesGeofenceCollection + ":" + consumerARN,
	}
}
