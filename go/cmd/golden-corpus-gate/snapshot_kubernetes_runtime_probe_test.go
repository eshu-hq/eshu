// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

const kubernetesRuntimeDigest = "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

var kubernetesRuntimeWorkloadMatches = []map[string]any{
	{
		"workload_uid": "kubernetes_live:supply-chain-demo:apps/v1/deployments:default:supply-chain-demo",
		"cluster_id":   "supply-chain-demo",
		"namespace":    "default",
		"name":         "supply-chain-demo",
	},
	{
		"workload_uid": "kubernetes_live:supply-chain-demo:apps/v1/replicasets:default:supply-chain-demo-7f8d9",
		"cluster_id":   "supply-chain-demo",
		"namespace":    "default",
		"name":         "supply-chain-demo-7f8d9",
	},
	{
		"workload_uid": "kubernetes_live:supply-chain-demo:/v1/pods:default:supply-chain-demo-pod",
		"cluster_id":   "supply-chain-demo",
		"namespace":    "default",
		"name":         "supply-chain-demo-pod",
	},
}

func TestGoldenSnapshotSupplyChainKubernetesRuntimeProbeIsNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	const listHTTPKey = "GET /api/v0/supply-chain/impact/findings?limit=50&subject_digest=" + kubernetesRuntimeDigest + "&profile=comprehensive"
	const explainHTTPKey = "GET /api/v0/supply-chain/impact/explain?cve_id=CVE-2026-00010&subject_digest=" + kubernetesRuntimeDigest

	listShapes := map[string]QueryShape{
		"http-list": snapshot.QueryShapes.HTTP[listHTTPKey],
		"mcp-list":  snapshot.QueryShapes.MCP["list_supply_chain_impact_findings"],
	}
	for name, shape := range listShapes {
		t.Run(name, func(t *testing.T) {
			assertKubernetesRuntimeListShape(t, shape)
		})
	}
	if got := listShapes["mcp-list"].Arguments["subject_digest"]; got != kubernetesRuntimeDigest {
		t.Fatalf("MCP list subject_digest = %#v, want %q", got, kubernetesRuntimeDigest)
	}

	explainShapes := map[string]QueryShape{
		"http-explain": snapshot.QueryShapes.HTTP[explainHTTPKey],
		"mcp-explain":  snapshot.QueryShapes.MCP["explain_supply_chain_impact"],
	}
	for name, shape := range explainShapes {
		t.Run(name, func(t *testing.T) {
			assertKubernetesRuntimeExplainShape(t, shape)
		})
	}
	if got := explainShapes["mcp-explain"].Arguments["subject_digest"]; got != kubernetesRuntimeDigest {
		t.Fatalf("MCP explain subject_digest = %#v, want %q", got, kubernetesRuntimeDigest)
	}
}

func assertKubernetesRuntimeListShape(t *testing.T, shape QueryShape) {
	t.Helper()
	if shape.MinimumResults != 1 || shape.MaximumResults != 1 || shape.ResultsField != "findings" {
		t.Fatalf(
			"list result contract = [%d,%d] field %q, want [1,1] findings",
			shape.MinimumResults,
			shape.MaximumResults,
			shape.ResultsField,
		)
	}
	for _, field := range []string{
		"kubernetes_runtime_workload_refs",
		"kubernetes_runtime_probe",
		"deployment_truth_tier",
		"version_resolution_tier",
	} {
		if !containsString(shape.ResultItemRequiredFields, field) {
			t.Errorf("result_item_required_fields = %#v, want %q", shape.ResultItemRequiredFields, field)
		}
	}
	assertKubernetesRuntimeShapeValues(t, shape, "findings[]")
	assertKubernetesRuntimeHostileMutation(t, shape, "findings")
}

func assertKubernetesRuntimeExplainShape(t *testing.T, shape QueryShape) {
	t.Helper()
	if !containsString(shape.RequiredResponseFields, "finding") {
		t.Fatalf("required_response_fields = %#v, want finding", shape.RequiredResponseFields)
	}
	for path, want := range map[string]any{
		"outcome":              "finding_explained",
		"input.subject_digest": kubernetesRuntimeDigest,
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Errorf("required_json_values[%q] = %#v, want %#v", path, got, want)
		}
	}
	assertKubernetesRuntimeShapeValues(t, shape, "finding")
	assertKubernetesRuntimeHostileMutation(t, shape, "finding")
}

func assertKubernetesRuntimeShapeValues(t *testing.T, shape QueryShape, prefix string) {
	t.Helper()
	for path, want := range map[string]any{
		prefix + ".subject_digest":                                   kubernetesRuntimeDigest,
		prefix + ".kubernetes_runtime_probe.candidate_limit":         float64(200),
		prefix + ".kubernetes_runtime_probe.workload_refs_truncated": false,
		prefix + ".deployment_truth_tier":                            "runtime_confirmed",
		prefix + ".version_resolution_tier":                          "runtime_confirmed",
	} {
		if got := shape.RequiredJSONValues[path]; got != want {
			t.Errorf("required_json_values[%q] = %#v, want %#v", path, got, want)
		}
	}
	path := prefix + ".kubernetes_runtime_workload_refs[]"
	if got := shape.RequiredJSONObjectMatches[path]; !reflect.DeepEqual(got, kubernetesRuntimeWorkloadMatches) {
		t.Errorf("required_json_object_matches[%q] = %#v, want %#v", path, got, kubernetesRuntimeWorkloadMatches)
	}
}

func assertKubernetesRuntimeHostileMutation(t *testing.T, shape QueryShape, findingField string) {
	t.Helper()
	response := fakeQueryShapeResponse(shape)
	var container map[string]any
	if findingField == "findings" {
		container = firstFakeKubernetesRuntimeFinding(t, response)
	} else {
		var ok bool
		container, ok = response["finding"].(map[string]any)
		if !ok {
			t.Fatal("fake response finding is not an object")
		}
	}
	container["kubernetes_runtime_workload_refs"] = []any{}

	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal hostile response: %v", err)
	}
	if finding := EvaluateQueryShape("kubernetes-runtime-hostile", shape, raw); finding.OK {
		t.Fatalf("empty Kubernetes runtime refs passed unexpectedly: %s", finding.Detail)
	}

	response = fakeQueryShapeResponse(shape)
	if findingField == "findings" {
		container = firstFakeKubernetesRuntimeFinding(t, response)
	} else {
		container = response["finding"].(map[string]any)
	}
	delete(container, "kubernetes_runtime_probe")
	raw, err = json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal missing-probe response: %v", err)
	}
	if finding := EvaluateQueryShape("kubernetes-runtime-missing-probe", shape, raw); finding.OK {
		t.Fatalf("missing Kubernetes runtime probe passed unexpectedly: %s", finding.Detail)
	}
}

func firstFakeKubernetesRuntimeFinding(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	switch findings := response["findings"].(type) {
	case []map[string]any:
		if len(findings) > 0 {
			return findings[0]
		}
	case []any:
		if len(findings) > 0 {
			if finding, ok := findings[0].(map[string]any); ok {
				return finding
			}
		}
	}
	t.Fatal("fake response findings is empty")
	return nil
}
