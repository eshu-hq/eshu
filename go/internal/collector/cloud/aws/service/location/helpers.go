// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package location

import (
	"strings"
	"time"
)

// mapResourceID returns the resource_id the map node publishes. It prefers the
// map ARN (always present from DescribeMap) and falls back to the map name so a
// node always has a stable identity.
func mapResourceID(m Map) string {
	return firstNonEmpty(m.ARN, m.Name)
}

// placeIndexResourceID returns the resource_id the place index node publishes.
// It prefers the index ARN and falls back to the index name.
func placeIndexResourceID(p PlaceIndex) string {
	return firstNonEmpty(p.ARN, p.Name)
}

// trackerResourceID returns the resource_id the tracker node publishes. It
// prefers the tracker ARN and falls back to the tracker name, so a tracker's
// own edges are sourced on the same id the tracker node publishes.
func trackerResourceID(t Tracker) string {
	return firstNonEmpty(t.ARN, t.Name)
}

// geofenceCollectionResourceID returns the resource_id the geofence collection
// node publishes. It prefers the collection ARN and falls back to the
// collection name, so the tracker-consumes edge can key the collection by the
// same ARN ListTrackerConsumers reports.
func geofenceCollectionResourceID(c GeofenceCollection) string {
	return firstNonEmpty(c.ARN, c.Name)
}

// routeCalculatorResourceID returns the resource_id the route calculator node
// publishes. It prefers the calculator ARN and falls back to the calculator
// name.
func routeCalculatorResourceID(r RouteCalculator) string {
	return firstNonEmpty(r.ARN, r.Name)
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
