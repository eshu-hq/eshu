// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

func TestGoldenSnapshotSupplyChainRuntimeEnvironmentEvidenceIsNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	const listHTTPKey = "GET /api/v0/supply-chain/impact/findings?limit=50&subject_digest=" + kubernetesRuntimeDigest + "&profile=comprehensive"
	const explainHTTPKey = "GET /api/v0/supply-chain/impact/explain?cve_id=CVE-2026-00010&subject_digest=" + kubernetesRuntimeDigest
	tests := map[string]struct {
		shape      QueryShape
		valuePath  string
		findingKey string
		list       bool
	}{
		"http-list": {
			shape:      snapshot.QueryShapes.HTTP[listHTTPKey],
			valuePath:  "findings[].runtime_context.environment_evidence.prod",
			findingKey: "findings",
			list:       true,
		},
		"mcp-list": {
			shape:      snapshot.QueryShapes.MCP["list_supply_chain_impact_findings"],
			valuePath:  "findings[].runtime_context.environment_evidence.prod",
			findingKey: "findings",
			list:       true,
		},
		"http-explain": {
			shape:      snapshot.QueryShapes.HTTP[explainHTTPKey],
			valuePath:  "finding.runtime_context.environment_evidence.prod",
			findingKey: "finding",
		},
		"mcp-explain": {
			shape:      snapshot.QueryShapes.MCP["explain_supply_chain_impact"],
			valuePath:  "finding.runtime_context.environment_evidence.prod",
			findingKey: "finding",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			prefix := "finding.runtime_context"
			if test.list {
				prefix = "findings[].runtime_context"
			}
			for path, want := range map[string]any{
				test.valuePath:             "deploy_event",
				prefix + ".truth_basis":    "read_time_resolved",
				prefix + ".environments[]": "prod",
				prefix + ".environment_evidence_probe.candidate_limit":      float64(1),
				prefix + ".environment_evidence_probe.candidates_truncated": false,
			} {
				if got := test.shape.RequiredJSONValues[path]; got != want {
					t.Errorf("required_json_values[%q] = %#v, want %#v", path, got, want)
				}
			}
			if test.list {
				if test.shape.MinimumResults < 1 || test.shape.ResultsField != "findings" {
					t.Fatalf(
						"list result contract = minimum %d field %q, want minimum >= 1 findings",
						test.shape.MinimumResults,
						test.shape.ResultsField,
					)
				}
			} else if !containsString(test.shape.RequiredResponseFields, "finding") {
				t.Fatalf("required_response_fields = %#v, want finding", test.shape.RequiredResponseFields)
			}
			assertMissingRuntimeEnvironmentEvidenceFails(t, test.shape, test.findingKey)
		})
	}
}

func assertMissingRuntimeEnvironmentEvidenceFails(t *testing.T, shape QueryShape, findingKey string) {
	t.Helper()

	response := fakeQueryShapeResponse(shape)
	var finding map[string]any
	if findingKey == "findings" {
		finding = firstFakeKubernetesRuntimeFinding(t, response)
	} else {
		var ok bool
		finding, ok = response[findingKey].(map[string]any)
		if !ok {
			t.Fatalf("fake response %s is not an object", findingKey)
		}
	}
	runtimeContext, ok := finding["runtime_context"].(map[string]any)
	if !ok {
		t.Fatal("fake response runtime_context is not an object")
	}
	delete(runtimeContext, "environment_evidence")

	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response without runtime environment evidence: %v", err)
	}
	if result := EvaluateQueryShape("runtime-environment-evidence-missing", shape, raw); result.OK {
		t.Fatalf("missing runtime_context.environment_evidence passed unexpectedly: %s", result.Detail)
	}
}
