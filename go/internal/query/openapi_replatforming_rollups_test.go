// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecIncludesReplatformingRollups(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/replatforming/rollups")
	post := testutil.MustMapField(t, path, "post")
	if got, want := post["operationId"], "rollupReplatformingReadiness"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := testutil.MustMapField(t, post, "responses")
	ok := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, ok, "content")
	jsonContent := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, jsonContent, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	if _, ok := properties["dimensions"]; !ok {
		t.Fatal("replatforming rollups response schema missing dimensions")
	}
	if _, ok := properties["readiness_totals"]; !ok {
		t.Fatal("replatforming rollups response schema missing readiness_totals")
	}

	components := testutil.MustMapField(t, spec, "components")
	schemas := testutil.MustMapField(t, components, "schemas")
	if _, ok := schemas["ReplatformingRollupBucket"]; !ok {
		t.Fatal("components.schemas missing ReplatformingRollupBucket")
	}
	if _, ok := schemas["ReplatformingReadinessCounts"]; !ok {
		t.Fatal("components.schemas missing ReplatformingReadinessCounts")
	}
}
