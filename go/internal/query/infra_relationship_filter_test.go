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
		{name: "alias is case-insensitive", input: "WHAT_DEPLOYS", wantTypes: []string{"DEPLOYS_FROM", "DEPLOYMENT_SOURCE", "HAS_DEPLOYMENT_EVIDENCE"}, wantOK: true},
		{name: "canonical edge type", input: "DEPLOYS_FROM", wantTypes: []string{"DEPLOYS_FROM"}, wantOK: true},
		{name: "canonical edge type lower", input: "uses_module", wantTypes: []string{"USES_MODULE"}, wantOK: true},
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
