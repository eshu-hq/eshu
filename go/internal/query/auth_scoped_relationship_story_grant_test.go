// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #5167 code-family batch 2b: two-tenant grant proofs for
// POST /api/v0/code/relationships/story.
//
// The route already carried grant text before this batch --
// relationshipStoryRepoPredicates rendered `sourceRepo.id IN …` and
// `targetRepo.id IN …` for a scoped caller -- and a live run against the pinned
// NornicDB measured that it filtered nothing: both consumers attached it to a
// WHERE that follows their OPTIONAL MATCH repository chains, so an out-of-grant
// row survived with the last optional pattern's variables nulled and the Go
// normalization then refilled the repository column from the node property.
//
// Every assertion below is against the serialized response body, and the graph
// fake judges clause attachment rather than substring presence, so putting the
// predicates back where they were reds these tests.
const (
	storyGrantedAnchor    = "entity:story-granted-anchor"
	storyGrantedNeighbour = "StoryGrantedNeighbour"
	storyUngrantedTarget  = "StoryUngrantedTarget"
	storyUngrantedEntity  = "entity:story-ungranted-target"
	storyAmbiguousName    = "StoryAmbiguousSymbol"
)

// storyGrantHandler builds a route handler over the two fakes, with the anchor
// entity resolvable from the content store.
func storyGrantHandler(backend GraphBackend, graph GraphQuery, content ContentStore) *CodeHandler {
	return &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: backend,
		Neo4j:        graph,
		Content:      content,
	}
}

func storyGrantContent() *storyGrantContentStore {
	return &storyGrantContentStore{
		entities: map[string]EntityContent{
			storyGrantedAnchor: {
				EntityID:   storyGrantedAnchor,
				EntityName: "StoryAnchor",
				EntityType: "Function",
				RepoID:     codeGrantGrantedRepo,
				Language:   "go",
			},
		},
	}
}

// storyNornicDBSeeds returns the two raw NornicDB projection rows the direct
// story read produces: one neighbour in the granted repository and one in the
// out-of-grant repository.
func storyNornicDBSeeds() []storyGrantSeed {
	return []storyGrantSeed{
		{
			repoByAlias: map[string]string{"anchor": codeGrantGrantedRepo, "target": codeGrantGrantedRepo},
			row: map[string]any{
				"direction": "outgoing", "type": "CALLS",
				"source_uid": storyGrantedAnchor, "source_name": "StoryAnchor",
				"source_node_repo_id": codeGrantGrantedRepo,
				"target_uid":          "entity:story-granted-neighbour", "target_name": storyGrantedNeighbour,
				"target_node_repo_id":     codeGrantGrantedRepo,
				"target_repo_fallback_id": codeGrantGrantedRepo,
				"target_file_path":        "internal/neighbour.go",
			},
		},
		{
			repoByAlias: map[string]string{"anchor": codeGrantGrantedRepo, "target": codeGrantOtherRepo},
			row: map[string]any{
				"direction": "outgoing", "type": "CALLS",
				"source_uid": storyGrantedAnchor, "source_name": "StoryAnchor",
				"source_node_repo_id": codeGrantGrantedRepo,
				"target_uid":          storyUngrantedEntity, "target_name": storyUngrantedTarget,
				"target_node_repo_id":     codeGrantOtherRepo,
				"target_repo_fallback_id": codeGrantOtherRepo,
				"target_file_path":        "internal/other.go",
			},
		},
	}
}

// storyCompatSeeds is the same pair in the Neo4j-compat projection.
func storyCompatSeeds() []storyGrantSeed {
	return []storyGrantSeed{
		{
			repoByAlias: map[string]string{"source": codeGrantGrantedRepo, "target": codeGrantGrantedRepo},
			row: map[string]any{
				"direction": "outgoing", "type": "CALLS",
				"source_id": storyGrantedAnchor, "source_name": "StoryAnchor",
				"source_repo_id": codeGrantGrantedRepo,
				"target_id":      "entity:story-granted-neighbour", "target_name": storyGrantedNeighbour,
				"target_repo_id":   codeGrantGrantedRepo,
				"target_file_path": "internal/neighbour.go",
			},
		},
		{
			repoByAlias: map[string]string{"source": codeGrantGrantedRepo, "target": codeGrantOtherRepo},
			row: map[string]any{
				"direction": "outgoing", "type": "CALLS",
				"source_id": storyGrantedAnchor, "source_name": "StoryAnchor",
				"source_repo_id": codeGrantGrantedRepo,
				"target_id":      storyUngrantedEntity, "target_name": storyUngrantedTarget,
				"target_repo_id":   codeGrantOtherRepo,
				"target_file_path": "internal/other.go",
			},
		},
	}
}

