// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Location Service client into the
// metadata-only Location Service scanner interface.
//
// The adapter uses ListMaps/DescribeMap, ListPlaceIndexes/DescribePlaceIndex,
// ListTrackers/DescribeTracker/ListTrackerConsumers,
// ListGeofenceCollections/DescribeGeofenceCollection, and
// ListRouteCalculators/DescribeRouteCalculator to read control-plane metadata
// (the Describe reads return resource tags inline). It intentionally excludes
// every device-position read, geofence-geometry read, place-search, route
// calculation, map-tile read, API-key read, and Create/Update/Delete mutation,
// so the adapter cannot read data-plane payloads or mutate Location Service
// state.
package awssdk
