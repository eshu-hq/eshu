// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPIDocumentsTheCoordinatorSource proves the coordinator_source object
// (#8828) is its own component with the same keys and closed enums as
// ActiveWorkSource, and that exactly the routes that render the coordinator
// section declare it.
func TestOpenAPIDocumentsTheCoordinatorSource(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	schemas := testutil.MustMapField(t, testutil.MustMapField(t, spec, "components"), "schemas")
	active := testutil.MustMapField(t, testutil.MustMapField(t, schemas, "ActiveWorkSource"), "properties")
	coordinator := testutil.MustMapField(t, testutil.MustMapField(t, schemas, "CoordinatorSource"), "properties")
	if len(coordinator) != 5 {
		t.Fatalf("CoordinatorSource has %d properties, want the same five as ActiveWorkSource", len(coordinator))
	}
	for _, field := range []string{"source", "reason", "as_of", "age_seconds", "stale"} {
		testutil.MustMapField(t, coordinator, field)
	}
	for _, field := range []string{"source", "reason"} {
		if got, want := testutil.MustMapField(t, coordinator, field)["enum"], testutil.MustMapField(t, active, field)["enum"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("CoordinatorSource %s enum = %v, want ActiveWorkSource's %v", field, got, want)
		}
	}

	withMarker := map[string]bool{
		"/api/v0/status/pipeline":             true,
		"/api/v0/status/ingesters":            true,
		"/api/v0/status/ingesters/{ingester}": true,
		"/api/v0/ingesters":                   true,
		"/api/v0/ingesters/{ingester}":        true,
		"/api/v0/status/index":                true,
		"/api/v0/index-status":                true,
		"/api/v0/status/hosted-readiness":     true,
	}
	paths := testutil.MustMapField(t, spec, "paths")
	for route := range paths {
		get, ok := testutil.MustMapField(t, paths, route)["get"].(map[string]any)
		if !ok {
			continue
		}
		field, declared := okResponseProperties(get)["coordinator_source"].(map[string]any)
		switch {
		case withMarker[route] && (!declared || field["$ref"] != "#/components/schemas/CoordinatorSource"):
			t.Fatalf("%s coordinator_source = %#v, want a $ref to the CoordinatorSource component", route, field)
		case !withMarker[route] && declared:
			t.Fatalf("%s declares coordinator_source although it does not render the coordinator section", route)
		}
	}
}