func storyGrantGraphFor(backend GraphBackend) *storyClauseGraph {
	if backend == GraphBackendNornicDB {
		return &storyClauseGraph{
			seeds: storyNornicDBSeeds(),
			optionalColumns: []string{
				"source_repo_fallback_id", "target_repo_fallback_id",
				"source_file_path", "target_file_path",
			},
		}
	}
	return &storyClauseGraph{
		seeds: storyCompatSeeds(),
		optionalColumns: []string{
			"source_repo_id", "target_repo_id",
			"source_file_path", "target_file_path",
		},
	}
}

func runStoryRequest(t *testing.T, handler *CodeHandler, body map[string]any, auth *AuthContext) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newCodeGrantRouteRequest(t, "/api/v0/code/relationships/story", body, auth))
	return rec
}

func storyDirectRequestBody() map[string]any {
	return map[string]any{
		"entity_id":         storyGrantedAnchor,
		"relationship_type": "CALLS",
		"direction":         "outgoing",
		"limit":             50,
	}
}

// TestRelationshipStoryFiltersByRepositoryGrant is the headline: a scoped caller
// that names no repository must not receive the out-of-grant neighbour, on
// either backend.
func TestRelationshipStoryFiltersByRepositoryGrant(t *testing.T) {
	t.Parallel()

	for _, backend := range []GraphBackend{GraphBackendNornicDB, GraphBackendNeo4j} {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			graph := storyGrantGraphFor(backend)
			auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			rec := runStoryRequest(t, storyGrantHandler(backend, graph, storyGrantContent()), storyDirectRequestBody(), &auth)
			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, storyGrantedNeighbour) {
				t.Fatalf("the granted neighbour %q is missing: %s", storyGrantedNeighbour, body)
			}
			for _, leaked := range []string{storyUngrantedTarget, storyUngrantedEntity, codeGrantOtherRepo} {
				if strings.Contains(body, leaked) {
					t.Fatalf("the scoped story leaked %q: %s", leaked, body)
				}
			}
		})
	}
}

// TestRelationshipStorySharedKeyReadIsUnchanged pins the other direction: an
// unscoped caller still sees both tenants and its statement carries no grant.
func TestRelationshipStorySharedKeyReadIsUnchanged(t *testing.T) {
	t.Parallel()

	for _, backend := range []GraphBackend{GraphBackendNornicDB, GraphBackendNeo4j} {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			graph := storyGrantGraphFor(backend)
			rec := runStoryRequest(t, storyGrantHandler(backend, graph, storyGrantContent()), storyDirectRequestBody(), nil)
			body := rec.Body.String()
			for _, want := range []string{storyGrantedNeighbour, storyUngrantedTarget} {
				if !strings.Contains(body, want) {
					t.Fatalf("the shared-key story lost %q: %s", want, body)
				}
			}
			for _, statement := range graph.recordedStatements() {
				if strings.Contains(statement, "$allowed_repository_ids") {
					t.Fatalf("an unscoped caller rendered a grant array:\n%s", statement)
				}
			}
		})
	}
}

