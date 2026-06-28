// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestBuildCallChainCypherCrossRepoUsesEndpointRepositorySelectors(t *testing.T) {
	t.Parallel()

	cypher, params := buildCallChainCypher(callChainRequest{
		StartEntityID: "entity:api-handler",
		EndEntityID:   "entity:billing-charge",
		CrossRepo:     true,
		StartRepoID:   "repo:api",
		EndRepoID:     "repo:billing",
		MaxDepth:      4,
	}, GraphBackendNeo4j)

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

	cypher, params := buildCallChainCypher(callChainRequest{
		StartEntityID: "entity:api-handler",
		EndEntityID:   "entity:billing-charge",
		CrossRepo:     true,
		StartRepoID:   "repo:api",
		RepoID:        "repo:billing",
		MaxDepth:      4,
	}, GraphBackendNeo4j)

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

	cypher, params := buildCallChainCypher(callChainRequest{
		StartEntityID: "entity:start",
		EndEntityID:   "entity:end",
		RepoID:        "repo:billing",
		MaxDepth:      4,
	}, GraphBackendNeo4j)

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

func TestCallChainCandidateOneHopRowsRepoScopedFiltersTargetRepository(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "coalesce(target.repo_id, '') IN $traversal_repo_ids") {
					t.Fatalf("candidate one-hop cypher missing repo filter:\n%s", cypher)
				}
				if got, want := params["traversal_repo_ids"], []string{"repo:billing"}; !reflect.DeepEqual(got, want) {
					t.Fatalf("params[traversal_repo_ids] = %#v, want %#v", got, want)
				}
				return nil, nil
			},
		},
	}

	_, err := handler.callChainCandidateOneHopRows(
		context.Background(),
		&callChainRequest{RepoID: "repo:billing"},
		"entity:start",
		"Function",
	)
	if err != nil {
		t.Fatalf("callChainCandidateOneHopRows() error = %v, want nil", err)
	}
}

func TestHandleCallChainRejectsEndpointRepositorySelectorsWithoutCrossRepo(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
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

	handler := &CodeHandler{}
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

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/call-chain",
		bytes.NewBufferString(`{"start_entity_id":"entity:a","end_entity_id":"entity:b","cross_repo":true,"start_repo_id":"repo-team-a","end_repo_id":"repo-team-b"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
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
