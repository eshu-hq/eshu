// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"
)

func TestGoldenSnapshotGroupBDeployedPinsAreNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	tests := []struct {
		name          string
		shapeName     string
		minimum       int
		resultsField  string
		arguments     map[string]any
		values        map[string]any
		paths         []string
		objectMatches map[string][]map[string]any
	}{
		{
			name:         "evidence citation packet",
			shapeName:    "build_evidence_citation_packet",
			minimum:      1,
			resultsField: "citations",
			arguments: map[string]any{
				"question": "What evidence supports the runtime readiness?",
				"handles": []any{map[string]any{
					"kind":          "file",
					"repo_id":       "repository:r_ea78e8bb",
					"relative_path": "main.go",
					"start_line":    float64(1),
					"end_line":      float64(20),
				}},
				"limit": float64(10),
			},
			values: map[string]any{
				"citations[].kind":            "file",
				"citations[].repo_id":         "repository:r_ea78e8bb",
				"citations[].relative_path":   "main.go",
				"coverage.input_handle_count": float64(1),
				"coverage.resolved_count":     float64(1),
				"coverage.missing_count":      float64(0),
				"coverage.source_backend":     "postgres_content_store",
			},
			paths: []string{"citations[].excerpt"},
		},
		{
			name:         "generation lifecycle",
			shapeName:    "get_generation_lifecycle",
			minimum:      1,
			resultsField: "generations",
			arguments: map[string]any{
				"scope_id": "prometheus_mimir:supply-chain-demo-metrics:supply-chain-demo",
				"limit":    float64(50),
			},
			values: map[string]any{
				"count":                                  float64(1),
				"generations[].scope_id":                 "prometheus_mimir:supply-chain-demo-metrics:supply-chain-demo",
				"generations[].generation_id":            "cassette-prometheus-scd-gen1",
				"generations[].source_system":            "prometheus_mimir",
				"generations[].collector_kind":           "prometheus_mimir",
				"generations[].is_active":                true,
				"generations[].status":                   "active",
				"generations[].queue_status.outstanding": float64(0),
			},
		},
		{
			name:         "graph summary packet",
			shapeName:    "get_graph_summary_packet",
			minimum:      1,
			resultsField: "hot_entities",
			arguments: map[string]any{
				"repo_id": "repository:r_ed3a9bab",
				"limit":   float64(10),
			},
			values: map[string]any{"scope": "repository", "repo_id": "repository:r_ed3a9bab"},
			paths: []string{
				"hot_entities[].function_id",
				"hot_entities[].function_name",
				"hot_entities[].incoming_calls",
				"hot_entities[].outgoing_calls",
				"hot_entities[].total_degree",
				"key_relationships.CALLS",
				"ecosystem_map.file_count",
			},
			objectMatches: map[string][]map[string]any{
				"hot_entities[]": {{"function_id": "content-entity:e_59c56c38911d", "function_name": "mutualPing", "incoming_calls": float64(1), "outgoing_calls": float64(1), "total_degree": float64(2)}},
			},
		},
		{
			name:      "hosted governance status",
			shapeName: "get_hosted_governance_status",
			values: map[string]any{
				"mode":                             "local_no_policy",
				"state":                            "disabled",
				"source_kind":                      "unknown",
				"identity.auth_mode":               "shared_token",
				"identity.configured":              true,
				"identity.shared_token_limitation": true,
				"readiness.identity":               true,
				"reasons": []any{
					"policy_not_configured",
					"shared_token_mode",
				},
			},
		},
		{
			name:         "incident context",
			shapeName:    "get_incident_context",
			minimum:      2,
			resultsField: "evidence_path",
			arguments: map[string]any{
				"provider_incident_id": "PSCD1",
				"limit":                float64(10),
			},
			values: map[string]any{
				"incident.provider":             "pagerduty",
				"incident.provider_incident_id": "PSCD1",
				"incident.title":                "Supply-chain-demo synthetic incident",
				"incident.status":               "resolved",
				"incident.service.id":           "SVCSCD1",
			},
			objectMatches: map[string][]map[string]any{
				"evidence_path[]": {
					{"slot": "incident", "truth_label": "exact"},
					{"slot": "service", "truth_label": "exact"},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			shape := snapshot.QueryShapes.MCP[test.shapeName]
			if got := shape.MinimumResults; got != test.minimum {
				t.Errorf("MinimumResults = %d, want %d", got, test.minimum)
			}
			if got := shape.ResultsField; got != test.resultsField {
				t.Errorf("ResultsField = %q, want %q", got, test.resultsField)
			}
			if !reflect.DeepEqual(shape.Arguments, test.arguments) {
				t.Errorf("Arguments = %#v, want %#v", shape.Arguments, test.arguments)
			}
			assertSnapshotValues(t, shape.RequiredJSONValues, test.values)
			assertSnapshotPaths(t, shape.RequiredJSONPaths, test.paths)
			for path, want := range test.objectMatches {
				if got := shape.RequiredJSONObjectMatches[path]; !reflect.DeepEqual(got, want) {
					t.Errorf("RequiredJSONObjectMatches[%q] = %#v, want %#v", path, got, want)
				}
			}
			if test.shapeName == "get_graph_summary_packet" && containsString(shape.RequiredResponseFields, "note") {
				t.Error("repo-scoped graph summary must not require the needs-repo note")
			}
		})
	}
}

func TestGoldenSnapshotOperationsStatusUsesDeployedEnvelope(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.HTTP["GET /api/v0/status/operations"]
	if !ok {
		t.Fatal("operations status HTTP shape is missing")
	}
	if !shape.Envelope {
		t.Error("operations status must retain its truth envelope")
	}
	assertSnapshotValues(t, shape.RequiredJSONValues, map[string]any{
		"data.scoped":            false,
		"data.limit":             float64(50),
		"data.health.state":      "degraded",
		"data.queue.outstanding": float64(0),
		"data.queue.dead_letter": float64(1),
		"data.health.reasons[]":  "1 work items are dead-lettered",
		"data.live_activity":     []any{},
		"truth.capability":       "operations.status",
		"truth.basis":            "runtime_state",
	})
	assertSnapshotPaths(t, shape.RequiredJSONPaths, []string{
		"data.collectors[]",
		"data.stage_summaries",
		"data.domain_backlogs",
	})
}

func assertSnapshotValues(t *testing.T, got, want map[string]any) {
	t.Helper()
	for path, value := range want {
		if actual := got[path]; !reflect.DeepEqual(actual, value) {
			t.Errorf("RequiredJSONValues[%q] = %#v, want %#v", path, actual, value)
		}
	}
}

func assertSnapshotPaths(t *testing.T, got, want []string) {
	t.Helper()
	for _, path := range want {
		if !containsString(got, path) {
			t.Errorf("RequiredJSONPaths missing %q", path)
		}
	}
}
