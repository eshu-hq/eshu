// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #5167 code-family batch 1: response-body two-tenant proof for the three
// graph-backed routes -- POST /api/v0/code/complexity,
// POST /api/v0/code/quality/inspect and POST /api/v0/code/call-graph/metrics.
//
// The sibling files assert the grant predicate is present in the emitted
// statement. That is necessary and not sufficient: a predicate can be present
// and still filter nothing, which is what an OPTIONAL MATCH-attached WHERE
// does. These tests seed two tenants into evaluatingRepositoryGraph, which
// applies Cypher's clause semantics to the statement the handler actually
// emits, and assert on the bytes the caller receives.

const (
	codeGrantGrantedFunction   = "GrantedComplexityProbe"
	codeGrantUngrantedFunction = "UngrantedComplexityProbe"
	codeGrantOrphanFunction    = "OrphanComplexityProbe"
)

// complexityListSeeds is the two-tenant fixture the complexity list branch
// scans: one function in the granted repository, one in another tenant's, and
// one the graph cannot attribute to any repository at all.
func complexityListSeeds() []graphGrantSeed {
	return []graphGrantSeed{
		{repoID: codeGrantGrantedRepo, row: complexityListRow("fn-granted", codeGrantGrantedFunction, codeGrantGrantedRepo, 7)},
		{repoID: codeGrantOtherRepo, row: complexityListRow("fn-other", codeGrantUngrantedFunction, codeGrantOtherRepo, 9)},
		{repoID: "", row: complexityListRow("fn-orphan", codeGrantOrphanFunction, "", 11)},
	}
}

func complexityListRow(id, name, repoID string, complexity int) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"labels":     []any{"Function"},
		"file_path":  "internal/auth/session.go",
		"repo_id":    repoID,
		"repo_name":  repoID,
		"language":   "go",
		"start_line": int64(10),
		"end_line":   int64(40),
		"complexity": int64(complexity),
	}
}

func codeQualityInspectSeeds() []graphGrantSeed {
	return []graphGrantSeed{
		{repoID: codeGrantGrantedRepo, row: codeQualityInspectRow("fn-granted", codeGrantGrantedFunction, codeGrantGrantedRepo)},
		{repoID: codeGrantOtherRepo, row: codeQualityInspectRow("fn-other", codeGrantUngrantedFunction, codeGrantOtherRepo)},
	}
}

func codeQualityInspectRow(id, name, repoID string) map[string]any {
	return map[string]any{
		"entity_id":      id,
		"name":           name,
		"labels":         []any{"Function"},
		"file_path":      "internal/auth/session.go",
		"repo_id":        repoID,
		"repo_name":      repoID,
		"language":       "go",
		"start_line":     int64(10),
		"end_line":       int64(40),
		"line_count":     int64(31),
		"argument_count": int64(6),
		"complexity":     int64(12),
	}
}

// repositoryProjectedColumns are the columns an OPTIONAL MATCH nulls when its
// pattern (including its WHERE) does not match.
func repositoryProjectedColumns() []string {
	return []string{"file_path", "repo_id", "repo_name"}
}

func runGraphGrantRoute(
	t *testing.T,
	graph *evaluatingRepositoryGraph,
	path string,
	body map[string]any,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newCodeGrantRouteRequest(t, path, body, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestComplexityListDoesNotLeakUngrantedFunctions is the #5167 P0 regression.
// With the grant on the OPTIONAL MATCH, this body carried every function in the
// corpus with only the repository columns nulled.
func TestComplexityListDoesNotLeakUngrantedFunctions(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             complexityListSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runGraphGrantRoute(t, graph, "/api/v0/code/complexity", map[string]any{}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) == 0 {
		t.Fatal("no statement reached the graph")
	}
	if repositoryBindingIsOptional(graph.statements[0]) {
		t.Fatalf("a scoped caller's Repository anchor is still optional, so the grant filters nothing:\n%s", graph.statements[0])
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedFunction) {
		t.Fatalf("granted tenant's function %q is missing: %s", codeGrantGrantedFunction, body)
	}
	for _, leaked := range []string{codeGrantUngrantedFunction, codeGrantOrphanFunction, codeGrantOtherRepo} {
		t.Run(leaked, func(t *testing.T) {
			if strings.Contains(body, leaked) {
				t.Fatalf("scoped complexity list leaked %q: %s", leaked, body)
			}
		})
	}
}

// TestComplexityListUnscopedAnswerIsUnchanged pins the other direction: the
// scoped anchor must not narrow what a shared-key caller sees.
func TestComplexityListUnscopedAnswerIsUnchanged(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             complexityListSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	rec := runGraphGrantRoute(t, graph, "/api/v0/code/complexity", map[string]any{}, nil)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{codeGrantGrantedFunction, codeGrantUngrantedFunction, codeGrantOrphanFunction} {
		if !strings.Contains(body, want) {
			t.Fatalf("unscoped complexity list lost %q: %s", want, body)
		}
	}
}

// TestComplexityListUnscopedRepoIDSelectorFiltersToThatRepository is the other
// half of the anchor decision. An unscoped shared-key, admin, or local caller
// that names a repository is asking for that repository's ranking, and the
// OPTIONAL MATCH form cannot give it one: the appended repo.id = $repo_id lands
// in a WHERE attached to the optional pattern, so every Function in the corpus
// came back with the repository columns nulled on the rows belonging to some
// other repository.
func TestComplexityListUnscopedRepoIDSelectorFiltersToThatRepository(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             complexityListSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	rec := runGraphGrantRoute(
		t,
		graph,
		"/api/v0/code/complexity",
		map[string]any{"repo_id": codeGrantGrantedRepo},
		nil,
	)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) == 0 {
		t.Fatal("no statement reached the graph")
	}
	if repositoryBindingIsOptional(graph.statements[0]) {
		t.Fatalf("a supplied repo_id sits on an optional Repository binding, so it filters nothing:\n%s", graph.statements[0])
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedFunction) {
		t.Fatalf("the named repository's function %q is missing: %s", codeGrantGrantedFunction, body)
	}
	for _, unwanted := range []string{codeGrantUngrantedFunction, codeGrantOrphanFunction, codeGrantOtherRepo} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("complexity list for repo_id %q returned %q: %s", codeGrantGrantedRepo, unwanted, body)
		}
	}
}

