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

// #5167 code-family batch 2a: two-tenant proof for
// POST /api/v0/code/language-query across all four of its dispatch branches.
//
// The route reads two backends and had no grant on either. Its SQL choke point,
// buildLanguageTypeEntityFilters (content_reader_entity_search.go), carried the
// same `if repoID != "" { repo_id = $n }`-with-no-else shape the four batch-1
// content builders had, so a scoped caller who omitted repo_id read the whole
// content corpus. Its Cypher choke point, buildLanguageCypherWithSemanticFilter
// (language_query_cypher.go), dispatches four builders that each carried
// `AND r.id = $repo_id` under the same condition and nothing otherwise.
//
// LanguageQueryHandler is not CodeHandler, so none of the batch-1 selector
// plumbing was reachable as a method: req.RepoID was used raw and an ungranted
// one was never rejected. The fix reuses the free functions rather than
// duplicating them -- applyRepositorySelectorForAccess and codeContentGrantScope
// (code_repository_selector.go) -- so both handlers resolve a selector and a
// grant through one implementation.

const (
	languageGrantGrantedEntity   = "GrantedLanguageProbe"
	languageGrantUngrantedEntity = "UngrantedLanguageProbe"
)

// languageQueryPlainContentStore satisfies only the ContentStore port method,
// so it drives the per-repository fallback a store that cannot take a grant
// argument forces. An unbound fallback asks for repository "" instead, which
// this store answers with every tenant's row.
type languageQueryPlainContentStore struct {
	fakePortContentStore
	askedRepoIDs []string
}

func (s *languageQueryPlainContentStore) SearchEntitiesByLanguageAndType(
	_ context.Context,
	repoID, _, entityType, _ string,
	_ int,
) ([]EntityContent, error) {
	s.askedRepoIDs = append(s.askedRepoIDs, repoID)
	return languageQueryGrantEntities(repoID, nil, entityType), nil
}

// languageQueryGrantEntities mirrors buildLanguageTypeEntityFilters' real
// contract: an explicit repo_id anchors the scan, a non-empty grant list
// restricts it, and an empty grant list does not restrict it at all. Keeping
// the fake faithful to the shipped SQL is what makes the leak assertions fail
// when the handler stops binding the grant.
func languageQueryGrantEntities(repoID string, allowedRepositoryIDs []string, entityType string) []EntityContent {
	if entityType == "" {
		entityType = "Variable"
	}
	entities := make([]EntityContent, 0, 2)
	for _, candidate := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		if !codeContentGrantAdmits(candidate, repoID, allowedRepositoryIDs) {
			continue
		}
		name := languageGrantGrantedEntity
		if candidate == codeGrantOtherRepo {
			name = languageGrantUngrantedEntity
		}
		entities = append(entities, EntityContent{
			EntityID:     candidate + "#" + name,
			RepoID:       candidate,
			RelativePath: "internal/auth/session.go",
			EntityType:   entityType,
			EntityName:   name,
			Language:     "go",
			StartLine:    10,
			EndLine:      20,
		})
	}
	return entities
}

// languageQueryGraphSeeds is the two-tenant graph fixture every graph-backed
// branch scans: one entity in the granted repository and one in another
// tenant's.
func languageQueryGraphSeeds(label string) []graphGrantSeed {
	return []graphGrantSeed{
		{repoID: codeGrantGrantedRepo, row: languageQueryGraphRow(label, languageGrantGrantedEntity, codeGrantGrantedRepo)},
		{repoID: codeGrantOtherRepo, row: languageQueryGraphRow(label, languageGrantUngrantedEntity, codeGrantOtherRepo)},
	}
}

func languageQueryGraphRow(label, name, repoID string) map[string]any {
	return map[string]any{
		"entity_id":  repoID + "#" + name,
		"name":       name,
		"labels":     []any{label},
		"file_path":  "internal/auth/session.go",
		"repo_id":    repoID,
		"repo_name":  repoID,
		"language":   "go",
		"start_line": int64(10),
		"end_line":   int64(20),
	}
}

// languageQueryGrantBranch is one dispatch branch of handleLanguageQuery, with
// the graph label its Cypher builder uses (empty for the content-only branch).
type languageQueryGrantBranch struct {
	name       string
	entityType string
	graphLabel string
}

