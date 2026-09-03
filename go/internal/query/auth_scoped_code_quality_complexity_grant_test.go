// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// #5167 code-family batch 1, step 5: POST /api/v0/code/quality/inspect and
// POST /api/v0/code/complexity.
//
// Both are Repository-anchored Cypher reads whose only repository predicate was
// the caller's own optional repo_id. Complexity was the worse of the two: its
// entity_id branch (lookupComplexityRowByID) carried no repository predicate at
// all, so it ignored even a repo_id the caller did supply -- a caller who knew
// an entity id read its repository, path, and complexity metrics regardless of
// grant.
//
// Every assertion below reads the Cypher the handler actually hands the graph
// reader, so a builder that loses its predicate fails even though no graph
// exists in the test. The unscoped sub-cases pin the other direction: a
// shared-key caller's query text must not gain a grant predicate.

const codeGrantQualityPredicate = "(repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)"

// capturedCodeCypher is every statement one route handed the graph reader,
// paired with its parameters, in call order.
type capturedCodeCypher struct {
	statements []string
	params     []map[string]any
}

func (c *capturedCodeCypher) record(cypher string, params map[string]any) {
	c.statements = append(c.statements, cypher)
	c.params = append(c.params, params)
}

// matching returns the first captured statement containing marker, so a test
// can name the builder it means rather than depend on call ordering.
func (c *capturedCodeCypher) matching(marker string) (string, map[string]any, bool) {
	for i, statement := range c.statements {
		if strings.Contains(statement, marker) {
			return statement, c.params[i], true
		}
	}
	return "", nil, false
}

func captureCodeQualityCypher(t *testing.T, path string, body map[string]any, auth *AuthContext) (*capturedCodeCypher, int) {
	t.Helper()

	captured := &capturedCodeCypher{}
	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				captured.record(cypher, params)
				return nil, nil
			},
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				captured.record(cypher, params)
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newCodeGrantRouteRequest(t, path, body, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return captured, rec.Code
}

func assertGrantArraysBound(t *testing.T, params map[string]any) {
	t.Helper()
	got, ok := params["allowed_repository_ids"].([]string)
	if !ok || !slices.Equal(got, []string{codeGrantGrantedRepo}) {
		t.Fatalf("params[allowed_repository_ids] = %#v, want [%q]; the predicate references a parameter that is never bound", params["allowed_repository_ids"], codeGrantGrantedRepo)
	}
	if _, ok := params["allowed_scope_ids"]; !ok {
		t.Fatalf("params = %#v, want an allowed_scope_ids binding for the predicate's second disjunct", params)
	}
}

// codeQualityGrantBuilder names one Repository-anchored builder by a marker
// unique to its own Cypher, plus the request body that reaches it.
type codeQualityGrantBuilder struct {
	name   string
	path   string
	body   map[string]any
	marker string
}

func codeQualityGrantBuilders() []codeQualityGrantBuilder {
	return []codeQualityGrantBuilder{
		{
			name:   "inspect_code_quality",
			path:   "/api/v0/code/quality/inspect",
			body:   map[string]any{"check": "complexity"},
			marker: "MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)",
		},
		{
			name:   "complexity_list",
			path:   "/api/v0/code/complexity",
			body:   map[string]any{},
			marker: "coalesce(e.cyclomatic_complexity, 0) > 0",
		},
		{
			name:   "complexity_by_name",
			path:   "/api/v0/code/complexity",
			body:   map[string]any{"function_name": "RefreshSession"},
			marker: "e.name = $entity_name",
		},
		{
			name:   "complexity_by_entity_id",
			path:   "/api/v0/code/complexity",
			body:   map[string]any{"entity_id": "entity-other"},
			marker: "(e {id: $entity_id})",
		},
	}
}

func TestCodeQualityAndComplexityBuildersBindTheGrant(t *testing.T) {
	t.Parallel()

	for _, builder := range codeQualityGrantBuilders() {
		t.Run(builder.name, func(t *testing.T) {
			t.Parallel()

			auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			captured, status := captureCodeQualityCypher(t, builder.path, builder.body, &auth)
			if status >= http.StatusInternalServerError {
				t.Fatalf("status = %d, want a non-server-error response", status)
			}
			statement, params, ok := captured.matching(builder.marker)
			if !ok {
				t.Fatalf("no captured statement contains %q; captured = %#v", builder.marker, captured.statements)
			}
			if !strings.Contains(statement, codeGrantQualityPredicate) {
				t.Fatalf("%s is missing %q; a scoped caller's grant is resolved but never applied:\n%s", builder.name, codeGrantQualityPredicate, statement)
			}
			assertGrantArraysBound(t, params)
		})
	}
}

func TestCodeQualityAndComplexityUnscopedCypherCarriesNoGrant(t *testing.T) {
	t.Parallel()

	for _, builder := range codeQualityGrantBuilders() {
		t.Run(builder.name, func(t *testing.T) {
			t.Parallel()

			captured, status := captureCodeQualityCypher(t, builder.path, builder.body, nil)
			if status >= http.StatusInternalServerError {
				t.Fatalf("status = %d, want a non-server-error response", status)
			}
			statement, params, ok := captured.matching(builder.marker)
			if !ok {
				t.Fatalf("no captured statement contains %q; captured = %#v", builder.marker, captured.statements)
			}
			if strings.Contains(statement, "allowed_repository_ids") {
				t.Fatalf("unscoped %s gained a grant predicate:\n%s", builder.name, statement)
			}
			if _, bound := params["allowed_repository_ids"]; bound {
				t.Fatalf("unscoped %s params = %#v, want no grant arrays", builder.name, params)
			}
		})
	}
}

func TestCodeQualityAndComplexityEmptyGrantSkipTheGraphRead(t *testing.T) {
	t.Parallel()

	for _, builder := range codeQualityGrantBuilders() {
		t.Run(builder.name, func(t *testing.T) {
			t.Parallel()

			auth := codeGrantScopedAuthContext(nil)
			captured, _ := captureCodeQualityCypher(t, builder.path, builder.body, &auth)
			if len(captured.statements) != 0 {
				t.Fatalf("an empty scoped grant reached the graph; want no read at all:\n%s", strings.Join(captured.statements, "\n---\n"))
			}
		})
	}
}

// TestComplexityByEntityIDHonoursASuppliedRepoID pins the second half of the
// entity_id branch's defect. Binding the grant alone would still leave
// lookupComplexityRowByID ignoring a repo_id the caller explicitly supplied, so
// an unscoped caller asking for entity X "in repository A" would get X's row
// from repository B. The branch must anchor on the supplied repository.
func TestComplexityByEntityIDHonoursASuppliedRepoID(t *testing.T) {
	t.Parallel()

	captured, status := captureCodeQualityCypher(
		t,
		"/api/v0/code/complexity",
		map[string]any{"entity_id": "entity-1", "repo_id": codeGrantGrantedRepo},
		nil,
	)
	if status >= http.StatusInternalServerError {
		t.Fatalf("status = %d, want a non-server-error response", status)
	}
	statement, params, ok := captured.matching("(e {id: $entity_id})")
	if !ok {
		t.Fatalf("no captured statement contains the entity-id anchor; captured = %#v", captured.statements)
	}
	if !strings.Contains(statement, "repo.id = $repo_id") {
		t.Fatalf("entity_id lookup ignores the supplied repo_id:\n%s", statement)
	}
	if got, want := params["repo_id"], codeGrantGrantedRepo; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
}
