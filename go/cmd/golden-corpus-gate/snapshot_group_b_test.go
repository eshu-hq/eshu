// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenHarnessSeedsDeadLetterAfterFinalDrainBeforeQuery(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join("..", "..", "..", "scripts", "verify-golden-corpus-gate.sh")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	script := string(body)

	drainAt := strings.Index(script, "\nrun_maintenance_drain_cycles\n")
	seedAt := strings.Index(script, "\nseed_golden_dead_letter_fixture\n")
	suppressionDrainAt := strings.Index(script, "\ngolden_suppression_verify_producer_truth\n")
	queryAt := strings.Index(script, "start_bg mcp-server mcp_pid")
	if drainAt < 0 || suppressionDrainAt < 0 || seedAt < 0 || queryAt < 0 {
		t.Fatalf("missing ordered harness stages: maintenance=%d suppression=%d seed=%d query=%d", drainAt, suppressionDrainAt, seedAt, queryAt)
	}
	if drainAt >= suppressionDrainAt || suppressionDrainAt >= seedAt || seedAt >= queryAt {
		t.Fatalf("dead-letter seed ordering = maintenance:%d suppression:%d seed:%d query:%d, want maintenance < suppression < seed < query", drainAt, suppressionDrainAt, seedAt, queryAt)
	}
}

func TestGoldenSnapshotDeadLetterListIsNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.MCP["list_dead_letter_work_items"]
	if !ok {
		t.Fatal("query_shapes.mcp missing list_dead_letter_work_items")
	}
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 {
		t.Fatalf("list_dead_letter_work_items result bounds = [%d,%d], want [1,1]", shape.MinimumResults, shape.MaximumResults)
	}
	if shape.ResultsField != "items" {
		t.Fatalf("list_dead_letter_work_items results_field = %q, want items", shape.ResultsField)
	}
	for path, want := range map[string]any{
		"items[].work_item_id":  "golden-dead-letter-fixture",
		"items[].failure_class": "golden_fixture",
		"items[].domain":        "golden_fixture",
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Errorf("list_dead_letter_work_items required_json_values[%q] = %#v, want %#v", path, got, want)
		}
	}
}

func TestGoldenSnapshotGroupBUsesCapabilitySpecificPositiveShapes(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	tests := []struct {
		name         string
		shape        QueryShape
		resultsField string
		valuePath    string
		value        any
	}{
		{
			name:         "contract impact",
			shape:        snapshot.QueryShapes.MCP["investigate_contract_impact"],
			resultsField: "providers",
			valuePath:    "providers[].provider_repo",
			value:        "api-svc",
		},
		{
			name:         "curated semantic search",
			shape:        snapshot.QueryShapes.MCP["search_semantic_context"],
			resultsField: "results",
			valuePath:    "retrieval_state",
			value:        "keyword_only",
		},
		{
			name:         "decorators",
			shape:        snapshot.QueryShapes.MCP["execute_language_query"],
			resultsField: "results",
			valuePath:    "results[].name",
			value:        "fetch_remote",
		},
		{
			name:         "developer change plan",
			shape:        snapshot.QueryShapes.MCP["plan_developer_change"],
			resultsField: "actions",
			valuePath:    "schema_version",
			value:        "developer_change_plan.v1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.shape.MinimumResults < 1 {
				t.Errorf("minimum_results = %d, want >= 1", test.shape.MinimumResults)
			}
			if test.shape.ResultsField != test.resultsField {
				t.Errorf("results_field = %q, want %q", test.shape.ResultsField, test.resultsField)
			}
			if got := test.shape.RequiredJSONValues[test.valuePath]; got != test.value {
				t.Errorf("required_json_values[%q] = %#v, want %#v", test.valuePath, got, test.value)
			}
		})
	}

	const dependencyKey = "GET /api/v0/dependencies?direction=forward&package=github.com/acme/lib-common&ecosystem=gomod&limit=10"
	dependencies, ok := snapshot.QueryShapes.HTTP[dependencyKey]
	if !ok {
		t.Fatalf("query_shapes.http missing %s", dependencyKey)
	}
	if dependencies.MinimumResults < 1 || dependencies.ResultsField != "dependencies" {
		t.Fatalf("dependencies result contract = minimum %d field %q, want >=1 dependencies", dependencies.MinimumResults, dependencies.ResultsField)
	}
	if got := dependencies.RequiredJSONValues["dependencies[].related_package"]; got != "github.com/acme/synthetic-dep" {
		t.Fatalf("dependencies related package pin = %#v, want github.com/acme/synthetic-dep", got)
	}

	decorators := snapshot.QueryShapes.MCP["execute_language_query"]
	if !containsString(decorators.RequiredJSONPaths, "results[].semantic_profile.decorators[]") {
		t.Fatal("execute_language_query must require a non-empty decorator value")
	}
}
