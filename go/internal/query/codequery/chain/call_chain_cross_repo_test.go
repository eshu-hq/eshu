// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestMain registers the call-chain capability row the moved HTTP tests drive
// through. Production registers it from root package query's
// contract_capability_matrix.go init(), which this package's test binary does
// not import (root imports the leaves, so that would be an import cycle).
// Without the row every handler answers 501 unsupported_capability. The
// values mirror that matrix entry.
func TestMain(m *testing.M) {
	authoritative := querycontract.TruthLevelExact
	fullStack := querycontract.TruthLevelExact
	production := querycontract.TruthLevelExact
	querycontract.SetCapabilitySupport("call_graph.call_chain_path", querycontract.CapabilitySupport{
		LocalAuthoritativeMax: &authoritative,
		LocalFullStackMax:     &fullStack,
		ProductionMax:         &production,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	})
	os.Exit(m.Run())
}

// newChainRouteRequest builds an envelope-accepting POST request for the
// call-chain route, carrying auth as the request's AuthContext when non-nil
// (nil means an unscoped shared-key caller).
func newChainRouteRequest(t *testing.T, body map[string]any, auth *queryauth.AuthContext) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal(body) error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/call-chain", bytes.NewReader(payload))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	if auth != nil {
		req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), *auth))
	}
	return req
}

// chainReachabilityContentStore answers entity-name resolution from a fixture
// map so the reachability disambiguation path can be driven through the route.
// It embeds the shared port double for the rest of the content port.
type chainReachabilityContentStore struct {
	querytestutil.FakePortContentStore
	byName map[string][]querycontract.EntityContent
}

func (s chainReachabilityContentStore) SearchEntitiesByName(
	_ context.Context,
	_ string,
	_ string,
	name string,
	_ int,
) ([]querycontract.EntityContent, error) {
	return s.byName[name], nil
}

// chainOneHopCaptureGraph records every statement the handler issues. The
// candidate one-hop read resolves the ambiguous start below onto its target;
// the shortestPath read answers nothing, so the route still succeeds.
type chainOneHopCaptureGraph struct {
	statements []string
	params     []map[string]any
}

func (g *chainOneHopCaptureGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.statements = append(g.statements, cypher)
	g.params = append(g.params, params)
	if strings.Contains(cypher, "-[:CALLS]->(target)") && !strings.Contains(cypher, "shortestPath(") {
		if sourceID, _ := params["source_id"].(string); sourceID == "entity:ambig-a" {
			return []map[string]any{{
				"id": "entity:target", "name": "TargetEnd", "labels": []any{"Function"},
			}}, nil
		}
	}
	return nil, nil
}

func (g *chainOneHopCaptureGraph) RunSingle(
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

func TestBuildCallChainCypherCrossRepoUsesEndpointRepositorySelectors(t *testing.T) {
	t.Parallel()

	cypher, params := chain.BuildCallChainCypher(chain.Request{
		StartEntityID: "entity:api-handler",
		EndEntityID:   "entity:billing-charge",
		CrossRepo:     true,
		StartRepoID:   "repo:api",
		EndRepoID:     "repo:billing",
		MaxDepth:      4,
	}, querycontract.GraphBackendNeo4j, querycontract.RepositoryAccessFilter{AllScopes: true})

	for _, fragment := range []string{
		"start.repo_id = $start_repo_id",
		"end.repo_id = $end_repo_id",
		"all(node IN nodes(path) WHERE coalesce(node.repo_id, '') IN $traversal_repo_ids)",
		"[:CALLS*1..4]",
		"LIMIT 5",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cross-repo call-chain cypher missing %q:\n%s", fragment, cypher)
		}
	}
	if got, want := params["start_repo_id"], "repo:api"; got != want {
		t.Fatalf("params[start_repo_id] = %#v, want %#v", got, want)
	}
	if got, want := params["end_repo_id"], "repo:billing"; got != want {
		t.Fatalf("params[end_repo_id] = %#v, want %#v", got, want)
	}
	if got, want := params["traversal_repo_ids"], []string{"repo:api", "repo:billing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("params[traversal_repo_ids] = %#v, want %#v", got, want)
	}
	if _, ok := params["repo_id"]; ok {
		t.Fatalf("params[repo_id] present for cross-repo endpoint selectors: %#v", params)
	}
}

func TestBuildCallChainCypherCrossRepoUsesRepoIDAsMissingEndpointFallback(t *testing.T) {
	t.Parallel()

	cypher, params := chain.BuildCallChainCypher(chain.Request{
		StartEntityID: "entity:api-handler",
		EndEntityID:   "entity:billing-charge",
		CrossRepo:     true,
		StartRepoID:   "repo:api",
		RepoID:        "repo:billing",
		MaxDepth:      4,
	}, querycontract.GraphBackendNeo4j, querycontract.RepositoryAccessFilter{AllScopes: true})

	for _, fragment := range []string{
		"start.repo_id = $start_repo_id",
		"end.repo_id = $end_repo_id",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cross-repo fallback cypher missing %q:\n%s", fragment, cypher)
		}
	}
	if got, want := params["start_repo_id"], "repo:api"; got != want {
		t.Fatalf("params[start_repo_id] = %#v, want %#v", got, want)
	}
	if got, want := params["end_repo_id"], "repo:billing"; got != want {
		t.Fatalf("params[end_repo_id] = %#v, want %#v", got, want)
	}
	if _, ok := params["repo_id"]; ok {
		t.Fatalf("params[repo_id] present for cross-repo fallback selectors: %#v", params)
	}
}

