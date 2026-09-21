// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// forbiddenDataPlaneOps are the exact Amazon Location Service data-plane and
// mutation operation names the metadata-only adapter must never reach. They read
// device positions, geofence geometries, place-search results, route
// calculations, map tiles, or API key material, or they mutate Location state.
// They are matched by exact method name so the legitimate control-plane reads
// (ListGeofenceCollections, DescribeGeofenceCollection, ListRouteCalculators,
// DescribeRouteCalculator, ...) are never tripped by a coarse substring.
var forbiddenDataPlaneOps = map[string]struct{}{
	"ListDevicePositions":            {},
	"GetDevicePosition":              {},
	"GetDevicePositionHistory":       {},
	"BatchGetDevicePosition":         {},
	"VerifyDevicePosition":           {},
	"ListGeofences":                  {},
	"GetGeofence":                    {},
	"SearchPlaceIndexForText":        {},
	"SearchPlaceIndexForPosition":    {},
	"SearchPlaceIndexForSuggestions": {},
	"GetPlace":                       {},
	"CalculateRoute":                 {},
	"CalculateRouteMatrix":           {},
	"ForecastGeofenceEvents":         {},
	"GetMapTile":                     {},
	"GetMapGlyphs":                   {},
	"GetMapSprites":                  {},
	"GetMapStyleDescriptor":          {},
	"ListKeys":                       {},
	"DescribeKey":                    {},
	"ListJobs":                       {},
	"GetJob":                         {},
}

// TestAdapterInterfaceForbidsDataPlaneAndMutation is the metadata-only
// acceptance gate the issue calls out for Location Service: the SDK adapter must
// never read device positions, geofence geometries, place-search results, route
// calculations, map tiles, or API key material, and must never mutate Location
// Service state. We reflect over the adapter's read interface and confirm no
// data-plane read, search, calculation, tile read, key read, or mutation method
// is reachable. This test fails the build if a future edit ever adds one of
// these to the adapter surface.
func TestAdapterInterfaceForbidsDataPlaneAndMutation(t *testing.T) {
	mutationPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Cancel", "Verify",
		"Forecast", "Search", "Calculate", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Location Service read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if _, banned := forbiddenDataPlaneOps[name]; banned {
			t.Fatalf("apiClient exposes forbidden data-plane/key method %q; the Location adapter is metadata-only", name)
		}
		for _, prefix := range mutationPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/data-plane method %q (prefix %q); the Location adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreControlPlaneReads asserts every method on the adapter
// interface is a List or Describe control-plane read so the read surface stays
// explicit and auditable. The scanner reads resource metadata and tracker
// consumer associations only; nothing fetches data-plane payloads.
func TestAdapterMethodsAreControlPlaneReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a List or Describe read", name)
		}
	}
}
