// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runFilterCase exercises getRelationships with a JSON body and returns the
// recorder plus the Cypher the handler passed to the graph, so a test can prove
// whether relationship_type narrowed the relationship query (#3492).
func runFilterCase(t *testing.T, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()

	var captured string
	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				captured = cypher
				return map[string]any{
					"id":       "workload:eshu",
					"name":     "eshu",
					"labels":   []any{"Workload"},
					"outgoing": []any{},
					"incoming": []any{},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.getRelationships(rec, req)
	return rec, captured
}

// TestInfraRelationshipsHonorsRelationshipTypeFilter is the #3492 regression
// guard: two distinct relationship_type values must bound the relationship query
// to two distinct edge types, and an absent filter must stay whole-relationship
// (backward compatible).
func TestInfraRelationshipsHonorsRelationshipTypeFilter(t *testing.T) {
	t.Parallel()

	deployRec, deployCypher := runFilterCase(t, `{"entity_id":"workload:eshu","relationship_type":"what_deploys"}`)
	if deployRec.Code != http.StatusOK {
		t.Fatalf("what_deploys status = %d, want %d; body=%s", deployRec.Code, http.StatusOK, deployRec.Body.String())
	}

	provisionRec, provisionCypher := runFilterCase(t, `{"entity_id":"workload:eshu","relationship_type":"what_provisions"}`)
	if provisionRec.Code != http.StatusOK {
		t.Fatalf("what_provisions status = %d, want %d; body=%s", provisionRec.Code, http.StatusOK, provisionRec.Body.String())
	}

	allRec, allCypher := runFilterCase(t, `{"entity_id":"workload:eshu"}`)
	if allRec.Code != http.StatusOK {
		t.Fatalf("unfiltered status = %d, want %d; body=%s", allRec.Code, http.StatusOK, allRec.Body.String())
	}

	if !strings.Contains(deployCypher, ":DEPLOYS_FROM") {
		t.Fatalf("what_deploys cypher = %q, want a :DEPLOYS_FROM relationship-type filter", deployCypher)
	}
	if !strings.Contains(provisionCypher, ":PROVISIONS_DEPENDENCY_FOR") {
		t.Fatalf("what_provisions cypher = %q, want a :PROVISIONS_DEPENDENCY_FOR relationship-type filter", provisionCypher)
	}
	if deployCypher == provisionCypher {
		t.Fatal("what_deploys and what_provisions produced identical Cypher; the argument is being ignored")
	}
	if strings.Contains(allCypher, ":DEPLOYS_FROM") || strings.Contains(allCypher, ":PROVISIONS_DEPENDENCY_FOR") {
		t.Fatalf("unfiltered cypher = %q, want no relationship-type filter (whole-relationship, backward compatible)", allCypher)
	}
	// The unfiltered query keeps the bare -[r]-> / -[r2]-> pattern.
	if !strings.Contains(allCypher, "(n)-[r]->(target)") || !strings.Contains(allCypher, "(source)-[r2]->(n)") {
		t.Fatalf("unfiltered cypher = %q, want the bare untyped relationship pattern", allCypher)
	}
}

// TestInfraRelationshipsRejectsUnknownRelationshipType proves an unrecognized
// filter is a 400, not a silent whole-graph fallback that would re-introduce
// the ignored-argument bug.
func TestInfraRelationshipsRejectsUnknownRelationshipType(t *testing.T) {
	t.Parallel()

	rec, _ := runFilterCase(t, `{"entity_id":"workload:eshu","relationship_type":"not_a_real_filter"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown relationship_type status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestInfraRelationshipsAcceptsCanonicalEdgeType proves a raw canonical edge
// type name is honored directly, since the HTTP route and dispatch both forward
// relationship_type.
func TestInfraRelationshipsAcceptsCanonicalEdgeType(t *testing.T) {
	t.Parallel()

	rec, cypher := runFilterCase(t, `{"entity_id":"workload:eshu","relationship_type":"USES_MODULE"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("USES_MODULE status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(cypher, ":USES_MODULE") {
		t.Fatalf("USES_MODULE cypher = %q, want a :USES_MODULE relationship-type filter", cypher)
	}
}

// TestInfraRelationshipsAcceptsMixedCaseCanonicalEdgeType proves the new
// AWS_lambda_function_uses_image edge (#5450) is queryable by its own
// relationship type. Its stored spelling is mixed-case, unlike every other
// canonical edge; resolveInfraRelationshipTypes upper-cases the input for a
// case-insensitive match but must render the exact stored case into the Cypher
// filter, or the read would bound to a type the graph does not have and return
// nothing. Both the exact spelling and an upper-cased request must resolve to
// the same stored-case filter (#5450 P2 review).
func TestInfraRelationshipsAcceptsMixedCaseCanonicalEdgeType(t *testing.T) {
	t.Parallel()

	for _, requested := range []string{
		"AWS_lambda_function_uses_image",
		"AWS_LAMBDA_FUNCTION_USES_IMAGE",
	} {
		rec, cypher := runFilterCase(t, `{"entity_id":"cloudresource:lambda","relationship_type":"`+requested+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d; body=%s", requested, rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(cypher, ":AWS_lambda_function_uses_image") {
			t.Fatalf("%s cypher = %q, want a stored-case :AWS_lambda_function_uses_image filter", requested, cypher)
		}
	}
}

// TestInfraRelationshipsScopedFilterCoexistsWithGrantPredicate proves the
// inline relationship-type filter and the scoped-token grant WHERE both render
// into one valid query: the typed pattern carries the edge filter and the
// neighbor WHERE still carries the grant predicate (no clause collision).
func TestInfraRelationshipsScopedFilterCoexistsWithGrantPredicate(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{single: map[string]any{
		"id": "tf:aws_s3_bucket.api", "name": "api", "labels": []any{"TerraformResource"},
		"outgoing": []any{}, "incoming": []any{},
	}}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"tf:aws_s3_bucket.api","relationship_type":"what_provisions"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	cypher := graph.lastSingle.Cypher
	if !strings.Contains(cypher, ":PROVISIONS_DEPENDENCY_FOR") {
		t.Fatalf("scoped+filtered Cypher missing relationship-type filter:\n%s", cypher)
	}
	if !strings.Contains(cypher, "target.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped+filtered Cypher missing neighbor grant predicate:\n%s", cypher)
	}
}

// TestWhatDeploysSurfacesRuntimeDeploymentSourceEdge is the #3507 follow-up
// regression guard: narrowing what_deploys to only DEPLOYS_FROM dropped the
// runtime deployment topology the pre-#3492 untyped read returned, such as the
// WorkloadInstance-[:DEPLOYMENT_SOURCE]->Repository edge that
// fetchDeploymentSourcesFromGraph reads. what_deploys must keep surfacing it.
//
// The fake reader returns the DEPLOYMENT_SOURCE edge only when the Cypher's
// relationship-type filter admits DEPLOYMENT_SOURCE, so the test proves the edge
// reaches the caller through the actual filter, not just that a token string is
// present. Before the fix what_deploys filtered to ":DEPLOYS_FROM" alone, the
// edge was dropped, and the response carried an empty outgoing slice.
func TestWhatDeploysSurfacesRuntimeDeploymentSourceEdge(t *testing.T) {
	t.Parallel()

	deploymentSourceEdge := map[string]any{
		"direction":     "outgoing",
		"type":          "DEPLOYMENT_SOURCE",
		"target_name":   "deploy-repo",
		"target_id":     "repo:deploy",
		"target_labels": []any{"Repository"},
	}

	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				outgoing := []any{}
				// Mirror the graph: the DEPLOYMENT_SOURCE edge only matches when
				// the relationship-type filter admits it (or the read is untyped).
				if !strings.Contains(cypher, ":") || strings.Contains(cypher, "DEPLOYMENT_SOURCE") {
					outgoing = []any{deploymentSourceEdge}
				}
				return map[string]any{
					"id":       "instance:eshu",
					"name":     "eshu",
					"labels":   []any{"WorkloadInstance"},
					"outgoing": outgoing,
					"incoming": []any{},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(`{"entity_id":"instance:eshu","relationship_type":"what_deploys"}`))
	rec := httptest.NewRecorder()
	handler.getRelationships(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeInfraData(t, rec.Body.Bytes())
	outgoing, ok := payload["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("what_deploys dropped the runtime DEPLOYMENT_SOURCE edge: outgoing = %#v", payload["outgoing"])
	}
	edge, _ := outgoing[0].(map[string]any)
	if got := StringVal(edge, "type"); got != "DEPLOYMENT_SOURCE" {
		t.Fatalf("outgoing[0].type = %q, want DEPLOYMENT_SOURCE", got)
	}
}

// TestInfraRelationshipsWhatRunsImageSurfacesOutgoingEdge is the #5436
// regression guard for the KubernetesWorkload-[:RUNS_IMAGE]->OciImageManifest
// edge, which storage/cypher/kubernetes_correlation_edge_writer.go writes but
// which had no declared query/MCP read path before this change. Anchored on a
// KubernetesWorkload, what_runs_image must bound the Cypher to :RUNS_IMAGE and
// the response must surface the resolved image as an outgoing relationship.
//
// The fake reader only returns the RUNS_IMAGE edge when the Cypher's
// relationship-type filter admits RUNS_IMAGE, so this proves the edge reaches
// the caller through the actual filter rather than a hardcoded fixture. Before
// the Stage-1 allowlist entry, "what_runs_image" and "RUNS_IMAGE" were both
// unrecognized by resolveInfraRelationshipTypes and this request was rejected
// with 400 (see TestResolveInfraRelationshipTypes's "what_runs_image alias"
// and "canonical RUNS_IMAGE edge type" cases, which fail without that entry).
func TestInfraRelationshipsWhatRunsImageSurfacesOutgoingEdge(t *testing.T) {
	t.Parallel()

	runsImageEdge := map[string]any{
		"direction":     "outgoing",
		"type":          "RUNS_IMAGE",
		"target_name":   "supply-chain-demo",
		"target_id":     "sha256:demo-manifest-digest",
		"target_labels": []any{"OciImageManifest"},
	}

	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				outgoing := []any{}
				if strings.Contains(cypher, ":RUNS_IMAGE") {
					outgoing = []any{runsImageEdge}
				}
				return map[string]any{
					"id":       "k8sworkload:supply-chain-demo",
					"name":     "supply-chain-demo",
					"labels":   []any{"KubernetesWorkload"},
					"outgoing": outgoing,
					"incoming": []any{},
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(`{"entity_id":"k8sworkload:supply-chain-demo","relationship_type":"what_runs_image"}`))
	rec := httptest.NewRecorder()
	handler.getRelationships(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeInfraData(t, rec.Body.Bytes())
	outgoing, ok := payload["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("what_runs_image dropped the RUNS_IMAGE edge: outgoing = %#v", payload["outgoing"])
	}
	edge, _ := outgoing[0].(map[string]any)
	if got := StringVal(edge, "type"); got != "RUNS_IMAGE" {
		t.Fatalf("outgoing[0].type = %q, want RUNS_IMAGE", got)
	}
	if got := StringVal(edge, "target_id"); got != "sha256:demo-manifest-digest" {
		t.Fatalf("outgoing[0].target_id = %q, want the resolved image id", got)
	}
}

// TestInfraRelationshipsWhatRunsImageSurfacesIncomingEdge proves the same
// RUNS_IMAGE edge is readable anchored the other direction: from an
// OciImageManifest, what_runs_image must surface the KubernetesWorkload(s)
// running it as incoming relationships (the reverse of the outgoing case
// above), matching the bidirectional pattern getRelationships already uses for
// every other alias.
func TestInfraRelationshipsWhatRunsImageSurfacesIncomingEdge(t *testing.T) {
	t.Parallel()

	runsImageEdge := map[string]any{
		"direction":     "incoming",
		"type":          "RUNS_IMAGE",
		"source_name":   "supply-chain-demo",
		"source_id":     "k8sworkload:supply-chain-demo",
		"source_labels": []any{"KubernetesWorkload"},
	}

	handler := &InfraHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				incoming := []any{}
				if strings.Contains(cypher, ":RUNS_IMAGE") {
					incoming = []any{runsImageEdge}
				}
				return map[string]any{
					"id":       "sha256:demo-manifest-digest",
					"name":     "supply-chain-demo",
					"labels":   []any{"OciImageManifest"},
					"outgoing": []any{},
					"incoming": incoming,
				}, nil
			},
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", bytes.NewBufferString(`{"entity_id":"sha256:demo-manifest-digest","relationship_type":"RUNS_IMAGE"}`))
	rec := httptest.NewRecorder()
	handler.getRelationships(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeInfraData(t, rec.Body.Bytes())
	incoming, ok := payload["incoming"].([]any)
	if !ok || len(incoming) != 1 {
		t.Fatalf("RUNS_IMAGE (canonical) dropped the incoming workload edge: incoming = %#v", payload["incoming"])
	}
	edge, _ := incoming[0].(map[string]any)
	if got := StringVal(edge, "type"); got != "RUNS_IMAGE" {
		t.Fatalf("incoming[0].type = %q, want RUNS_IMAGE", got)
	}
	if got := StringVal(edge, "source_id"); got != "k8sworkload:supply-chain-demo" {
		t.Fatalf("incoming[0].source_id = %q, want the workload id", got)
	}
}

// TestResolveInfraRelationshipTypes covers every dispatch variant of the
// alias/canonical resolver, including the empty (backward-compatible) and
// rejected cases.
func TestResolveInfraRelationshipTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		input     string
		wantTypes []string
		wantOK    bool
	}{
		{name: "empty is unfiltered", input: "", wantTypes: nil, wantOK: true},
		{name: "whitespace is unfiltered", input: "   ", wantTypes: nil, wantOK: true},
		{name: "what_deploys alias", input: "what_deploys", wantTypes: []string{"DEPLOYS_FROM", "DEPLOYMENT_SOURCE", "HAS_DEPLOYMENT_EVIDENCE"}, wantOK: true},
		{name: "what_provisions alias", input: "what_provisions", wantTypes: []string{"PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM"}, wantOK: true},
		{name: "module_consumers alias", input: "module_consumers", wantTypes: []string{"USES_MODULE"}, wantOK: true},
		{name: "who_consumes_xrd alias", input: "who_consumes_xrd", wantTypes: []string{"USES_MODULE"}, wantOK: true},
		{name: "what_runs_image alias", input: "what_runs_image", wantTypes: []string{"RUNS_IMAGE"}, wantOK: true},
		{name: "alias is case-insensitive", input: "WHAT_DEPLOYS", wantTypes: []string{"DEPLOYS_FROM", "DEPLOYMENT_SOURCE", "HAS_DEPLOYMENT_EVIDENCE"}, wantOK: true},
		{name: "canonical edge type", input: "DEPLOYS_FROM", wantTypes: []string{"DEPLOYS_FROM"}, wantOK: true},
		{name: "canonical edge type lower", input: "uses_module", wantTypes: []string{"USES_MODULE"}, wantOK: true},
		{name: "canonical RUNS_IMAGE edge type", input: "RUNS_IMAGE", wantTypes: []string{"RUNS_IMAGE"}, wantOK: true},
		{name: "unknown is rejected", input: "not_a_real_filter", wantTypes: nil, wantOK: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := resolveInfraRelationshipTypes(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("resolveInfraRelationshipTypes(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			if !equalStringSlices(got, tc.wantTypes) {
				t.Fatalf("resolveInfraRelationshipTypes(%q) = %#v, want %#v", tc.input, got, tc.wantTypes)
			}
		})
	}
}
