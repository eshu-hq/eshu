// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package location

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Location Service observations for one
// AWS claim. Implementations read control-plane describe/list management APIs
// and never read device positions, geofence geometries, place-search results,
// route calculations, or map tiles.
type Client interface {
	// Snapshot returns every Location Service map, place index, tracker,
	// geofence collection, and route calculator visible to the configured AWS
	// credentials, each carrying its control-plane metadata only.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Amazon Location Service control-plane metadata plus
// non-fatal scan warnings. It intentionally carries no device positions,
// geofence geometries, place-search results, or route calculations.
type Snapshot struct {
	// Maps is the metadata-only set of Location Service maps.
	Maps []Map
	// PlaceIndexes is the metadata-only set of Location Service place indexes.
	PlaceIndexes []PlaceIndex
	// Trackers is the metadata-only set of Location Service trackers, each
	// carrying its consumer geofence collection ARNs.
	Trackers []Tracker
	// GeofenceCollections is the metadata-only set of Location Service geofence
	// collections.
	GeofenceCollections []GeofenceCollection
	// RouteCalculators is the metadata-only set of Location Service route
	// calculators.
	RouteCalculators []RouteCalculator
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Map is the scanner-owned Location Service map model. It carries control-plane
// metadata only and intentionally excludes map tiles, glyphs, sprites, and
// style descriptors.
type Map struct {
	// ARN is the Amazon Resource Name that uniquely identifies the map.
	ARN string
	// Name is the Location Service map name.
	Name string
	// DataSource is the name of the map style data source (for example Esri or
	// HERE) the map renders from.
	DataSource string
	// Description is the optional human description of the map.
	Description string
	// Style is the map style configured for the map (for example
	// VectorEsriStreets).
	Style string
	// PoliticalView is the optional ISO country code applied as the political
	// view when rendering borders.
	PoliticalView string
	// CustomLayers are the optional custom layer names enabled on the map style.
	CustomLayers []string
	// CreateTime is when the map was created.
	CreateTime time.Time
	// UpdateTime is when the map was last updated.
	UpdateTime time.Time
	// Tags carries the map resource tags.
	Tags map[string]string
}

// PlaceIndex is the scanner-owned Location Service place index model. It carries
// control-plane metadata only and intentionally excludes place-search queries
// and geocoding results.
type PlaceIndex struct {
	// ARN is the Amazon Resource Name that uniquely identifies the place index.
	ARN string
	// Name is the Location Service place index name.
	Name string
	// DataSource is the name of the geocoding data provider (for example Esri or
	// HERE) the place index queries.
	DataSource string
	// Description is the optional human description of the place index.
	Description string
	// IntendedUse is the configured intended use (SingleUse or Storage) for the
	// place index results.
	IntendedUse string
	// CreateTime is when the place index was created.
	CreateTime time.Time
	// UpdateTime is when the place index was last updated.
	UpdateTime time.Time
	// Tags carries the place index resource tags.
	Tags map[string]string
}

// Tracker is the scanner-owned Location Service tracker model. It carries
// control-plane metadata only and intentionally excludes device positions and
// position history.
type Tracker struct {
	// ARN is the Amazon Resource Name that uniquely identifies the tracker.
	ARN string
	// Name is the Location Service tracker name.
	Name string
	// Description is the optional human description of the tracker.
	Description string
	// KMSKeyID is the identifier of the KMS key used to encrypt tracker data,
	// when a customer-managed key is configured. AWS may report a key id, key
	// ARN, or alias here.
	KMSKeyID string
	// KMSKeyEnableGeospatialQueries reports whether geospatial queries are
	// enabled on the tracker's customer-managed KMS key.
	KMSKeyEnableGeospatialQueries bool
	// EventBridgeEnabled reports whether the tracker publishes events to Amazon
	// EventBridge.
	EventBridgeEnabled bool
	// PositionFiltering is the configured position-filtering mode (for example
	// TimeBased, DistanceBased, or AccuracyBased).
	PositionFiltering string
	// CreateTime is when the tracker was created.
	CreateTime time.Time
	// UpdateTime is when the tracker was last updated.
	UpdateTime time.Time
	// Tags carries the tracker resource tags.
	Tags map[string]string
	// ConsumerCollectionARNs are the geofence collection ARNs associated with
	// the tracker as consumers, reported by ListTrackerConsumers. They key the
	// tracker-consumes-geofence-collection edges and never carry geofence
	// geometries.
	ConsumerCollectionARNs []string
}

// GeofenceCollection is the scanner-owned Location Service geofence collection
// model. It carries control-plane metadata only and intentionally excludes
// geofence geometries.
type GeofenceCollection struct {
	// ARN is the Amazon Resource Name that uniquely identifies the collection.
	ARN string
	// Name is the Location Service geofence collection name.
	Name string
	// Description is the optional human description of the collection.
	Description string
	// KMSKeyID is the identifier of the KMS key used to encrypt collection data,
	// when a customer-managed key is configured. AWS may report a key id, key
	// ARN, or alias here.
	KMSKeyID string
	// GeofenceCount is the number of geofences AWS reports in the collection. The
	// geofence geometries themselves are never read.
	GeofenceCount int32
	// CreateTime is when the collection was created.
	CreateTime time.Time
	// UpdateTime is when the collection was last updated.
	UpdateTime time.Time
	// Tags carries the collection resource tags.
	Tags map[string]string
}

// RouteCalculator is the scanner-owned Location Service route calculator model.
// It carries control-plane metadata only and intentionally excludes route
// calculations and route matrices.
type RouteCalculator struct {
	// ARN is the Amazon Resource Name that uniquely identifies the calculator.
	ARN string
	// Name is the Location Service route calculator name.
	Name string
	// DataSource is the name of the routing data provider (for example Esri or
	// HERE) the calculator queries.
	DataSource string
	// Description is the optional human description of the calculator.
	Description string
	// CreateTime is when the calculator was created.
	CreateTime time.Time
	// UpdateTime is when the calculator was last updated.
	UpdateTime time.Time
	// Tags carries the calculator resource tags.
	Tags map[string]string
}
