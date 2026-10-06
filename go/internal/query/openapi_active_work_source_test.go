// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPIDocumentsTheActiveWorkSource proves the active_work_source object
// (#7009) is a shared component and every status route that renders queue,
// stage, backlog, blockage, or failure data declares it, in lockstep with the
// handlers in status.go and status_mappers.go.
func TestOpenAPIDocumentsTheActiveWorkSource(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	components := testutil.MustMapField(t, spec, "components")
	schemas := testutil.MustMapField(t, components, "schemas")
	source := testutil.MustMapField(t, schemas, "ActiveWorkSource")
	properties := testutil.MustMapField(t, source, "properties")
	for _, field := range []string{"source", "reason", "as_of", "age_seconds", "stale"} {
		testutil.MustMapField(t, properties, field)
	}
	assertEnum(t, testutil.MustMapField(t, properties, "source"), "model", "live", "live_fallback")
	assertEnum(t, testutil.MustMapField(t, properties, "reason"),
		"fresh", "flag_off", "missing", "not_installed", "version", "row_count", "stale", "decode")

	paths := testutil.MustMapField(t, spec, "paths")
	for _, route := range []string{
		"/api/v0/status/pipeline",
		"/api/v0/status/index",
		"/api/v0/index-status",
		"/api/v0/status/ingesters",
		"/api/v0/status/ingesters/{ingester}",
		"/api/v0/ingesters",
		"/api/v0/ingesters/{ingester}",
		"/api/v0/status/hosted-readiness",
		"/api/v0/status/operations",
	} {
		get := testutil.MustMapField(t, testutil.MustMapField(t, paths, route), "get")
		responses := testutil.MustMapField(t, get, "responses")
		ok := testutil.MustMapField(t, responses, "200")
		content := testutil.MustMapField(t, testutil.MustMapField(t, ok, "content"), "application/json")
		schema := testutil.MustMapField(t, content, "schema")
		props := testutil.MustMapField(t, schema, "properties")
		field := testutil.MustMapField(t, props, "active_work_source")
		if got := field["$ref"]; got != "#/components/schemas/ActiveWorkSource" {
			t.Fatalf("%s active_work_source = %#v, want a $ref to the ActiveWorkSource component", route, field)
		}
	}
}

func assertEnum(t *testing.T, field map[string]any, want ...string) {
	t.Helper()
	raw, ok := field["enum"].([]any)
	if !ok {
		t.Fatalf("field %#v has no enum", field)
	}
	got := map[string]bool{}
	for _, value := range raw {
		got[value.(string)] = true
	}
	if len(got) != len(want) {
		t.Fatalf("enum = %v, want %v", raw, want)
	}
	for _, value := range want {
		if !got[value] {
			t.Fatalf("enum %v is missing %q", raw, value)
		}
	}
}
