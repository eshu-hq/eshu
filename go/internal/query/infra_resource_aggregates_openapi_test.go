// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestInfraAggregateOpenAPIDocumentsCategoryTruthBases pins that both infra
// aggregate routes document every truth basis an unscoped read can report
// after the backfill: hybrid for a mixed label set, content_index for a
// table-only category such as k8s, and authoritative_graph for the
// graph-only cloud category (infraResourceAggregateTruth in the handler).
func TestInfraAggregateOpenAPIDocumentsCategoryTruthBases(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI spec has no paths object")
	}
	// The table-only categories come from the production split, so the
	// documented list cannot drift from the categories that report
	// content_index.
	var tableOnly []string
	for category, labels := range infraCategoryLabels {
		table, graphWhole, graphMixed := splitInfraLabels(labels)
		if len(table) > 0 && len(graphWhole)+len(graphMixed) == 0 {
			tableOnly = append(tableOnly, category)
		}
	}
	sort.Strings(tableOnly)
	if len(tableOnly) < 2 {
		t.Fatalf("table-only categories = %v, want at least two", tableOnly)
	}
	tableOnlyPhrase := "category=" + strings.Join(tableOnly[:len(tableOnly)-1], ", ") +
		", or " + tableOnly[len(tableOnly)-1]

	for _, path := range []string{"/api/v0/infra/resources/count", "/api/v0/infra/resources/inventory"} {
		route, _ := paths[path].(map[string]any)
		get, _ := route["get"].(map[string]any)
		description, _ := get["description"].(string)
		if description == "" {
			t.Fatalf("%s GET has no description", path)
		}
		for _, want := range []string{
			"hybrid",
			tableOnlyPhrase,
			"content_index",
			"category=cloud",
			"authoritative_graph",
		} {
			if !strings.Contains(description, want) {
				t.Errorf("%s GET description does not mention %q:\n%s", path, want, description)
			}
		}
	}
}