// TestRelationshipStoryEmptyGrantReachesNoBackend proves a grantless scoped
// caller is refused in front of both backends and gets the route's own
// not-found story, indistinguishable from a target that does not exist.
func TestRelationshipStoryEmptyGrantReachesNoBackend(t *testing.T) {
	t.Parallel()

	graph := storyGrantGraphFor(GraphBackendNornicDB)
	content := storyGrantContent()
	auth := codeGrantScopedAuthContext(nil)
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, graph, content), storyDirectRequestBody(), &auth)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if statements := graph.recordedStatements(); len(statements) != 0 {
		t.Fatalf("a grantless scoped caller reached the graph: %v", statements)
	}
	if content.reachedTheStore() {
		t.Fatalf("a grantless scoped caller reached the content store: repos=%v entities=%v anyRepo=%v",
			content.askedRepo, content.askedEntity, content.anyRepo)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"status":"not_found"`) {
		t.Fatalf("empty-grant story is not the route's not-found answer: %s", body)
	}
	if strings.Contains(body, storyGrantedNeighbour) || strings.Contains(body, storyUngrantedTarget) {
		t.Fatalf("empty-grant story returned rows: %s", body)
	}
}

// TestRelationshipStoryEmptyGrantNamingARepositoryReachesNoBackend is the other
// caller shape. The test above sends no repo_id, which is the case the empty-
// grant gate answers; a grantless caller that NAMES one is refused earlier, by
// the repository selector, and never reaches that gate. Either way no backend
// is read -- which is the property both halves exist to pin -- but the answer
// the caller sees differs, and the PR body says so rather than claiming one
// shape for both.
func TestRelationshipStoryEmptyGrantNamingARepositoryReachesNoBackend(t *testing.T) {
	t.Parallel()

	graph := storyGrantGraphFor(GraphBackendNornicDB)
	content := storyGrantContent()
	auth := codeGrantScopedAuthContext(nil)
	body := storyDirectRequestBody()
	body["repo_id"] = codeGrantGrantedRepo
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, graph, content), body, &auth)
	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if statements := graph.recordedStatements(); len(statements) != 0 {
		t.Fatalf("a grantless scoped caller reached the graph: %v", statements)
	}
	if content.reachedTheStore() {
		t.Fatalf("a grantless scoped caller reached the content store: repos=%v entities=%v anyRepo=%v",
			content.askedRepo, content.askedEntity, content.anyRepo)
	}
	if leaked := rec.Body.String(); strings.Contains(leaked, storyGrantedNeighbour) ||
		strings.Contains(leaked, storyUngrantedTarget) {
		t.Fatalf("the refusal named a row: %s", leaked)
	}
}

// TestRelationshipStoryResolvesAScopeOnlyGrantToItsRepository covers a grant
// that names the git ingestion scope rather than the repository it owns.
func TestRelationshipStoryResolvesAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	graph := storyGrantGraphFor(GraphBackendNornicDB)
	auth := AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"git-repository-scope:" + codeGrantGrantedRepo},
	}
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, graph, storyGrantContent()), storyDirectRequestBody(), &auth)
	body := rec.Body.String()
	if !strings.Contains(body, storyGrantedNeighbour) {
		t.Fatalf("a scope-only grant did not reach the repository it owns: %s", body)
	}
	if strings.Contains(body, storyUngrantedTarget) {
		t.Fatalf("scope-only story leaked %q: %s", storyUngrantedTarget, body)
	}
}

// TestRelationshipStoryAmbiguousCandidatesStayInGrant covers the leak that has
// nothing to do with Cypher: an ambiguous target name used to be resolved with
// SearchEntitiesByNameAnyRepo, and the ambiguous response lists every candidate
// with its entity id, path and repository id.
func TestRelationshipStoryAmbiguousCandidatesStayInGrant(t *testing.T) {
	t.Parallel()

	content := storyGrantContent()
	content.byName = []EntityContent{
		{
			EntityID: "entity:story-ambiguous-granted", EntityName: storyAmbiguousName,
			EntityType: "Function", RepoID: codeGrantGrantedRepo, RelativePath: "internal/a.go",
		},
		{
			EntityID: storyUngrantedEntity, EntityName: storyAmbiguousName,
			EntityType: "Function", RepoID: codeGrantOtherRepo, RelativePath: "internal/b.go",
		},
	}
	graph := storyGrantGraphFor(GraphBackendNornicDB)
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runStoryRequest(t, storyGrantHandler(GraphBackendNornicDB, graph, content), map[string]any{
		"target":            storyAmbiguousName,
		"relationship_type": "CALLS",
		"direction":         "outgoing",
		"limit":             50,
	}, &auth)
	body := rec.Body.String()
	if content.anyRepo {
		t.Fatalf("a scoped candidate lookup used the corpus-wide any-repo search")
	}
	if got := content.askedRepositories(); len(got) != 1 || got[0] != codeGrantGrantedRepo {
		t.Fatalf("candidate lookup asked repositories %v, want only %q", got, codeGrantGrantedRepo)
	}
	for _, leaked := range []string{storyUngrantedEntity, codeGrantOtherRepo, "internal/b.go"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("the ambiguous candidate list leaked %q: %s", leaked, body)
		}
	}
}
