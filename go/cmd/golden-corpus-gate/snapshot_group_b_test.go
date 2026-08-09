// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

func TestGoldenHarnessSeedsDocumentationAfterFinalDrainBeforeQuery(t *testing.T) {
	t.Parallel()

	scriptPath := filepath.Join("..", "..", "..", "scripts", "verify-golden-corpus-gate.sh")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	script := string(body)

	drainAt := strings.Index(script, "\nrun_maintenance_drain_cycles\n")
	suppressionDrainAt := strings.Index(script, "\ngolden_suppression_verify_producer_truth\n")
	seedAt := strings.Index(script, "\nseed_golden_documentation_fixture\n")
	queryAt := strings.Index(script, "start_bg mcp-server mcp_pid")
	if drainAt < 0 || suppressionDrainAt < 0 || seedAt < 0 || queryAt < 0 {
		t.Fatalf("missing ordered harness stages: maintenance=%d suppression=%d seed=%d query=%d", drainAt, suppressionDrainAt, seedAt, queryAt)
	}
	if drainAt >= suppressionDrainAt || suppressionDrainAt >= seedAt || seedAt >= queryAt {
		t.Fatalf("documentation seed ordering = maintenance:%d suppression:%d seed:%d query:%d, want maintenance < suppression < seed < query", drainAt, suppressionDrainAt, seedAt, queryAt)
	}

	helperPath := filepath.Join("..", "..", "..", "scripts", "lib", "golden-corpus-documentation-fixture.sh")
	helperBody, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read %s: %v", helperPath, err)
	}
	helper := string(helperBody)
	for _, want := range []string{
		"seed_golden_documentation_fixture()",
		"ON CONFLICT (fact_id) DO UPDATE",
		"golden-documentation-section",
		"golden-documentation-finding",
		"golden-documentation-evidence-packet",
		"documentation_section",
		"documentation_finding",
		"documentation_evidence_packet",
		"'generated_at', NOW()",
	} {
		if !strings.Contains(helper, want) {
			t.Errorf("documentation fixture helper missing %q", want)
		}
	}
	if strings.Contains(helper, "'generated_at', '2026-") {
		t.Error("documentation fixture generated_at must be seeded at run time, not frozen to a calendar date")
	}
}

func TestGoldenDocumentationFixtureMatchesRegisteredFactContracts(t *testing.T) {
	t.Parallel()

	helperPath := filepath.Join("..", "..", "..", "scripts", "lib", "golden-corpus-documentation-fixture.sh")
	body, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read %s: %v", helperPath, err)
	}
	helper := string(body)
	sectionAt := strings.Index(helper, "'golden-documentation-section'::text AS fact_id")
	findingAt := strings.Index(helper, "'golden-documentation-finding',")
	packetAt := strings.Index(helper, "'golden-documentation-evidence-packet',")
	insertAt := strings.Index(helper, "INSERT INTO fact_records (")
	if sectionAt < 0 || findingAt <= sectionAt || packetAt <= findingAt || insertAt <= packetAt {
		t.Fatalf("documentation fixture segments not ordered: section=%d finding=%d packet=%d insert=%d", sectionAt, findingAt, packetAt, insertAt)
	}

	tests := []struct {
		name           string
		kind           string
		segment        string
		requiredFields []string
	}{
		{
			name:           "section",
			kind:           facts.DocumentationSectionFactKind,
			segment:        helper[sectionAt:findingAt],
			requiredFields: []string{"document_id", "revision_id", "section_id"},
		},
		{
			name:           "finding",
			kind:           facts.DocumentationFindingFactKind,
			segment:        helper[findingAt:packetAt],
			requiredFields: []string{"finding_id", "finding_version"},
		},
		{
			name:           "evidence packet",
			kind:           facts.DocumentationEvidencePacketFactKind,
			segment:        helper[packetAt:insertAt],
			requiredFields: []string{"packet_id", "finding_id"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version, ok := facts.DocumentationSchemaVersion(test.kind)
			if !ok {
				t.Fatalf("DocumentationSchemaVersion(%q) is not registered", test.kind)
			}
			for _, want := range []string{"'" + test.kind + "'", "'" + version + "'"} {
				if !strings.Contains(test.segment, want) {
					t.Errorf("fixture segment missing registered contract token %q", want)
				}
			}
			for _, field := range test.requiredFields {
				if !strings.Contains(test.segment, "'"+field+"'") {
					t.Errorf("fixture segment missing required payload field %q", field)
				}
			}
		})
	}

	for _, column := range []string{
		"fact_id", "scope_id", "generation_id", "fact_kind", "stable_fact_key",
		"schema_version", "collector_kind", "fencing_token", "source_confidence",
		"source_system", "source_fact_key", "observed_at", "ingested_at",
		"is_tombstone", "payload",
	} {
		if !strings.Contains(helper[insertAt:], column) {
			t.Errorf("fact_records upsert missing required column %q", column)
		}
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

	const dependencyKey = "GET /api/v0/dependencies?direction=forward&package=github.com/acme/lib-common&ecosystem=go&limit=10"
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
	if got := decorators.RequiredJSONValues["source_backend"]; got != "hybrid_graph_and_content" {
		t.Fatalf("execute_language_query source_backend = %#v, want hybrid_graph_and_content", got)
	}

	developerPlan := snapshot.QueryShapes.MCP["plan_developer_change"]
	wantDeveloperPlanArguments := map[string]any{
		"repo_id":          "orders-api",
		"changed_paths":    []any{"main.go"},
		"developer_intent": "query envelope",
		"limit":            float64(10),
	}
	if !reflect.DeepEqual(developerPlan.Arguments, wantDeveloperPlanArguments) {
		t.Fatalf("plan_developer_change arguments = %#v, want %#v", developerPlan.Arguments, wantDeveloperPlanArguments)
	}
	assertSnapshotValues(t, developerPlan.RequiredJSONValues, map[string]any{
		"changed_file_count": float64(1),
		"change_set.repo_id": "orders-api",
		"change_set.mode":    "file_list",
		"schema_version":     "developer_change_plan.v1",
		"workflow":           "developer_change_plan",
		"read_only":          true,
	})
	wantChangedFile := []map[string]any{{"repo_id": "orders-api", "path": "main.go", "status": "modified"}}
	if got := developerPlan.RequiredJSONObjectMatches["changed_files[]"]; !reflect.DeepEqual(got, wantChangedFile) {
		t.Fatalf("plan_developer_change changed file match = %#v, want %#v", got, wantChangedFile)
	}
}