func languageQueryGrantBranches() []languageQueryGrantBranch {
	return []languageQueryGrantBranch{
		{name: "guard", entityType: "guard", graphLabel: "Function"},
		{name: "graph_backed", entityType: "function", graphLabel: "Function"},
		{name: "graph_first_content_backed", entityType: "sql_table", graphLabel: "SqlTable"},
		{name: "content_backed", entityType: "variable"},
	}
}

func newLanguageQueryGrantHandler(branch languageQueryGrantBranch, store ContentStore) (*LanguageQueryHandler, *evaluatingRepositoryGraph) {
	handler := &LanguageQueryHandler{Content: store, Profile: ProfileLocalAuthoritative}
	if branch.graphLabel == "" {
		return handler, nil
	}
	graph := &evaluatingRepositoryGraph{
		seeds:             languageQueryGraphSeeds(branch.graphLabel),
		repositoryAlias:   "r",
		repositoryColumns: repositoryProjectedColumns(),
	}
	handler.Neo4j = graph
	return handler, graph
}

func runLanguageQueryGrantRequest(
	t *testing.T,
	handler *LanguageQueryHandler,
	body map[string]any,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := newCodeGrantRouteRequest(t, "/api/v0/code/language-query", body, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func languageQueryGrantBody(entityType string) map[string]any {
	return map[string]any{"language": "go", "entity_type": entityType}
}

func TestLanguageQueryFiltersByRepositoryGrant(t *testing.T) {
	t.Parallel()

	for _, branch := range languageQueryGrantBranches() {
		t.Run(branch.name, func(t *testing.T) {
			t.Parallel()

			handler, _ := newLanguageQueryGrantHandler(branch, &languageQueryPlainContentStore{})
			auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody(branch.entityType), &auth)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, languageGrantGrantedEntity) {
				t.Fatalf("granted tenant's entity %q is missing: %s", languageGrantGrantedEntity, body)
			}
			for _, leaked := range []string{languageGrantUngrantedEntity, codeGrantOtherRepo} {
				if strings.Contains(body, leaked) {
					t.Fatalf("scoped language query leaked %q: %s", leaked, body)
				}
			}
		})
	}
}

func TestLanguageQueryEmptyGrantReachesNoBackend(t *testing.T) {
	t.Parallel()

	for _, branch := range languageQueryGrantBranches() {
		t.Run(branch.name, func(t *testing.T) {
			t.Parallel()

			store := &languageQueryPlainContentStore{}
			handler, graph := newLanguageQueryGrantHandler(branch, store)
			auth := codeGrantScopedAuthContext(nil)
			rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody(branch.entityType), &auth)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if len(store.askedRepoIDs) != 0 {
				t.Fatalf("content store was queried with %#v; a grantless scoped caller must not reach a backend", store.askedRepoIDs)
			}
			if graph != nil && len(graph.statements) != 0 {
				t.Fatalf("a grantless scoped caller reached the graph: %v", graph.statements)
			}
			body := rec.Body.String()
			for _, leaked := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
				if strings.Contains(body, leaked) {
					t.Fatalf("response leaked rows for an empty-grant caller: %s", body)
				}
			}
		})
	}
}

func TestLanguageQueryResolvesAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	for _, branch := range languageQueryGrantBranches() {
		t.Run(branch.name, func(t *testing.T) {
			t.Parallel()

			handler, _ := newLanguageQueryGrantHandler(branch, &languageQueryPlainContentStore{})
			auth := codeGrantScopeOnlyAuthContext([]string{codeGrantGrantedRepo})
			rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody(branch.entityType), &auth)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, languageGrantGrantedEntity) {
				t.Fatalf("scope-only grant returned no row for the repository it names: %s", body)
			}
			if strings.Contains(body, languageGrantUngrantedEntity) {
				t.Fatalf("scope-only language query leaked %q: %s", languageGrantUngrantedEntity, body)
			}
		})
	}
}

