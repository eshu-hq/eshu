// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAPISpecDocumentsSupplyChainRuntimeContextRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := mustMapField(t, path, "get")
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	findings := mustMapField(t, properties, "findings")
	items := mustMapField(t, findings, "items")
	itemProperties := mustMapField(t, items, "properties")
	runtimeContext := mustMapField(t, itemProperties, "runtime_context")
	runtimeContextProperties := mustMapField(t, runtimeContext, "properties")
	environmentEvidence := mustMapField(t, runtimeContextProperties, "environment_evidence")
	additionalProperties := mustMapField(t, environmentEvidence, "additionalProperties")
	if got, want := additionalProperties["enum"], []any{"deploy_event", "declared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime_context.environment_evidence enum = %#v, want %#v", got, want)
	}
	probe := mustMapField(t, runtimeContextProperties, "environment_evidence_probe")
	probeProperties := mustMapField(t, probe, "properties")
	if got := mustMapField(t, probeProperties, "candidate_limit")["maximum"]; got != float64(200) {
		t.Fatalf("environment_evidence_probe.candidate_limit maximum = %#v, want 200", got)
	}
	if got := mustMapField(t, probeProperties, "candidates_truncated")["type"]; got != "boolean" {
		t.Fatalf("environment_evidence_probe.candidates_truncated type = %#v, want boolean", got)
	}

	const wantDescription = "Read-time-resolved runtime context (#5746). Workloads, services, deployments, and catalog refs are current repository mappings. Environment corroboration additionally confirms already-visible finding environment names against current accepted correlations for the finding's exact subject digest, mirroring the reducer's strong digest match across builder/deployer repository seams; it is artifact deployment context, not repository ownership. Populated on findings list and impact explain responses; the transformed investigation packet omits it. truth_basis is always read_time_resolved. The workload_id/service_id/environment filters use current active repository mappings (#5747); stale baked values cannot satisfy them."
	if got := runtimeContext["description"]; got != wantDescription {
		t.Fatalf("runtime_context.description = %#v, want %#v", got, wantDescription)
	}
}

func TestOpenAPISpecDistinguishesDigestBoundKubernetesRefsFromRuntimeContext(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := mustMapField(t, path, "get")
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	findings := mustMapField(t, properties, "findings")
	items := mustMapField(t, findings, "items")
	itemProperties := mustMapField(t, items, "properties")

	refs := mustMapField(t, itemProperties, "kubernetes_runtime_workload_refs")
	description, _ := refs["description"].(string)
	for _, want := range []string{"exact digest", "independently current and authorized", "runtime_context.workload_ids"} {
		if !strings.Contains(description, want) {
			t.Fatalf("kubernetes runtime refs description missing %q: %q", want, description)
		}
	}
	refItems := mustMapField(t, refs, "items")
	refProperties := mustMapField(t, refItems, "properties")
	for _, want := range []string{"workload_uid", "cluster_id", "namespace", "name"} {
		if _, ok := refProperties[want]; !ok {
			t.Fatalf("kubernetes runtime workload ref missing %q", want)
		}
	}
	if _, duplicatesParentIdentity := refProperties["subject_digest"]; duplicatesParentIdentity {
		t.Fatal("nested kubernetes runtime workload ref must not repeat parent subject_digest")
	}
	probe := mustMapField(t, itemProperties, "kubernetes_runtime_probe")
	probeDescription, _ := probe["description"].(string)
	for _, want := range []string{"per-digest", "serialized-page cap", "repeated findings", "scoped callers", "authorized current refs", "raw graph query"} {
		if !strings.Contains(probeDescription, want) {
			t.Fatalf("kubernetes runtime probe description missing %q: %q", want, probeDescription)
		}
	}
	probeProperties := mustMapField(t, probe, "properties")
	if _, ok := probeProperties["candidate_limit"]; !ok {
		t.Fatal("kubernetes runtime probe missing candidate_limit")
	}
	truncated := mustMapField(t, probeProperties, "workload_refs_truncated")
	if got, want := truncated["type"], "boolean"; got != want || truncated["nullable"] != true {
		t.Fatalf("workload_refs_truncated schema = %#v, want nullable boolean", truncated)
	}
	corroboration := mustMapField(t, itemProperties, "version_resolution_corroboration")
	corroborationItems := mustMapField(t, corroboration, "items")
	corroborationProperties := mustMapField(t, corroborationItems, "properties")
	evidenceKind := mustMapField(t, corroborationProperties, "evidence_kind")
	if got := mustStringSliceField(t, evidenceKind, "enum"); containsOpenAPIEnumString(got, "kubernetes_runtime_probe") {
		t.Fatalf("version-resolution corroboration evidence kinds = %#v, must omit winner-only kubernetes runtime source", got)
	}

	explainPath := mustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	explainGet := mustMapField(t, explainPath, "get")
	explainResponses := mustMapField(t, explainGet, "responses")
	explainOK := mustMapField(t, explainResponses, "200")
	explainContent := mustMapField(t, explainOK, "content")
	explainJSON := mustMapField(t, explainContent, "application/json")
	explainSchema := mustMapField(t, explainJSON, "schema")
	explainProperties := mustMapField(t, explainSchema, "properties")
	explainFinding := mustMapField(t, explainProperties, "finding")
	explainFindingProperties := mustMapField(t, explainFinding, "properties")
	explainProbe := mustMapField(t, explainFindingProperties, "kubernetes_runtime_probe")
	if got := mustStringSliceField(t, explainProbe, "required"); !containsOpenAPIEnumString(got, "candidate_limit") || !containsOpenAPIEnumString(got, "workload_refs_truncated") {
		t.Fatalf("explain kubernetes runtime probe required fields = %#v", got)
	}
}
