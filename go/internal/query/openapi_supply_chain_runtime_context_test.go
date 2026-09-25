// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecDocumentsSupplyChainRuntimeContextRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := testutil.MustMapField(t, path, "get")
	responses := testutil.MustMapField(t, get, "responses")
	twoHundred := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, twoHundred, "content")
	appJSON := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, appJSON, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	findings := testutil.MustMapField(t, properties, "findings")
	items := testutil.MustMapField(t, findings, "items")
	itemProperties := testutil.MustMapField(t, items, "properties")
	runtimeContext := testutil.MustMapField(t, itemProperties, "runtime_context")
	runtimeContextProperties := testutil.MustMapField(t, runtimeContext, "properties")
	environmentEvidence := testutil.MustMapField(t, runtimeContextProperties, "environment_evidence")
	additionalProperties := testutil.MustMapField(t, environmentEvidence, "additionalProperties")
	if got, want := additionalProperties["enum"], []any{"deploy_event", "declared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime_context.environment_evidence enum = %#v, want %#v", got, want)
	}
	probe := testutil.MustMapField(t, runtimeContextProperties, "environment_evidence_probe")
	probeProperties := testutil.MustMapField(t, probe, "properties")
	if got := testutil.MustMapField(t, probeProperties, "candidate_limit")["maximum"]; got != float64(200) {
		t.Fatalf("environment_evidence_probe.candidate_limit maximum = %#v, want 200", got)
	}
	if got := testutil.MustMapField(t, probeProperties, "candidates_truncated")["type"]; got != "boolean" {
		t.Fatalf("environment_evidence_probe.candidates_truncated type = %#v, want boolean", got)
	}

	const wantDescription = "Read-time-resolved runtime context (#5746). Workloads, services, deployments, and catalog refs are current repository mappings. Environment corroboration additionally confirms already-visible finding environment names against current accepted correlations for the finding's exact subject digest, mirroring the reducer's strong digest match across builder/deployer repository seams; it is artifact deployment context, not repository ownership. Populated on findings list and impact explain responses; the transformed investigation packet omits it. truth_basis is always read_time_resolved. The workload_id/service_id/environment filters use current active repository mappings (#5747); stale baked values cannot satisfy them."
	if got := runtimeContext["description"]; got != wantDescription {
		t.Fatalf("runtime_context.description = %#v, want %#v", got, wantDescription)
	}

	explainPath := testutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	explainGet := testutil.MustMapField(t, explainPath, "get")
	explainResponses := testutil.MustMapField(t, explainGet, "responses")
	explainOK := testutil.MustMapField(t, explainResponses, "200")
	explainContent := testutil.MustMapField(t, explainOK, "content")
	explainJSON := testutil.MustMapField(t, explainContent, "application/json")
	explainSchema := testutil.MustMapField(t, explainJSON, "schema")
	explainProperties := testutil.MustMapField(t, explainSchema, "properties")
	explainFinding := testutil.MustMapField(t, explainProperties, "finding")
	explainFindingProperties := testutil.MustMapField(t, explainFinding, "properties")
	explainRuntimeContext := testutil.MustMapField(t, explainFindingProperties, "runtime_context")
	if !reflect.DeepEqual(explainRuntimeContext, runtimeContext) {
		t.Fatalf("explain runtime_context = %#v, want list runtime_context %#v", explainRuntimeContext, runtimeContext)
	}
}

func TestOpenAPISpecDistinguishesDigestBoundKubernetesRefsFromRuntimeContext(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := testutil.MustMapField(t, path, "get")
	responses := testutil.MustMapField(t, get, "responses")
	twoHundred := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, twoHundred, "content")
	appJSON := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, appJSON, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	findings := testutil.MustMapField(t, properties, "findings")
	items := testutil.MustMapField(t, findings, "items")
	itemProperties := testutil.MustMapField(t, items, "properties")

	refs := testutil.MustMapField(t, itemProperties, "kubernetes_runtime_workload_refs")
	description, _ := refs["description"].(string)
	for _, want := range []string{"exact digest", "independently current and authorized", "runtime_context.workload_ids"} {
		if !strings.Contains(description, want) {
			t.Fatalf("kubernetes runtime refs description missing %q: %q", want, description)
		}
	}
	refItems := testutil.MustMapField(t, refs, "items")
	refProperties := testutil.MustMapField(t, refItems, "properties")
	for _, want := range []string{"workload_uid", "cluster_id", "namespace", "name"} {
		if _, ok := refProperties[want]; !ok {
			t.Fatalf("kubernetes runtime workload ref missing %q", want)
		}
	}
	if _, duplicatesParentIdentity := refProperties["subject_digest"]; duplicatesParentIdentity {
		t.Fatal("nested kubernetes runtime workload ref must not repeat parent subject_digest")
	}
	probe := testutil.MustMapField(t, itemProperties, "kubernetes_runtime_probe")
	probeDescription, _ := probe["description"].(string)
	for _, want := range []string{"per-digest", "serialized-page cap", "repeated findings", "scoped callers", "authorized current refs", "raw graph query"} {
		if !strings.Contains(probeDescription, want) {
			t.Fatalf("kubernetes runtime probe description missing %q: %q", want, probeDescription)
		}
	}
	probeProperties := testutil.MustMapField(t, probe, "properties")
	if _, ok := probeProperties["candidate_limit"]; !ok {
		t.Fatal("kubernetes runtime probe missing candidate_limit")
	}
	truncated := testutil.MustMapField(t, probeProperties, "workload_refs_truncated")
	if got, want := truncated["type"], "boolean"; got != want || truncated["nullable"] != true {
		t.Fatalf("workload_refs_truncated schema = %#v, want nullable boolean", truncated)
	}
	corroboration := testutil.MustMapField(t, itemProperties, "version_resolution_corroboration")
	corroborationItems := testutil.MustMapField(t, corroboration, "items")
	corroborationProperties := testutil.MustMapField(t, corroborationItems, "properties")
	evidenceKind := testutil.MustMapField(t, corroborationProperties, "evidence_kind")
	if got := mustStringSliceField(t, evidenceKind, "enum"); containsOpenAPIEnumString(got, "kubernetes_runtime_probe") {
		t.Fatalf("version-resolution corroboration evidence kinds = %#v, must omit winner-only kubernetes runtime source", got)
	}

	explainPath := testutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	explainGet := testutil.MustMapField(t, explainPath, "get")
	explainResponses := testutil.MustMapField(t, explainGet, "responses")
	explainOK := testutil.MustMapField(t, explainResponses, "200")
	explainContent := testutil.MustMapField(t, explainOK, "content")
	explainJSON := testutil.MustMapField(t, explainContent, "application/json")
	explainSchema := testutil.MustMapField(t, explainJSON, "schema")
	explainProperties := testutil.MustMapField(t, explainSchema, "properties")
	explainFinding := testutil.MustMapField(t, explainProperties, "finding")
	explainFindingProperties := testutil.MustMapField(t, explainFinding, "properties")
	explainProbe := testutil.MustMapField(t, explainFindingProperties, "kubernetes_runtime_probe")
	if got := mustStringSliceField(t, explainProbe, "required"); !containsOpenAPIEnumString(got, "candidate_limit") || !containsOpenAPIEnumString(got, "workload_refs_truncated") {
		t.Fatalf("explain kubernetes runtime probe required fields = %#v", got)
	}
}
