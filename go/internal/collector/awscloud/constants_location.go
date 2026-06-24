// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceLocation identifies the regional Amazon Location Service
	// metadata-only scan slice. The scanner reads control-plane describe/list
	// management APIs for maps, place indexes, trackers, geofence collections,
	// and route calculators and never reads device positions, geofence
	// geometries, place-search results, route calculations, or map tiles, and
	// never mutates Location Service state.
	ServiceLocation = "location"
)

const (
	// ResourceTypeLocationMap identifies an Amazon Location Service map
	// resource. The scanner emits identity, the map style data source, the
	// configured map style and political view, and lifecycle timestamps only.
	// Map tiles, glyphs, sprites, and style descriptors stay outside the
	// contract.
	ResourceTypeLocationMap = "aws_location_map"
	// ResourceTypeLocationPlaceIndex identifies an Amazon Location Service place
	// index resource. The scanner emits identity, the geocoding data source, the
	// intended-use configuration, and lifecycle timestamps only. Place-search
	// queries and geocoding results stay outside the contract.
	ResourceTypeLocationPlaceIndex = "aws_location_place_index"
	// ResourceTypeLocationTracker identifies an Amazon Location Service tracker
	// resource. The scanner emits identity, the KMS encryption key identifier,
	// the position-filtering mode, the EventBridge and geospatial-query flags,
	// and lifecycle timestamps only. Device positions and position history stay
	// outside the contract.
	ResourceTypeLocationTracker = "aws_location_tracker"
	// ResourceTypeLocationGeofenceCollection identifies an Amazon Location
	// Service geofence collection resource. The scanner emits identity, the KMS
	// encryption key identifier, the geofence count, and lifecycle timestamps
	// only. Geofence geometries stay outside the contract.
	ResourceTypeLocationGeofenceCollection = "aws_location_geofence_collection"
	// ResourceTypeLocationRouteCalculator identifies an Amazon Location Service
	// route calculator resource. The scanner emits identity, the routing data
	// source, and lifecycle timestamps only. Route calculations and route
	// matrices stay outside the contract.
	ResourceTypeLocationRouteCalculator = "aws_location_route_calculator"
)

const (
	// RelationshipLocationTrackerUsesKMSKey records an Amazon Location Service
	// tracker's reported KMS encryption key dependency. The target is keyed by
	// the reported key identifier and targets aws_kms_key.
	RelationshipLocationTrackerUsesKMSKey = "location_tracker_uses_kms_key"
	// RelationshipLocationGeofenceCollectionUsesKMSKey records an Amazon Location
	// Service geofence collection's reported KMS encryption key dependency. The
	// target is keyed by the reported key identifier and targets aws_kms_key.
	RelationshipLocationGeofenceCollectionUsesKMSKey = "location_geofence_collection_uses_kms_key"
	// RelationshipLocationTrackerConsumesGeofenceCollection records an Amazon
	// Location Service tracker's consumer association with a geofence collection.
	// ListTrackerConsumers reports the geofence collection ARN, which the
	// geofence-collection node also publishes as its resource_id, so the edge
	// joins that node. The association is the control-plane link that evaluates
	// device positions against a collection's geofences; the scanner never reads
	// the geofence geometries or the device positions themselves.
	RelationshipLocationTrackerConsumesGeofenceCollection = "location_tracker_consumes_geofence_collection"
)
