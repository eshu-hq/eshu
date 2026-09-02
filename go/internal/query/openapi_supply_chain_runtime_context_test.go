// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecDocumentsSupplyChainRuntimeContextRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := querytestutil.MustMapField(t, path, "get")
	responses := querytestutil.MustMapField(t, get, "responses")
	twoHundred := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, twoHundred, "content")
	appJSON := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, appJSON, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	findings := querytestutil.MustMapField(t, properties, "findings")
	items := querytestutil.MustMapField(t, findings, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")
	runtimeContext := querytestutil.MustMapField(t, itemProperties, "runtime_context")
	runtimeContextProperties := querytestutil.MustMapField(t, runtimeContext, "properties")
	environmentEvidence := querytestutil.MustMapField(t, runtimeContextProperties, "environment_evidence")
	additionalProperties := querytestutil.MustMapField(t, environmentEvidence, "additionalProperties")
	if got, want := additionalProperties["enum"], []any{"deploy_event", "declared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime_context.environment_evidence enum = %#v, want %#v", got, want)
	}
	probe := querytestutil.MustMapField(t, runtimeContextProperties, "environment_evidence_probe")
	probeProperties := querytestutil.MustMapField(t, probe, "properties")
	if got := querytestutil.MustMapField(t, probeProperties, "candidate_limit")["maximum"]; got != float64(200) {
		t.Fatalf("environment_evidence_probe.candidate_limit maximum = %#v, want 200", got)
	}
	if got := querytestutil.MustMapField(t, probeProperties, "candidates_truncated")["type"]; got != "boolean" {
		t.Fatalf("environment_evidence_probe.candidates_truncated type = %#v, want boolean", got)
	}

	const wantDescription = "Read-time-resolved runtime context (#5746). Workloads, services, deployments, and catalog refs are current repository mappings. Environment corroboration additionally confirms already-visible finding environment names against current accepted correlations for the finding's exact subject digest, mirroring the reducer's strong digest match across builder/deployer repository seams; it is artifact deployment context, not repository ownership. Populated on findings list and impact explain responses; the transformed investigation packet omits it. truth_basis is always read_time_resolved. The workload_id/service_id/environment filters use current active repository mappings (#5747); stale baked values cannot satisfy them."
	if got := runtimeContext["description"]; got != wantDescription {
		t.Fatalf("runtime_context.description = %#v, want %#v", got, wantDescription)
	}

	explainPath := querytestutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	explainGet := querytestutil.MustMapField(t, explainPath, "get")
	explainResponses := querytestutil.MustMapField(t, explainGet, "responses")
	explainOK := querytestutil.MustMapField(t, explainResponses, "200")
	explainContent := querytestutil.MustMapField(t, explainOK, "content")
	explainJSON := querytestutil.MustMapField(t, explainContent, "application/json")
	explainSchema := querytestutil.MustMapField(t, explainJSON, "schema")
	explainProperties := querytestutil.MustMapField(t, explainSchema, "properties")
	explainFinding := querytestutil.MustMapField(t, explainProperties, "finding")
	explainFindingProperties := querytestutil.MustMapField(t, explainFinding, "properties")
	explainRuntimeContext := querytestutil.MustMapField(t, explainFindingProperties, "runtime_context")
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
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := querytestutil.MustMapField(t, path, "get")
	responses := querytestutil.MustMapField(t, get, "responses")
	twoHundred := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, twoHundred, "content")
	appJSON := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, appJSON, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	findings := querytestutil.MustMapField(t, properties, "findings")
	items := querytestutil.MustMapField(t, findings, "items")
	itemProperties := querytestutil.MustMapField(t, items, "properties")

	refs := querytestutil.MustMapField(t, itemProperties, "kubernetes_runtime_workload_refs")
	description, _ := refs["description"].(string)
	for _, want := range []string{"exact digest", "independently current and authorized", "runtime_context.workload_ids"} {
		if !strings.Contains(description, want) {
			t.Fatalf("kubernetes runtime refs description missing %q: %q", want, description)
		}
	}
	refItems := querytestutil.MustMapField(t, refs, "items")
	refProperties := querytestutil.MustMapField(t, refItems, "properties")
	for _, want := range []string{"workload_uid", "cluster_id", "namespace", "name"} {
		if _, ok := refProperties[want]; !ok {
			t.Fatalf("kubernetes runtime workload ref missing %q", want)
		}
	}
	if _, duplicatesParentIdentity := refProperties["subject_digest"]; duplicatesParentIdentity {
		t.Fatal("nested kubernetes runtime workload ref must not repeat parent subject_digest")
	}
	probe := querytestutil.MustMapField(t, itemProperties, "kubernetes_runtime_probe")
	probeDescription, _ := probe["description"].(string)
	for _, want := range []string{"per-digest", "serialized-page cap", "repeated findings", "scoped callers", "authorized current refs", "raw graph query"} {
		if !strings.Contains(probeDescription, want) {
			t.Fatalf("kubernetes runtime probe description missing %q: %q", want, probeDescription)
		}
	}
	probeProperties := querytestutil.MustMapField(t, probe, "properties")
	if _, ok := probeProperties["candidate_limit"]; !ok {
		t.Fatal("kubernetes runtime probe missing candidate_limit")
	}
	truncated := querytestutil.MustMapField(t, probeProperties, "workload_refs_truncated")
	if got, want := truncated["type"], "boolean"; got != want || truncated["nullable"] != true {
		t.Fatalf("workload_refs_truncated schema = %#v, want nullable boolean", truncated)
	}
	corroboration := querytestutil.MustMapField(t, itemProperties, "version_resolution_corroboration")
	corroborationItems := querytestutil.MustMapField(t, corroboration, "items")
	corroborationProperties := querytestutil.MustMapField(t, corroborationItems, "properties")
	evidenceKind := querytestutil.MustMapField(t, corroborationProperties, "evidence_kind")
	if got := mustStringSliceField(t, evidenceKind, "enum"); containsOpenAPIEnumString(got, "kubernetes_runtime_probe") {
		t.Fatalf("version-resolution corroboration evidence kinds = %#v, must omit winner-only kubernetes runtime source", got)
	}

	explainPath := querytestutil.MustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	explainGet := querytestutil.MustMapField(t, explainPath, "get")
	explainResponses := querytestutil.MustMapField(t, explainGet, "responses")
	explainOK := querytestutil.MustMapField(t, explainResponses, "200")
	explainContent := querytestutil.MustMapField(t, explainOK, "content")
	explainJSON := querytestutil.MustMapField(t, explainContent, "application/json")
	explainSchema := querytestutil.MustMapField(t, explainJSON, "schema")
	explainProperties := querytestutil.MustMapField(t, explainSchema, "properties")
	explainFinding := querytestutil.MustMapField(t, explainProperties, "finding")
	explainFindingProperties := querytestutil.MustMapField(t, explainFinding, "properties")
	explainProbe := querytestutil.MustMapField(t, explainFindingProperties, "kubernetes_runtime_probe")
	if got := mustStringSliceField(t, explainProbe, "required"); !containsOpenAPIEnumString(got, "candidate_limit") || !containsOpenAPIEnumString(got, "workload_refs_truncated") {
		t.Fatalf("explain kubernetes runtime probe required fields = %#v", got)
	}
}