func TestBuildCallChainCypherRepoScopedFiltersEveryPathNode(t *testing.T) {
	t.Parallel()

	cypher, params := chain.BuildCallChainCypher(chain.Request{
		StartEntityID: "entity:start",
		EndEntityID:   "entity:end",
		RepoID:        "repo:billing",
		MaxDepth:      4,
	}, querycontract.GraphBackendNeo4j, querycontract.RepositoryAccessFilter{AllScopes: true})

	for _, fragment := range []string{
		"start.repo_id = $repo_id",
		"end.repo_id = $repo_id",
		"all(node IN nodes(path) WHERE coalesce(node.repo_id, '') = $repo_id)",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("repo-scoped call-chain cypher missing %q:\n%s", fragment, cypher)
		}
	}
	if got, want := params["repo_id"], "repo:billing"; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
}

// TestCallChainCandidateOneHopRowsRepoScopedFiltersTargetRepository proves the
// candidate one-hop read the reachability disambiguation issues binds the
// request's traversal repositories. It used to call the handler method
// directly; the method is unexported and unreachable from this leaf's external
// test package, so the proof now drives the same read through the route: an
// ambiguous start name forces the reachability path, and the capture graph
// judges the statement the handler actually issued.
func TestCallChainCandidateOneHopRowsRepoScopedFiltersTargetRepository(t *testing.T) {
	t.Parallel()

	graph := &chainOneHopCaptureGraph{}
	content := &chainReachabilityContentStore{
		byName: map[string][]querycontract.EntityContent{
			"AmbigStart": {
				{EntityID: "entity:ambig-a", EntityName: "AmbigStart", EntityType: "Function", RepoID: codeGrantGrantedRepo, RelativePath: "internal/a.go"},
				{EntityID: "entity:ambig-b", EntityName: "AmbigStart", EntityType: "Function", RepoID: codeGrantGrantedRepo, RelativePath: "internal/b.go"},
			},
			"TargetEnd": {
				{EntityID: "entity:target", EntityName: "TargetEnd", EntityType: "Function", RepoID: codeGrantGrantedRepo, RelativePath: "internal/target.go"},
			},
		},
	}
	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph,
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newChainRouteRequest(t, map[string]any{
		"start": "AmbigStart", "end": "TargetEnd",
		"repo_id": codeGrantGrantedRepo, "max_depth": 3,
	}, &auth))
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}

	oneHop := 0
	for i, statement := range graph.statements {
		if !strings.Contains(statement, "-[:CALLS]->(target)") || strings.Contains(statement, "shortestPath(") {
			continue
		}
		oneHop++
		if !strings.Contains(statement, "coalesce(target.repo_id, '') IN $traversal_repo_ids") {
			t.Fatalf("candidate one-hop cypher missing repo filter:\n%s", statement)
		}
		if got, want := graph.params[i]["traversal_repo_ids"], []string{codeGrantGrantedRepo}; !reflect.DeepEqual(got, want) {
			t.Fatalf("params[traversal_repo_ids] = %#v, want %#v", got, want)
		}
	}
	if oneHop == 0 {
		t.Fatalf("the disambiguation issued no candidate one-hop read: %v", graph.statements)
	}
}

func TestHandleCallChainRejectsEndpointRepositorySelectorsWithoutCrossRepo(t *testing.T) {
	t.Parallel()

	handler := &codequery.CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start_entity_id":"entity:a","end_entity_id":"entity:b","start_repo_id":"repo:a","end_repo_id":"repo:b"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "start_repo_id and end_repo_id require cross_repo") {
		t.Fatalf("body = %q, want endpoint selector validation", w.Body.String())
	}
}

func TestHandleCallChainRejectsCrossRepoExactIDsWithoutEndpointRepositories(t *testing.T) {
	t.Parallel()

	handler := &codequery.CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start_entity_id":"entity:a","end_entity_id":"entity:b","cross_repo":true}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cross_repo call-chain traversal requires start_repo_id or repo_id") {
		t.Fatalf("body = %q, want endpoint repository validation", w.Body.String())
	}
}

func TestHandleCallChainRejectsCrossRepoEndpointSelectorOutsideGrant(t *testing.T) {
	t.Parallel()

	handler := &codequery.CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start_entity_id":"entity:a","end_entity_id":"entity:b","cross_repo":true,"start_repo_id":"repo-team-a","end_repo_id":"repo-team-b"}`),
	)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "repo-team-b") {
		t.Fatalf("body = %q, want denied repository selector", w.Body.String())
	}
}
