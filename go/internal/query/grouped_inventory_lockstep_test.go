// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPISpecSupplyChainImpactInventoryGroupedDimensions pins the #9873
// lockstep between the handler's eight group_by values, the closed
// completeness vocabulary, and the published inventory schema.
func TestOpenAPISpecSupplyChainImpactInventoryGroupedDimensions(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/inventory")
	get := testutil.MustMapField(t, path, "get")

	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	var groupBy map[string]any
	for _, raw := range parameters {
		parameter, _ := raw.(map[string]any)
		if parameter["name"] == "group_by" {
			groupBy = testutil.MustMapField(t, parameter, "schema")
		}
	}
	if groupBy == nil {
		t.Fatal("inventory parameters lack group_by")
	}
	enum := mustStringSliceField(t, groupBy, "enum")
	want := []string{
		string(impact.InventoryByStatus), string(impact.InventoryByPriorityBucket),
		string(impact.InventoryBySeverity), string(impact.InventoryByRepository),
		string(impact.InventoryByEcosystem), string(impact.GroupedInventoryByService),
		string(impact.GroupedInventoryByWorkload), string(impact.GroupedInventoryByEnvironment),
	}
	if len(enum) != len(want) {
		t.Fatalf("group_by enum = %v, want the eight dimensions %v", enum, want)
	}
	for _, dimension := range want {
		if !slices.Contains(enum, dimension) {
			t.Errorf("group_by enum %v lacks %q", enum, dimension)
		}
	}

	twoHundred := testutil.MustMapField(t, testutil.MustMapField(t, get, "responses"), "200")
	schema := testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, twoHundred, "content"), "application/json"), "schema")
	props := testutil.MustMapField(t, schema, "properties")
	for _, key := range []string{"distinct_findings", "freshness"} {
		if _, ok := props[key]; !ok {
			t.Errorf("inventory 200 schema lacks %q", key)
		}
	}
	items := testutil.MustMapField(t, testutil.MustMapField(t, props, "buckets"), "items")
	itemProps := testutil.MustMapField(t, items, "properties")
	for _, key := range []string{
		"dimension", "value", "count", "unattributed", "by_impact_status",
		"by_severity", "max_priority_score", "completeness", "missing_evidence",
	} {
		if _, ok := itemProps[key]; !ok {
			t.Errorf("bucket schema lacks %q", key)
		}
	}
	completeness := mustStringSliceField(t, testutil.MustMapField(t, itemProps, "completeness"), "enum")
	for _, c := range []impact.GroupCompleteness{
		impact.GroupCompletenessComplete, impact.GroupCompletenessPartial,
		impact.GroupCompletenessUnattributed, impact.GroupCompletenessReadModelUnavailable,
	} {
		if !slices.Contains(completeness, string(c)) {
			t.Errorf("completeness enum %v lacks %q", completeness, c)
		}
	}
	if len(completeness) != 4 {
		t.Errorf("completeness enum = %v, want exactly the four closed values", completeness)
	}
}
