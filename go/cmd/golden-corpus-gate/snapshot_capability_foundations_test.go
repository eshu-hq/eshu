// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"
)

const (
	semanticObservationHTTPShape = "GET /api/v0/semantic/documentation-observations?provider_profile_id=semantic-docs-default&limit=25"
	serviceCatalogHTTPShape      = "GET /api/v0/service-catalog/correlations?limit=10&repository_id=repository:r_217415d9"
	metricsTimeSeriesHTTPShape   = "GET /api/v0/metrics/timeseries?metric=queue_depth&window=1h&step=5m"
)

func TestGoldenSnapshotSemanticObservationPinsCassetteTruth(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	for name, shape := range map[string]QueryShape{
		"http": snapshot.QueryShapes.HTTP[semanticObservationHTTPShape],
		"mcp":  snapshot.QueryShapes.MCP["list_semantic_documentation_observations"],
	} {
		t.Run(name, func(t *testing.T) {
			if shape.MinimumResults != 1 || shape.MaximumResults != 1 || shape.ResultsField != "observations" {
				t.Fatalf("semantic observation bounds/field = [%d,%d] %q, want [1,1] observations", shape.MinimumResults, shape.MaximumResults, shape.ResultsField)
			}
			assertSnapshotValues(t, shape.RequiredJSONValues, map[string]any{
				"observations[].fact_kind":           "semantic.documentation_observation",
				"observations[].observation_id":      "semantic-doc-obs:0f6b6e56538c417114a768b112ca500f1bc991c78d9a60a8e21a5e11744a0ceb",
				"observations[].observation_type":    "runtime_readiness_summary",
				"observations[].provider_profile_id": "semantic-docs-default",
				"observations[].freshness_state":     "fresh",
				"observations[].policy_state":        "allowed",
				"observations[].admission_state":     "provenance_only",
			})
		})
	}
}

func TestGoldenSnapshotServiceCatalogPinsRepoLocalBackstageTruth(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	for name, shape := range map[string]QueryShape{
		"http": snapshot.QueryShapes.HTTP[serviceCatalogHTTPShape],
		"mcp":  snapshot.QueryShapes.MCP["list_service_catalog_correlations"],
	} {
		t.Run(name, func(t *testing.T) {
			if shape.MinimumResults != 1 || shape.MaximumResults != 1 || shape.ResultsField != "correlations" {
				t.Fatalf("service catalog bounds/field = [%d,%d] %q, want [1,1] correlations", shape.MinimumResults, shape.MaximumResults, shape.ResultsField)
			}
			assertSnapshotValues(t, shape.RequiredJSONValues, map[string]any{
				"correlations[].provider":        "backstage",
				"correlations[].entity_ref":      "component:default/deployable-config",
				"correlations[].entity_type":     "service",
				"correlations[].repository_id":   "repository:r_217415d9",
				"correlations[].service_id":      "component:default/deployable-config",
				"correlations[].owner_ref":       "group:default/runtime-platform",
				"correlations[].lifecycle":       "production",
				"correlations[].outcome":         "exact",
				"correlations[].provenance_only": false,
				"correlations[].drift_status":    "matches",
			})
		})
	}
}

func TestGoldenSnapshotHardcodedSecretPinsRedactedFixture(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["investigate_hardcoded_secrets"]
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 || shape.ResultsField != "findings" {
		t.Fatalf("hardcoded-secret bounds/field = [%d,%d] %q, want [1,1] findings", shape.MinimumResults, shape.MaximumResults, shape.ResultsField)
	}
	wantArguments := map[string]any{
		"repo_id":            "repository:r_217415d9",
		"finding_kinds":      []any{"password_literal"},
		"include_suppressed": false,
		"limit":              float64(10),
		"offset":             float64(0),
	}
	if !reflect.DeepEqual(shape.Arguments, wantArguments) {
		t.Fatalf("Arguments = %#v, want %#v", shape.Arguments, wantArguments)
	}
	assertSnapshotValues(t, shape.RequiredJSONValues, map[string]any{
		"count":                       float64(1),
		"coverage.redaction":          "secret_values_replaced_with_redacted_marker",
		"coverage.suppressed_count":   float64(0),
		"findings[].repo_id":          "repository:r_217415d9",
		"findings[].relative_path":    "config/runtime.cfg",
		"findings[].finding_kind":     "password_literal",
		"findings[].confidence":       "medium",
		"findings[].severity":         "high",
		"findings[].redacted_excerpt": `password = "[REDACTED]"`,
		"findings[].suppressed":       false,
	})
	if got := shape.RequiredJSONValues["findings[].redacted_excerpt"]; got == `password = "invalid-by-design"` {
		t.Fatal("snapshot exposes the synthetic secret value instead of the redaction marker")
	}
}

