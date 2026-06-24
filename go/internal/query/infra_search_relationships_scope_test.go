// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingInfraScopeGraph records the Cypher + params sent to Run / RunSingle
// so scope tests can assert the repository-anchored predicate placement and
// grant binding for the search and relationship handlers.
type recordingInfraScopeGraph struct {
	runRows    []map[string]any
	single     map[string]any
	runCalls   []recordedInfraCall
	singleN    int
	lastSingle recordedInfraCall
}

type recordedInfraCall struct {
	Cypher string
	Params map[string]any
}

func (g *recordingInfraScopeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.runCalls = append(g.runCalls, recordedInfraCall{Cypher: cypher, Params: params})
	return g.runRows, nil
}

func (g *recordingInfraScopeGraph) RunSingle(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	g.singleN++
	g.lastSingle = recordedInfraCall{Cypher: cypher, Params: params}
	return g.single, nil
}

// ── Route gate ──

// TestAuthMiddlewareWithScopedTokensAllowsInfraSearchAndRelationships proves the
// search and relationship routes now pass the scoped-token route gate so a
// scoped caller reaches the tenant-filtered handler.
func TestAuthMiddlewareWithScopedTokensAllowsInfraSearchAndRelationships(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, target := range []string{
		"/api/v0/infra/resources/search",
		"/api/v0/infra/relationships",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, target, strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusNoContent; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

// ── Predicate placement / no-regression ──

// TestInfraSearchScopePredicateRendersOnlyWhenScoped proves the
// repository-anchored predicate is bound for scoped access and that the
// unscoped search Cypher is byte-identical to the pre-scoped query.
func TestInfraSearchScopePredicateRendersOnlyWhenScoped(t *testing.T) {
	t.Parallel()

	unscoped := infraSearchScopeClause(repositoryAccessFilter{allScopes: true})
	if unscoped != "" {
		t.Fatalf("unscoped search clause must be empty, got %q", unscoped)
	}

	scoped := infraSearchScopeClause(repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-team-a"},
		allowed:              map[string]struct{}{"repo-team-a": {}},
	})
	for _, want := range []string{
		"AND ",
		"n.repo_id IN $allowed_repository_ids",
		"EXISTS {",
		"(scopeRepo:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n)",
	} {
		if !strings.Contains(scoped, want) {
			t.Fatalf("scoped search clause missing %q:\n%s", want, scoped)
		}
	}
}

// TestInfraSearchScopedEmptyGrantReturnsEmptyWithoutGraphRead proves an
// authenticated scoped token with no granted repositories gets a bounded empty
// result without the graph ever being read.
func TestInfraSearchScopedEmptyGrantReturnsEmptyWithoutGraphRead(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", strings.NewReader(`{"query":"api"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.runCalls) != 0 {
		t.Fatalf("graph was read for an empty scoped grant: %d calls", len(graph.runCalls))
	}
	payload := decodeInfraData(t, rec.Body.Bytes())
	if got := payload["count"]; got != float64(0) {
		t.Fatalf("count = %v, want 0", got)
	}
	if results, ok := payload["results"].([]any); !ok || len(results) != 0 {
		t.Fatalf("results = %v, want empty", payload["results"])
	}
}

// TestInfraSearchScopedGrantBindsPredicate proves the search handler binds the
// repository-anchored predicate and grant parameters for a scoped token.
func TestInfraSearchScopedGrantBindsPredicate(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", strings.NewReader(`{"query":"api"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
		AllowedScopeIDs:      []string{"git-repository-scope:team-a"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.runCalls) != 1 {
		t.Fatalf("graph Run calls = %d, want 1", len(graph.runCalls))
	}
	call := graph.runCalls[0]
	if !strings.Contains(call.Cypher, "n.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped search Cypher missing grant predicate:\n%s", call.Cypher)
	}
	if _, ok := call.Params["allowed_repository_ids"]; !ok {
		t.Fatalf("scoped search params missing allowed_repository_ids: %v", call.Params)
	}
	if _, ok := call.Params["allowed_scope_ids"]; !ok {
		t.Fatalf("scoped search params missing allowed_scope_ids: %v", call.Params)
	}
}

// TestInfraSearchUnscopedCypherUnchanged proves a shared/local caller's search
// Cypher carries no grant predicate and no repository traversal (no-regression).
func TestInfraSearchUnscopedCypherUnchanged(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", strings.NewReader(`{"query":"api"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.runCalls) != 1 {
		t.Fatalf("graph Run calls = %d, want 1", len(graph.runCalls))
	}
	cypher := graph.runCalls[0].Cypher
	if strings.Contains(cypher, "$allowed_repository_ids") {
		t.Fatalf("unscoped search Cypher must not bind grant arrays:\n%s", cypher)
	}
	if strings.Contains(cypher, "scopeRepo") {
		t.Fatalf("unscoped search Cypher must not traverse repositories:\n%s", cypher)
	}
}

// ── Relationships ──