// TestComplexityByNameDoesNotLeakUngrantedFunctions covers the name branch,
// whose ambiguity candidate list would otherwise name the other tenant's
// function and its repository.
func TestComplexityByNameDoesNotLeakUngrantedFunctions(t *testing.T) {
	t.Parallel()

	seeds := []graphGrantSeed{
		{repoID: codeGrantGrantedRepo, row: complexityListRow("fn-granted", "RefreshSession", codeGrantGrantedRepo, 7)},
		{repoID: codeGrantOtherRepo, row: complexityListRow("fn-other", "RefreshSession", codeGrantOtherRepo, 9)},
	}
	graph := &evaluatingRepositoryGraph{seeds: seeds, repositoryColumns: repositoryProjectedColumns()}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runGraphGrantRoute(t, graph, "/api/v0/code/complexity", map[string]any{"function_name": "RefreshSession"}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, codeGrantOtherRepo) || strings.Contains(body, "fn-other") {
		t.Fatalf("complexity-by-name leaked the other tenant's candidate: %s", body)
	}
}

// TestCodeQualityInspectDoesNotLeakUngrantedFunctions is the same proof for the
// refactoring-candidate scan.
func TestCodeQualityInspectDoesNotLeakUngrantedFunctions(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             codeQualityInspectSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runGraphGrantRoute(t, graph, "/api/v0/code/quality/inspect", map[string]any{"check": "complexity"}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedFunction) {
		t.Fatalf("granted tenant's function %q is missing: %s", codeGrantGrantedFunction, body)
	}
	for _, leaked := range []string{codeGrantUngrantedFunction, codeGrantOtherRepo} {
		if strings.Contains(body, leaked) {
			t.Fatalf("scoped quality inspection leaked %q: %s", leaked, body)
		}
	}
}

func TestCodeQualityInspectUnscopedAnswerIsUnchanged(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             codeQualityInspectSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	rec := runGraphGrantRoute(t, graph, "/api/v0/code/quality/inspect", map[string]any{"check": "complexity"}, nil)

	body := rec.Body.String()
	for _, want := range []string{codeGrantGrantedFunction, codeGrantUngrantedFunction} {
		if !strings.Contains(body, want) {
			t.Fatalf("unscoped quality inspection lost %q: %s", want, body)
		}
	}
}

// TestEvaluatingRepositoryGraphKeepsOptionalMatchRows proves the fake above can
// fail. Fed the statement shape this PR replaced -- the grant on an
// OPTIONAL MATCH -- it returns the out-of-grant row with the repository columns
// nulled, which is exactly what the live NornicDB run returned before the fix.
// Without this, a fake that dropped every non-matching row would make the route
// tests pass no matter where the predicate sat.
func TestEvaluatingRepositoryGraphKeepsOptionalMatchRows(t *testing.T) {
	t.Parallel()

	graph := &evaluatingRepositoryGraph{
		seeds:             complexityListSeeds(),
		repositoryColumns: repositoryProjectedColumns(),
	}
	optional := `
		MATCH (e:Function)
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE coalesce(e.cyclomatic_complexity, 0) > 0
	 AND (repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)
		RETURN e.id as id
	`
	params := map[string]any{
		"allowed_repository_ids": []string{codeGrantGrantedRepo},
		"allowed_scope_ids":      []string{},
	}
	rows, err := graph.Run(context.Background(), optional, params)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(rows) != len(complexityListSeeds()) {
		t.Fatalf("rows = %d, want %d; an OPTIONAL MATCH-attached WHERE filters the optional pattern, not the row set", len(rows), len(complexityListSeeds()))
	}
	for _, row := range rows {
		if StringVal(row, "name") != codeGrantUngrantedFunction {
			continue
		}
		if got := row["repo_id"]; got != nil {
			t.Fatalf("out-of-grant row repo_id = %#v, want nil -- the optional pattern did not match", got)
		}
		return
	}
	t.Fatal("the out-of-grant row was dropped; the fake models a required MATCH, so it cannot prove the P0")
}

