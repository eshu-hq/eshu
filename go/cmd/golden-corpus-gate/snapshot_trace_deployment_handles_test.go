// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"slices"
	"testing"
)

// traceDeploymentDerivedFamilies are the families the #7174 MCP default
// (evidence_detail handles, no sections) omits from trace_deployment_chain.
var traceDeploymentDerivedFamilies = []string{
	"delivery_paths", "deployment_facts", "controller_driven_paths", "k8s_relationships",
	"topology_edges", "artifact_lineage", "network_paths", "entrypoints",
}

// mcpTraceDeploymentHandlesBody is a hand-written trace_deployment_chain MCP
// data payload in the post-#7174 default shape for api-svc: handle rows,
// derived families absent, section_detail and evidence_detail present.
const mcpTraceDeploymentHandlesBody = `{
  "service_name": "api-svc", "workload_id": "workload:api-svc", "name": "api-svc", "kind": "service",
  "repo_id": "repository:r_api", "repo_name": "api-svc",
  "subject": {"type": "service", "id": "workload:api-svc", "name": "api-svc"},
  "instances": [{"instance_id": "workload-instance:api-svc:prod", "environment": "prod", "platform_name": "", "platform_kind": ""}],
  "provisioned_platforms": [], "deployment_sources": [], "cloud_resources": [], "k8s_resources": [],
  "image_refs": [], "story": "api-svc has no deployment evidence.",
  "story_sections": [], "deployment_overview": {}, "controller_overview": {}, "gitops_overview": {},
  "runtime_overview": {}, "provenance_overview": {}, "documentation_overview": {}, "support_overview": {},
  "api_surface": {"endpoint_count": 2}, "deployment_evidence": {"artifact_count": 0, "artifacts": []},
  "deployment_fact_summary": {"overall_confidence_reason": "no_deployment_evidence"},
  "cloud_resource_limits": {}, "deployment_source_limits": {}, "k8s_resource_limits": {}, "runtime_topology_limits": {},
  "drilldowns": {}, "evidence_boundaries": [{"domain": "ci_cd_run_correlation"}],
  "evidence_detail": "handles",
  "section_detail": {
    "instances": {"detail": "handles", "returned": 1, "total": 1, "drilldown_tool": "trace_deployment_chain"},
    "delivery_paths": {"detail": "omitted", "returned": 0, "total": 0, "drilldown_tool": "trace_deployment_chain"}
  }
}`

// TestGoldenSnapshotTraceDeploymentChainMCPShapeMatchesHandlesDefault runs the
// committed MCP trace_deployment_chain shape through the real evaluator
// against a post-#7174 MCP-default body. It proves no required field or pinned
// value path points at a family the handles default omits, and that such a
// stale pin would fail rather than pass vacuously.
func TestGoldenSnapshotTraceDeploymentChainMCPShapeMatchesHandlesDefault(t *testing.T) {
	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape, ok := snapshot.QueryShapes.MCP["trace_deployment_chain"]
	if !ok {
		t.Fatal("query_shapes.mcp missing trace_deployment_chain")
	}
	for _, family := range traceDeploymentDerivedFamilies {
		if slices.Contains(shape.RequiredResponseFields, family) {
			t.Fatalf("MCP trace_deployment_chain requires %q, which the handles default omits", family)
		}
	}
	if got := shape.RequiredJSONValues["evidence_detail"]; got != "handles" {
		t.Fatalf("MCP trace_deployment_chain evidence_detail pin = %#v, want handles", got)
	}
	if finding := EvaluateQueryShape("trace_deployment_chain", shape, []byte(mcpTraceDeploymentHandlesBody)); !finding.OK {
		t.Fatalf("committed MCP shape rejects the handles-default body: %s", finding.Detail)
	}

	staleField := shape
	staleField.RequiredResponseFields = append(slices.Clone(shape.RequiredResponseFields), "delivery_paths")
	if finding := EvaluateQueryShape("trace_deployment_chain", staleField, []byte(mcpTraceDeploymentHandlesBody)); finding.OK {
		t.Fatal("a stale required delivery_paths field passed against the handles-default body")
	}
	staleValue := shape
	staleValue.RequiredJSONValues = map[string]any{"deployment_facts[].type": "deployment_source"}
	for path, want := range shape.RequiredJSONValues {
		staleValue.RequiredJSONValues[path] = want
	}
	if finding := EvaluateQueryShape("trace_deployment_chain", staleValue, []byte(mcpTraceDeploymentHandlesBody)); finding.OK {
		t.Fatal("a stale deployment_facts value pin passed against the handles-default body")
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(mcpTraceDeploymentHandlesBody), &body); err != nil {
		t.Fatalf("decode handles body: %v", err)
	}
	for _, family := range traceDeploymentDerivedFamilies {
		if _, present := body[family]; present {
			t.Fatalf("handles body fixture carries omitted family %q", family)
		}
	}
}