func TestLanguageQuerySharedKeyReadIsUnchanged(t *testing.T) {
	t.Parallel()

	for _, branch := range languageQueryGrantBranches() {
		t.Run(branch.name, func(t *testing.T) {
			t.Parallel()

			handler, _ := newLanguageQueryGrantHandler(branch, &languageQueryPlainContentStore{})
			rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody(branch.entityType), nil)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			for _, want := range []string{languageGrantGrantedEntity, languageGrantUngrantedEntity} {
				if !strings.Contains(body, want) {
					t.Fatalf("unscoped language query lost %q: %s", want, body)
				}
			}
		})
	}
}

// TestLanguageQueryGraphlessProfileBindsTheContentFallback covers the branch a
// graphless profile takes: queryByLanguageWithSemanticFilter falls through to
// the content store when no graph backend is configured, and that fallback is a
// separate read from the one the content-only entity types take.
func TestLanguageQueryGraphlessProfileBindsTheContentFallback(t *testing.T) {
	t.Parallel()

	store := &languageQueryPlainContentStore{}
	handler := &LanguageQueryHandler{Content: store, Profile: ProfileLocalAuthoritative}
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody("function"), &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !slices.Equal(store.askedRepoIDs, []string{codeGrantGrantedRepo}) {
		t.Fatalf("content fallback repositories = %#v, want [%q]; a graphless read must iterate the grant, never ask for every repository", store.askedRepoIDs, codeGrantGrantedRepo)
	}
	body := rec.Body.String()
	if strings.Contains(body, languageGrantUngrantedEntity) {
		t.Fatalf("the graphless content fallback leaked %q: %s", languageGrantUngrantedEntity, body)
	}
}

// TestLanguageQueryMetadataEnrichmentCannotWidenTheAnswer pins the second
// content read on the graph-backed branch. enrichLanguageResultsWithContentMetadata
// runs its own SearchEntitiesByLanguageAndType after the graph read; an unbound
// one reads every tenant's rows to build its merge key map.
func TestLanguageQueryMetadataEnrichmentCannotWidenTheAnswer(t *testing.T) {
	t.Parallel()

	store := &languageQueryPlainContentStore{}
	handler, graph := newLanguageQueryGrantHandler(
		languageQueryGrantBranch{name: "graph_backed", entityType: "function", graphLabel: "Function"},
		store,
	)
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody("function"), &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) == 0 {
		t.Fatal("no statement reached the graph")
	}
	for _, asked := range store.askedRepoIDs {
		if asked != codeGrantGrantedRepo {
			t.Fatalf("the metadata enrichment read asked for repository %q, want only %q", asked, codeGrantGrantedRepo)
		}
	}
	if strings.Contains(rec.Body.String(), languageGrantUngrantedEntity) {
		t.Fatalf("the enrichment read widened the answer to %q: %s", languageGrantUngrantedEntity, rec.Body.String())
	}
}

// TestLanguageQueryUngrantedRepositorySelectorIsRejected is the selector half of
// the promotion. Before this change req.RepoID was used raw: an ungranted
// repository id was pushed straight into the query instead of being refused,
// which is the behaviour the OpenAPI operation and the MCP tool description now
// promise.
func TestLanguageQueryUngrantedRepositorySelectorIsRejected(t *testing.T) {
	t.Parallel()

	store := &languageQueryPlainContentStore{}
	handler, graph := newLanguageQueryGrantHandler(
		languageQueryGrantBranch{name: "graph_backed", entityType: "function", graphLabel: "Function"},
		store,
	)
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	body := languageQueryGrantBody("function")
	body["repo_id"] = codeGrantOtherRepo
	rec := runLanguageQueryGrantRequest(t, handler, body, &auth)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d for an ungranted repository selector; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) != 0 {
		t.Fatalf("an ungranted selector reached the graph: %v", graph.statements)
	}
	if len(store.askedRepoIDs) != 0 {
		t.Fatalf("an ungranted selector reached the content store: %#v", store.askedRepoIDs)
	}
	if strings.Contains(rec.Body.String(), languageGrantUngrantedEntity) {
		t.Fatalf("rejection body leaked the other tenant's rows: %s", rec.Body.String())
	}
}