// callGraphSeedEdge is one CALLS edge, tagged with the repository each endpoint
// belongs to.
type callGraphSeedEdge struct {
	sourceRepo string
	targetRepo string
	row        map[string]any
}

// evaluatingCallGraphEdges answers the call-graph edge scan by applying the
// statement's own repository predicates -- the inline {repo_id: $repo_id}
// anchors and, for a scoped caller, the grant condition on each endpoint.
type evaluatingCallGraphEdges struct {
	edges      []callGraphSeedEdge
	statements []string
}

func (g *evaluatingCallGraphEdges) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.statements = append(g.statements, cypher)
	normalized := normalizeCypherWhitespace(cypher)
	anchor, _ := params["repo_id"].(string)
	rows := make([]map[string]any, 0, len(g.edges))
	for _, edge := range g.edges {
		if strings.Contains(normalized, "source:Function {repo_id: $repo_id}") && edge.sourceRepo != anchor {
			continue
		}
		if strings.Contains(normalized, "target:Function {repo_id: $repo_id}") && edge.targetRepo != anchor {
			continue
		}
		if strings.Contains(normalized, "source.repo_id IN $allowed_repository_ids") &&
			!graphParamContains(params, "allowed_repository_ids", edge.sourceRepo) &&
			!graphParamContains(params, "allowed_scope_ids", edge.sourceRepo) {
			continue
		}
		if strings.Contains(normalized, "target.repo_id IN $allowed_repository_ids") &&
			!graphParamContains(params, "allowed_repository_ids", edge.targetRepo) &&
			!graphParamContains(params, "allowed_scope_ids", edge.targetRepo) {
			continue
		}
		rows = append(rows, edge.row)
	}
	return rows, nil
}

func (g *evaluatingCallGraphEdges) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func callGraphGrantEdges() []callGraphSeedEdge {
	return []callGraphSeedEdge{
		{
			sourceRepo: codeGrantGrantedRepo,
			targetRepo: codeGrantGrantedRepo,
			row: map[string]any{
				"source_uid": "granted-caller", "source_id": "granted-caller",
				"source_name": codeGrantGrantedFunction, "source_path": "internal/auth/session.go",
				"source_language": "go", "source_start_line": int64(1), "source_end_line": int64(9),
				"target_uid": "granted-callee", "target_id": "granted-callee",
				"target_name": codeGrantGrantedFunction + "Callee", "target_path": "internal/auth/session.go",
				"target_language": "go", "target_start_line": int64(10), "target_end_line": int64(20),
			},
		},
		{
			sourceRepo: codeGrantOtherRepo,
			targetRepo: codeGrantOtherRepo,
			row: map[string]any{
				"source_uid": "other-caller", "source_id": "other-caller",
				"source_name": codeGrantUngrantedFunction, "source_path": "internal/auth/session.go",
				"source_language": "go", "source_start_line": int64(1), "source_end_line": int64(9),
				"target_uid": "other-callee", "target_id": "other-callee",
				"target_name": codeGrantUngrantedFunction + "Callee", "target_path": "internal/auth/session.go",
				"target_language": "go", "target_start_line": int64(10), "target_end_line": int64(20),
			},
		},
	}
}

func TestCallGraphMetricsBodyCarriesOnlyGrantedFunctions(t *testing.T) {
	t.Parallel()

	graph := &evaluatingCallGraphEdges{edges: callGraphGrantEdges()}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	req := newCodeGrantRouteRequest(t, "/api/v0/code/call-graph/metrics", callGraphMetricsGrantBody(), &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedFunction) {
		t.Fatalf("granted tenant's function %q is missing: %s", codeGrantGrantedFunction, body)
	}
	for _, leaked := range []string{codeGrantUngrantedFunction, codeGrantOtherRepo} {
		if strings.Contains(body, leaked) {
			t.Fatalf("scoped call-graph metrics leaked %q: %s", leaked, body)
		}
	}
}

// TestUngrantedRepositorySelectorIsRejectedWith400 pins the status code the
// OpenAPI and MCP descriptions now name. applyRepositorySelectorForCapability
// hands the selector error to WriteGraphReadError, which does not claim it, so
// the caller gets 400 -- not the 404 an earlier draft of those descriptions
// claimed.
func TestUngrantedRepositorySelectorIsRejectedWith400(t *testing.T) {
	t.Parallel()

	graph := &evaluatingCallGraphEdges{edges: callGraphGrantEdges()}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	body := map[string]any{"repo_id": codeGrantOtherRepo, "metric_type": "hub_functions"}
	req := newCodeGrantRouteRequest(t, "/api/v0/code/call-graph/metrics", body, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d for an ungranted repository selector; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) != 0 {
		t.Fatalf("an ungranted selector reached the edge scan: %v", graph.statements)
	}
	if strings.Contains(rec.Body.String(), codeGrantUngrantedFunction) {
		t.Fatalf("rejection body leaked the other tenant's rows: %s", rec.Body.String())
	}
}
