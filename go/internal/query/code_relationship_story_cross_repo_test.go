// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleRelationshipStoryRejectsCrossRepoNameWithoutAnchor(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"target":"chargeCard","cross_repo":true}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cross_repo relationship story requires repo_id") {
		t.Fatalf("body = %q, want cross-repo anchor validation", w.Body.String())
	}
}

func TestHandleRelationshipStoryRejectsCrossRepoRepositoryOutsideGrant(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"target":"chargeCard","repo_id":"repo-team-b","cross_repo":true}`),
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

func TestHandleRelationshipStoryExactEntityIDHidesOutOfRepoEntityMetadata(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content: relationshipStoryContentStore{entity: &EntityContent{
			EntityID:   "entity:private",
			EntityName: "privateHandler",
			RepoID:     "repo-team-b",
		}},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"entity_id":"entity:private","repo_id":"repo-team-a","cross_repo":true}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "privateHandler") || strings.Contains(w.Body.String(), "repo-team-b") {
		t.Fatalf("body = %q, must not disclose out-of-repo entity metadata", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"not_found"`) {
		t.Fatalf("body = %q, want not_found resolution", w.Body.String())
	}
}

func TestHandleRelationshipStoryRejectsCrossRepoClassHierarchyEnrichment(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"query_type":"class_hierarchy","entity_id":"class:a","repo_id":"repo-team-a","cross_repo":true}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cross_repo class_hierarchy enrichment is not supported") {
		t.Fatalf("body = %q, want class hierarchy enrichment validation", w.Body.String())
	}
}

func TestHandleRelationshipStoryRejectsCrossRepoOverridesEnrichment(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships/story",
		bytes.NewBufferString(`{"query_type":"overrides","repo_id":"repo-team-a","cross_repo":true}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cross_repo overrides enrichment is not supported") {
		t.Fatalf("body = %q, want overrides enrichment validation", w.Body.String())
	}
}

func TestRelationshipStoryDataMarksCrossRepoScopeAndCoverage(t *testing.T) {
	t.Parallel()

	data := relationshipStoryData(relationshipStoryRequest{
		EntityID:         "entity:charge-card",
		RepoID:           "repo:billing",
		CrossRepo:        true,
		Direction:        "incoming",
		RelationshipType: "CALLS",
		Limit:            10,
	}, relationshipStoryResolution{Status: "resolved", EntityID: "entity:charge-card"}, nil)

	scope := data["scope"].(map[string]any)
	if got, want := scope["cross_repo"], true; got != want {
		t.Fatalf("scope[cross_repo] = %#v, want %#v", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["scope_mode"], "cross_repo"; got != want {
		t.Fatalf("coverage[scope_mode] = %#v, want %#v", got, want)
	}
}

func TestRelationshipStoryGraphCypherCrossRepoScopesAnchorAndRelatedRepositories(t *testing.T) {
	t.Parallel()

	cypher, params := relationshipStoryGraphCypher(
		relationshipStoryRequest{
			EntityID:         "entity:charge-card",
			RepoID:           "repo:billing",
			CrossRepo:        true,
			Direction:        "incoming",
			RelationshipType: "CALLS",
		},
		nil,
		"incoming",
		graphEntityIDPredicate,
		repositoryAccessFilter{
			allowedRepositoryIDs: []string{"repo:billing", "repo:api"},
			allowed: map[string]struct{}{
				"repo:billing": {},
				"repo:api":     {},
			},
		},
	)

	for _, fragment := range []string{
		"targetRepo.id = $repo_id",
		"sourceRepo.id IN $relationship_repo_ids",
		"targetRepo.id IN $relationship_repo_ids",
		"sourceRepo.id as source_repo_id",
		"targetRepo.id as target_repo_id",
		"'direct_code_edge' as edge_origin",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("cross-repo story cypher missing %q:\n%s", fragment, cypher)
		}
	}
	if got, want := params["repo_id"], "repo:billing"; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
	repos, ok := params["relationship_repo_ids"].([]string)
	if !ok || len(repos) != 2 {
		t.Fatalf("params[relationship_repo_ids] = %#v, want two scoped repos", params["relationship_repo_ids"])
	}
}

func TestRelationshipStoryGraphCypherRepoScopedKeepsBothEndpointsInRepository(t *testing.T) {
	t.Parallel()

	cypher, params := relationshipStoryGraphCypher(
		relationshipStoryRequest{
			EntityID:         "entity:charge-card",
			RepoID:           "repo:billing",
			Direction:        "incoming",
			RelationshipType: "CALLS",
		},
		nil,
		"incoming",
		graphEntityIDPredicate,
		repositoryAccessFilter{allScopes: true},
	)

	for _, fragment := range []string{
		"sourceRepo.id = $repo_id",
		"targetRepo.id = $repo_id",
	} {
		if !strings.Contains(cypher, fragment) {
			t.Fatalf("repo-scoped story cypher missing %q:\n%s", fragment, cypher)
		}
	}
	if got, want := params["repo_id"], "repo:billing"; got != want {
		t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
	}
}