// unscopedLanguageQueryGrant is the grant an unscoped shared-key, admin, or
// local caller carries: no restriction on either backend. It is what the
// pre-existing language-query tests pass, so their assertions keep describing
// the unscoped read.
func unscopedLanguageQueryGrant() languageQueryGrant {
	return languageQueryGrant{access: repositoryAccessFilter{AllScopes: true}}
}

// TestLanguageQuerySharedKeyRepoIDGoesThroughTheSelector covers the half of the
// contract change that lands on callers who are NOT scoped tokens.
//
// The sibling tests above pass no repo_id at all, which is exactly the case the
// selector never touches, so on their own they prove nothing about it. Routing
// req.RepoID through applyRepositorySelectorForAccess changes what an unscoped
// shared-key, admin or local caller gets for a repo_id that is not a canonical
// id: the OpenAPI operation has always advertised the field as "canonical ID,
// name, slug, or path", and until now this route ignored every form but the
// first.
//
// The unresolvable sub-case runs on the content-backed branch, where h.Neo4j is
// nil, on purpose. evaluatingRepositoryGraph answers the selector's own
// MATCH (r:Repository) probe from its seeded rows, so a graph-backed handler
// would resolve a selector that does not exist in the fixture.
func TestLanguageQuerySharedKeyRepoIDGoesThroughTheSelector(t *testing.T) {
	t.Parallel()

	t.Run("canonical_id_anchors_the_read", func(t *testing.T) {
		t.Parallel()

		branch := languageQueryGrantBranch{name: "graph_backed", entityType: "function", graphLabel: "Function"}
		handler, graph := newLanguageQueryGrantHandler(branch, &languageQueryPlainContentStore{})
		body := languageQueryGrantBody("function")
		body["repo_id"] = codeGrantGrantedRepo
		rec := runLanguageQueryGrantRequest(t, handler, body, nil)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if len(graph.statements) == 0 {
			t.Fatal("no statement reached the graph")
		}
		if !strings.Contains(normalizeCypherWhitespace(graph.statements[0]), "r.id = $repo_id") {
			t.Fatalf("a canonical repo_id no longer anchors the read:\n%s", graph.statements[0])
		}
		if !strings.Contains(rec.Body.String(), languageGrantGrantedEntity) {
			t.Fatalf("the named repository's entity is missing: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), languageGrantUngrantedEntity) {
			t.Fatalf("a canonical repo_id returned another repository's rows: %s", rec.Body.String())
		}
	})

	t.Run("resolvable_name_anchors_the_read", func(t *testing.T) {
		t.Parallel()

		store := &languageQueryPlainContentStore{
			fakePortContentStore: fakePortContentStore{repositories: []RepositoryCatalogEntry{{
				ID:   codeGrantGrantedRepo,
				Name: "granted-service",
			}}},
		}
		handler := &LanguageQueryHandler{Content: store, Profile: ProfileLocalAuthoritative}
		body := languageQueryGrantBody("variable")
		body["repo_id"] = "granted-service"
		rec := runLanguageQueryGrantRequest(t, handler, body, nil)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if !slices.Equal(store.askedRepoIDs, []string{codeGrantGrantedRepo}) {
			t.Fatalf("content read repositories = %#v, want [%q]; a repository name must resolve to its canonical id before the read", store.askedRepoIDs, codeGrantGrantedRepo)
		}
		if strings.Contains(rec.Body.String(), languageGrantUngrantedEntity) {
			t.Fatalf("a resolved repository name returned another repository's rows: %s", rec.Body.String())
		}
	})

	t.Run("unresolvable_selector_is_rejected", func(t *testing.T) {
		t.Parallel()

		store := &languageQueryPlainContentStore{}
		handler := &LanguageQueryHandler{Content: store, Profile: ProfileLocalAuthoritative}
		body := languageQueryGrantBody("variable")
		body["repo_id"] = "no-such-repository"
		rec := runLanguageQueryGrantRequest(t, handler, body, nil)

		if got, want := rec.Code, http.StatusBadRequest; got != want {
			t.Fatalf("status = %d, want %d for a repo_id that resolves to nothing; body = %s", got, want, rec.Body.String())
		}
		if len(store.askedRepoIDs) != 0 {
			t.Fatalf("an unresolvable repo_id reached the content store: %#v", store.askedRepoIDs)
		}
	})
}