func TestGoldenSnapshotDocumentationFamilyIsNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	listShapes := []struct {
		tool         string
		resultsField string
		valuePath    string
		value        any
	}{
		{
			tool:         "list_documentation_facts",
			resultsField: "facts",
			valuePath:    "facts[].fact_id",
			value:        "golden-documentation-section",
		},
		{
			tool:         "list_documentation_findings",
			resultsField: "findings",
			valuePath:    "findings[].finding_id",
			value:        "documentation-finding:runtime-readiness",
		},
		{
			tool:         "get_documentation_finding_inventory",
			resultsField: "buckets",
			valuePath:    "buckets[].value",
			value:        "active",
		},
	}
	for _, test := range listShapes {
		t.Run(test.tool, func(t *testing.T) {
			shape := snapshot.QueryShapes.MCP[test.tool]
			if shape.MinimumResults != 1 || shape.MaximumResults != 1 {
				t.Errorf("result bounds = [%d,%d], want [1,1]", shape.MinimumResults, shape.MaximumResults)
			}
			if shape.ResultsField != test.resultsField {
				t.Errorf("results_field = %q, want %q", shape.ResultsField, test.resultsField)
			}
			if got := shape.RequiredJSONValues[test.valuePath]; got != test.value {
				t.Errorf("required_json_values[%q] = %#v, want %#v", test.valuePath, got, test.value)
			}
		})
	}

	objectShapes := []struct {
		tool      string
		valuePath string
		value     any
	}{
		{
			tool:      "get_documentation_evidence_packet",
			valuePath: "packet_id",
			value:     "documentation-packet:runtime-readiness",
		},
		{
			tool:      "count_documentation_findings",
			valuePath: "total_findings",
			value:     float64(1),
		},
		{
			tool:      "check_documentation_evidence_packet_freshness",
			valuePath: "freshness_state",
			value:     "fresh",
		},
	}
	for _, test := range objectShapes {
		t.Run(test.tool, func(t *testing.T) {
			shape := snapshot.QueryShapes.MCP[test.tool]
			if shape.ExpectedErrorContains != "" {
				t.Errorf("expected_error_contains = %q, want empty positive assertion", shape.ExpectedErrorContains)
			}
			if got := shape.RequiredJSONValues[test.valuePath]; got != test.value {
				t.Errorf("required_json_values[%q] = %#v, want %#v", test.valuePath, got, test.value)
			}
		})
	}
}

func TestGoldenSnapshotEnvironmentCompareUsesTwoMaterializedEnvironments(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["compare_environments"]
	for argument, want := range map[string]any{
		"workload_id": "workload:deployable-source",
		"left":        "stage",
		"right":       "prod",
	} {
		if got := shape.Arguments[argument]; got != want {
			t.Errorf("arguments[%q] = %#v, want %#v", argument, got, want)
		}
	}
	for path, want := range map[string]any{
		"workload.id":       "workload:deployable-source",
		"left.status":       "present",
		"left.environment":  "stage",
		"left.instance.id":  "workload-instance:deployable-source:stage",
		"right.status":      "present",
		"right.environment": "prod",
		"right.instance.id": "workload-instance:deployable-source:prod",
		"confidence":        float64(1),
		"reason":            "Environments are identical",
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Errorf("required_json_values[%q] = %#v, want %#v", path, got, want)
		}
	}
	for _, path := range []string{"left.provenance[]", "right.provenance[]"} {
		if !containsString(shape.RequiredJSONPaths, path) {
			t.Errorf("required_json_paths missing %q", path)
		}
	}
}