// TestInfraRelationshipsScopePredicateRendersOnlyWhenScoped proves the anchor
// and neighbor predicates render for scoped access only.
func TestInfraRelationshipsScopePredicateRendersOnlyWhenScoped(t *testing.T) {
	t.Parallel()

	unscopedAnchor := infraRelationshipAnchorClause(repositoryAccessFilter{allScopes: true})
	if unscopedAnchor != "" {
		t.Fatalf("unscoped anchor clause must be empty, got %q", unscopedAnchor)
	}
	unscopedNeighbor := infraRelationshipNeighborClause(repositoryAccessFilter{allScopes: true}, "target")
	if unscopedNeighbor != "" {
		t.Fatalf("unscoped neighbor clause must be empty, got %q", unscopedNeighbor)
	}

	scoped := repositoryAccessFilter{
		allowedRepositoryIDs: []string{"repo-team-a"},
		allowed:              map[string]struct{}{"repo-team-a": {}},
	}
	anchor := infraRelationshipAnchorClause(scoped)
	if !strings.Contains(anchor, "n.repo_id IN $allowed_repository_ids") ||
		!strings.Contains(anchor, "-[:USES]->(n)") {
		t.Fatalf("scoped anchor clause missing predicate:\n%s", anchor)
	}
	neighbor := infraRelationshipNeighborClause(scoped, "target")
	if !strings.Contains(neighbor, "WHERE ") ||
		!strings.Contains(neighbor, "target.repo_id IN $allowed_repository_ids") ||
		!strings.Contains(neighbor, "-[:USES]->(target)") {
		t.Fatalf("scoped neighbor clause missing predicate:\n%s", neighbor)
	}
}

// TestInfraRelationshipsScopedEmptyGrantReturnsNotFoundWithoutGraphRead proves
// an empty-grant scoped token fails closed (not found) without a graph read.
func TestInfraRelationshipsScopedEmptyGrantReturnsNotFoundWithoutGraphRead(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"tf:aws_s3_bucket.api"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if graph.singleN != 0 {
		t.Fatalf("graph was read for an empty scoped grant: %d RunSingle calls", graph.singleN)
	}
}

// TestInfraRelationshipsScopedOutOfGrantReturnsNotFound proves an in-grant token
// whose seed node resolves to no granted repository gets not_found (no existence
// disclosure), and that the anchor predicate plus grant params were bound.
func TestInfraRelationshipsScopedOutOfGrantReturnsNotFound(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{single: nil}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"tf:aws_s3_bucket.other"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if graph.singleN != 1 {
		t.Fatalf("graph RunSingle calls = %d, want 1", graph.singleN)
	}
	if !strings.Contains(graph.lastSingle.Cypher, "n.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped relationships Cypher missing anchor predicate:\n%s", graph.lastSingle.Cypher)
	}
	if !strings.Contains(graph.lastSingle.Cypher, "target.repo_id IN $allowed_repository_ids") {
		t.Fatalf("scoped relationships Cypher missing neighbor predicate:\n%s", graph.lastSingle.Cypher)
	}
	if _, ok := graph.lastSingle.Params["allowed_repository_ids"]; !ok {
		t.Fatalf("scoped relationships params missing allowed_repository_ids: %v", graph.lastSingle.Params)
	}
	if _, ok := graph.lastSingle.Params["allowed_scope_ids"]; !ok {
		t.Fatalf("scoped relationships params missing allowed_scope_ids: %v", graph.lastSingle.Params)
	}
}

// TestInfraRelationshipsScopedInGrantVisible proves an in-grant seed node
// returns its bounded relationships.
func TestInfraRelationshipsScopedInGrantVisible(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{single: map[string]any{
		"id":     "tf:aws_s3_bucket.api",
		"name":   "api",
		"labels": []any{"TerraformResource"},
		"outgoing": []any{map[string]any{
			"direction":     "outgoing",
			"type":          "USES_KMS_KEY",
			"target_name":   "api-key",
			"target_id":     "tf:aws_kms_key.api",
			"target_labels": []any{"TerraformResource"},
		}},
		"incoming": []any{},
	}}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"tf:aws_s3_bucket.api"}`))
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
	payload := decodeInfraData(t, rec.Body.Bytes())
	outgoing, ok := payload["outgoing"].([]any)
	if !ok || len(outgoing) != 1 {
		t.Fatalf("outgoing = %v, want one edge", payload["outgoing"])
	}
}

// TestInfraRelationshipsUnscopedCypherUnchanged proves a shared/local caller's
// relationship Cypher carries no grant predicate (no-regression). The existing
// capability test pins the exact unscoped anchor, this pins the absence of
// scope artifacts end to end through the handler.
func TestInfraRelationshipsUnscopedCypherUnchanged(t *testing.T) {
	t.Parallel()

	graph := &recordingInfraScopeGraph{single: map[string]any{
		"id": "workload:eshu", "name": "eshu", "labels": []any{"Workload"},
		"outgoing": []any{}, "incoming": []any{},
	}}
	handler := &InfraHandler{Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/relationships", strings.NewReader(`{"entity_id":"workload:eshu"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	cypher := graph.lastSingle.Cypher
	if strings.Contains(cypher, "$allowed_repository_ids") {
		t.Fatalf("unscoped relationships Cypher must not bind grant arrays:\n%s", cypher)
	}
	if strings.Contains(cypher, "scopeRepo") {
		t.Fatalf("unscoped relationships Cypher must not traverse repositories:\n%s", cypher)
	}
	if !strings.Contains(cypher, "MATCH (n) WHERE n.id = $entity_id") {
		t.Fatalf("unscoped relationships Cypher must keep the pinned anchor:\n%s", cypher)
	}
}

func decodeInfraData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	data := map[string]any{}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, body)
	}
	return data
}
