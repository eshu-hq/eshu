// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesReplatformingRollups(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/replatforming/rollups")
	post := mustMapField(t, path, "post")
	if got, want := post["operationId"], "rollupReplatformingReadiness"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := mustMapField(t, post, "responses")
	ok := mustMapField(t, responses, "200")
	content := mustMapField(t, ok, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	if _, ok := properties["dimensions"]; !ok {
		t.Fatal("replatforming rollups response schema missing dimensions")
	}
	if _, ok := properties["readiness_totals"]; !ok {
		t.Fatal("replatforming rollups response schema missing readiness_totals")
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	if _, ok := schemas["ReplatformingRollupBucket"]; !ok {
		t.Fatal("components.schemas missing ReplatformingRollupBucket")
	}
	if _, ok := schemas["ReplatformingReadinessCounts"]; !ok {
		t.Fatal("components.schemas missing ReplatformingReadinessCounts")
	}
}