func TestGoldenSnapshotMetricsTimeSeriesRequiresPositiveFreshRange(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP[metricsTimeSeriesHTTPShape]
	if !ok {
		t.Fatal("metrics time-series HTTP shape is missing")
	}
	if !shape.Envelope {
		t.Fatal("metrics time-series shape must assert the truth envelope")
	}
	assertSnapshotPaths(t, shape.RequiredJSONPaths, []string{"data.points[].t", "data.points[].v"})
	assertSnapshotValues(t, shape.RequiredJSONValues, map[string]any{
		"data.metric":           "queue_depth",
		"data.unit":             "items",
		"data.window":           "1h",
		"data.step":             "5m",
		"truth.capability":      "platform_metrics.timeseries",
		"truth.level":           "derived",
		"truth.basis":           "semantic_facts",
		"truth.freshness.state": "fresh",
	})
	wantMatches := []map[string]any{{"v": float64(2)}, {"v": float64(0)}}
	if got := shape.RequiredJSONObjectMatches["data.points[]"]; !reflect.DeepEqual(got, wantMatches) {
		t.Fatalf("RequiredJSONObjectMatches[data.points[]] = %#v, want %#v", got, wantMatches)
	}

	empty := []byte(`{"data":{"metric":"queue_depth","unit":"items","window":"1h","step":"5m","points":[]},"truth":{"capability":"platform_metrics.timeseries","level":"fallback","basis":"semantic_facts","freshness":{"state":"unavailable"}},"error":null}`)
	if finding := EvaluateQueryShape("metrics-empty", shape, empty); finding.OK {
		t.Fatalf("empty fallback response passed: %+v", finding)
	}
	positive := []byte(`{"data":{"metric":"queue_depth","unit":"items","window":"1h","step":"5m","points":[{"t":"2026-08-08T12:00:00Z","v":2},{"t":"2026-08-08T13:00:00Z","v":0}]},"truth":{"capability":"platform_metrics.timeseries","level":"derived","basis":"semantic_facts","freshness":{"state":"fresh"}},"error":null}`)
	if finding := EvaluateQueryShape("metrics-positive", shape, positive); !finding.OK {
		t.Fatalf("positive bounded range failed: %+v", finding)
	}
}

func TestGoldenSnapshotDataflowPinsMeasuredFixtureTruth(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	wantArguments := map[string]any{
		"repo_id": "repository:r_8477a002", "language": "go",
		"symbol": "GoldenDataflowHandler", "file_path": "dataflow_proof.go",
		"limit": float64(1),
	}
	for _, test := range []struct {
		name         string
		resultsField string
		values       map[string]any
	}{
		{name: "dispatch_cfg_summary", resultsField: "functions", values: map[string]any{
			"query.kind": "cfg_summary", "functions[].def_use_count": float64(4),
			"functions[].control_dependence_count": float64(1), "functions[].overflow": false,
		}},
		{name: "dispatch_pdg_summary", resultsField: "summaries", values: map[string]any{
			"query.kind": "pdg_summary", "summaries[].cfg_block_count": float64(3),
			"summaries[].cfg_edge_count": float64(3), "summaries[].def_use_count": float64(4),
			"summaries[].control_dependence_count": float64(1), "summaries[].coverage_state": "derived",
		}},
		{name: "dispatch_taint_path", resultsField: "paths", values: map[string]any{
			"query.kind": "taint_path", "paths[].source_kind": "http_request",
			"paths[].sink_kind": "sql", "paths[].source_line": float64(13),
			"paths[].sink_line": float64(15), "paths[].confidence": float64(0.8),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			shape := snapshot.QueryShapes.MCP[test.name]
			if shape.MinimumResults != 1 || shape.MaximumResults != 1 || shape.ResultsField != test.resultsField {
				t.Fatalf("bounds/field = [%d,%d] %q, want [1,1] %s", shape.MinimumResults, shape.MaximumResults, shape.ResultsField, test.resultsField)
			}
			if !reflect.DeepEqual(shape.Arguments, wantArguments) {
				t.Fatalf("Arguments = %#v, want %#v", shape.Arguments, wantArguments)
			}
			common := map[string]any{
				"query.repo_id": "repository:r_8477a002", "query.language": "go",
				"query.symbol": "GoldenDataflowHandler", "query.file_path": "dataflow_proof.go",
				"bounds.count": float64(1), "bounds.truncated": test.name == "dispatch_taint_path",
			}
			if test.name == "dispatch_taint_path" {
				common["bounds.limit"] = float64(1)
			}
			for path, value := range test.values {
				common[path] = value
			}
			assertSnapshotValues(t, shape.RequiredJSONValues, common)
			empty := []byte(`{"ambiguity":{},"bounds":{"count":0,"truncated":false},"coverage":{"state":"partial"},"query":{},"functions":[],"summaries":[],"paths":[]}`)
			if finding := EvaluateQueryShape(test.name+"-empty", shape, empty); finding.OK {
				t.Fatalf("empty dataflow response passed: %+v", finding)
			}
		})
	}

	cfg := snapshot.QueryShapes.MCP["dispatch_cfg_summary"]
	if got, want := cfg.RequiredJSONObjectMatches["functions[].cfg.blocks[]"], []map[string]any{{"id": float64(0)}, {"id": float64(1)}, {"id": float64(2)}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CFG block matches = %#v, want %#v", got, want)
	}
	if got, want := cfg.RequiredJSONObjectMatches["functions[].cfg.edges[]"], []map[string]any{{"from": float64(0), "to": float64(1)}, {"from": float64(0), "to": float64(2)}, {"from": float64(2), "to": float64(1)}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CFG edge matches = %#v, want %#v", got, want)
	}
}
