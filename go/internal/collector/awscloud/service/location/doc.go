// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package location maps Amazon Location Service control-plane metadata into AWS
// cloud collector facts.
//
// The scanner emits reported-confidence resources for Location Service maps,
// place indexes, trackers, geofence collections, and route calculators plus
// relationships for tracker-to-KMS-key, geofence-collection-to-KMS-key, and
// tracker-consumes-geofence-collection (the consumer association from
// ListTrackerConsumers) evidence. Device positions, geofence geometries,
// place-search results, route calculations, map tiles, and any mutation API
// stay outside this package contract: the scanner is metadata-only.
package location
