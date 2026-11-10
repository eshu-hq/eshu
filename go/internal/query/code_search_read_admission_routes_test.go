// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/code"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/entity"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// codeSearchPinnedRoutes is the exact route surface the #9669 W4 pin
// covers: the code search route plus the symbol lookup route. The
// handler tests below and the spec-fragment parity test both range over
// this list, so a route added without pin review fails the seal
// elsewhere and a route dropped breaks every subtest here.
var codeSearchPinnedRoutes = []string{
	"POST /api/v0/code/search",
	"POST /api/v0/code/symbols/search",
}

// codeSearchGraphDouble is the pin-half double for the graph lane: it
// implements querycontract.GraphQuery, records every call, and answers
// Run from a caller-installed func. RunSingle exists only because the
// port demands it — the pin keeps it OUT and no code search subtest
// below may record one.
type codeSearchGraphDouble struct {
	calls []string
	run   func(context.Context, string, map[string]any) ([]map[string]any, error)
}

func (g *codeSearchGraphDouble) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.calls = append(g.calls, "Run")
	if g.run == nil {
		return nil, nil
	}
	return g.run(ctx, cypher, params)
}

func (g *codeSearchGraphDouble) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	g.calls = append(g.calls, "RunSingle")
	return nil, nil
}

var _ querycontract.GraphQuery = (*codeSearchGraphDouble)(nil)

// codeSearchContentDouble is the pin-half double for the content lanes:
// it embeds the shared content.FakePortContentStore double and
// implements exactly the eight pinned content reads, recording every
// call and capturing the entity-name search for grant assertions. The
// handlers type-assert these narrow seams off the ContentStore port, so
// serving the routes through this double proves the frozen content half
// of the pin — nothing wider — is enough to answer them.
type codeSearchContentDouble struct {
	content.FakePortContentStore
	calls      []string
	nameSearch entity.EntityNameSearch
	namesRows  []querycontract.EntityContent
	symbolRows []querycontract.EntityContent
}

func (d *codeSearchContentDouble) SearchEntityNames(_ context.Context, search entity.EntityNameSearch) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntityNames")
	d.nameSearch = search
	return d.namesRows, nil
}

func (d *codeSearchContentDouble) SearchSymbols(_ context.Context, _ codequery.SymbolSearchRequest) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchSymbols")
	return d.symbolRows, nil
}

func (d *codeSearchContentDouble) SearchCodeCandidates(ctx context.Context, repoID, pattern, language string, limit int, exact bool) ([]querycontract.EntityContent, []querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchCodeCandidates")
	return d.FakePortContentStore.SearchCodeCandidates(ctx, repoID, pattern, language, limit, exact)
}

func (d *codeSearchContentDouble) SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntitiesByName")
	return d.FakePortContentStore.SearchEntitiesByName(ctx, repoID, entityType, name, limit)
}

func (d *codeSearchContentDouble) SearchEntityContent(ctx context.Context, repoID, pattern string, limit int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntityContent")
	return d.FakePortContentStore.SearchEntityContent(ctx, repoID, pattern, limit)
}

func (d *codeSearchContentDouble) SearchEntitiesByNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntitiesByNameAnyRepo")
	return d.FakePortContentStore.SearchEntitiesByNameAnyRepo(ctx, entityType, name, limit)
}

func (d *codeSearchContentDouble) SearchEntityContentAnyRepo(ctx context.Context, pattern string, limit int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntityContentAnyRepo")
	return d.FakePortContentStore.SearchEntityContentAnyRepo(ctx, pattern, limit)
}

func (d *codeSearchContentDouble) ListRepoEntitiesByIDs(ctx context.Context, repoID string, entityIDs []string, limit int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "ListRepoEntitiesByIDs")
	return d.FakePortContentStore.ListRepoEntitiesByIDs(ctx, repoID, entityIDs, limit)
}

// codeSearchNameFallbackDouble serves the symbol plain-name fallback the
// handlers take when the store does not implement the symbol-aware
// seam. It deliberately lacks SearchSymbols so the handlers route into
// the fallback branch; the member carries the exact pinned signature,
// so the recorded call still proves the frozen shape serves it.
type codeSearchNameFallbackDouble struct {
	content.FakePortContentStore
	calls []string
}

func (d *codeSearchNameFallbackDouble) SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntitiesByName")
	return d.FakePortContentStore.SearchEntitiesByName(ctx, repoID, entityType, name, limit)
}

// codeSearchFallbackSignatureMatchPin pins the fallback member to its
// exact pin-member signature: a drifted double fails compilation here
// instead of silently testing a different shape.
var _ func(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) = (*codeSearchNameFallbackDouble)(nil).SearchEntitiesByName

// codeSearchServe mounts a code handler over the graph and content
// doubles and serves one request through the real mux, so the tests
// below prove the registered routes, not direct method calls.
func codeSearchServe(t *testing.T, handler *codequery.CodeHandler, method, path, body string, scope func(context.Context) context.Context) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if scope != nil {
		req = req.WithContext(scope(req.Context()))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func codeSearchScopedTo(repoIDs ...string) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		return auth.ContextWithAuthContext(ctx, auth.AuthContext{
			Mode:                 auth.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: repoIDs,
		})
	}
}

var codeSearchCatalog = []querycontract.RepositoryCatalogEntry{{
	ID: "repo-1", Name: "payments", RepoSlug: "acme/payments",
}}

// TestCodeSearchReadAdmissionRoutesServeSearches proves the two pinned
// routes serve their reads through the pin-half doubles: each route
// answers HTTP 200 through the real mux and records exactly the pinned
// methods its branches admit through — the graph lane, the global
// entity-name index, the candidate and fallback content reads, the
// symbol-aware read and its plain-name fallback, and the by-ID
// hydration behind graph enrichment. No subtest may record RunSingle:
// the pin keeps the single-row graph read OUT.
func TestCodeSearchReadAdmissionRoutesServeSearches(t *testing.T) {
	t.Parallel()

	t.Run("search global admits through the entity-name index", func(t *testing.T) {
		t.Parallel()
		store := &codeSearchContentDouble{namesRows: []querycontract.EntityContent{{
			EntityID: "entity-1", RepoID: "repo-1", RelativePath: "src/app.ts",
			EntityType: "Function", EntityName: "renderApp",
		}}}
		graph := &codeSearchGraphDouble{}
		handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/search",
			`{"query":"render","limit":5}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchEntityNames"}) {
			t.Fatalf("content calls = %v, want [SearchEntityNames]", store.calls)
		}
		if len(graph.calls) != 0 {
			t.Fatalf("graph calls = %v, want none: the global path must not touch the graph", graph.calls)
		}
		if !strings.Contains(rec.Body.String(), "entity-1") {
			t.Fatalf("body = %s, want the indexed entity row", rec.Body.String())
		}
	})

	t.Run("search scoped global threads the caller grant", func(t *testing.T) {
		t.Parallel()
		store := &codeSearchContentDouble{}
		graph := &codeSearchGraphDouble{}
		handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/search",
			`{"query":"render","limit":5}`, codeSearchScopedTo("repo-1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchEntityNames"}) {
			t.Fatalf("content calls = %v, want [SearchEntityNames]", store.calls)
		}
		if store.nameSearch.Scope != entity.EntityNameScopeRepositories {
			t.Errorf("entity-name scope = %q, want %q", store.nameSearch.Scope, entity.EntityNameScopeRepositories)
		}
		if !slices.Equal(store.nameSearch.RepositoryIDs, []string{"repo-1"}) {
			t.Errorf("entity-name repositories = %v, want [repo-1]", store.nameSearch.RepositoryIDs)
		}
	})

	t.Run("search repo admits the graph lane and hydrates by ID", func(t *testing.T) {
		t.Parallel()
		store := &codeSearchContentDouble{}
		store.FakePortContentStore = content.FakePortContentStore{
			Repositories: codeSearchCatalog,
			Entities: []querycontract.EntityContent{{
				EntityID: "entity-1", RepoID: "repo-1", RelativePath: "src/app.ts",
				EntityType: "Function", EntityName: "renderApp",
				StartLine: 10, EndLine: 24, Language: "typescript",
				SourceCache: "function renderApp() {}",
				Metadata:    map[string]any{"docstring": "Renders the app."},
			}},
		}
		graph := &codeSearchGraphDouble{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{
				"entity_id": "entity-1", "name": "renderApp", "repo_id": "repo-1",
				"file_path": "src/app.ts", "language": "typescript",
			}}, nil
		}}
		handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/search",
			`{"query":"renderApp","repo_id":"repo-1","limit":5}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(graph.calls, []string{"Run"}) {
			t.Fatalf("graph calls = %v, want [Run]", graph.calls)
		}
		if !slices.Contains(store.calls, "ListRepoEntitiesByIDs") {
			t.Fatalf("content calls = %v, want to contain ListRepoEntitiesByIDs: enrichment hydrates graph rows by ID", store.calls)
		}
		if !strings.Contains(rec.Body.String(), "entity-1") {
			t.Fatalf("body = %s, want the graph row", rec.Body.String())
		}
	})

	t.Run("search repo falls back to the name and content reads", func(t *testing.T) {
		t.Parallel()
		store := &codeSearchContentDouble{}
		store.FakePortContentStore = content.FakePortContentStore{Repositories: codeSearchCatalog}
		graph := &codeSearchGraphDouble{}
		handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/search",
			`{"query":"renderApp","repo_id":"repo-1","limit":5}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(graph.calls, []string{"Run"}) {
			t.Fatalf("graph calls = %v, want [Run]: an empty graph page falls back to the content index", graph.calls)
		}
		if !slices.Equal(store.calls, []string{"SearchEntitiesByName", "SearchEntityContent"}) {
			t.Fatalf("content calls = %v, want [SearchEntitiesByName SearchEntityContent]", store.calls)
		}
	})

	t.Run("search repo with language admits through candidates", func(t *testing.T) {
		t.Parallel()
		store := &codeSearchContentDouble{}
		store.FakePortContentStore = content.FakePortContentStore{
			Repositories: codeSearchCatalog,
			Entities: []querycontract.EntityContent{{
				EntityID: "entity-1", RepoID: "repo-1", RelativePath: "payments/handler.go",
				EntityType: "Function", EntityName: "HandlePayment", Language: "go",
			}},
		}
		graph := &codeSearchGraphDouble{}
		handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/search",
			`{"query":"Handle","repo_id":"repo-1","language":"go","limit":5}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchCodeCandidates"}) {
			t.Fatalf("content calls = %v, want [SearchCodeCandidates]", store.calls)
		}
	})

	t.Run("symbols admits through the symbol-aware read", func(t *testing.T) {
		t.Parallel()
		store := &codeSearchContentDouble{symbolRows: []querycontract.EntityContent{{
			EntityID: "entity-1", RepoID: "repo-1", RelativePath: "src/app.ts",
			EntityType: "Function", EntityName: "Foo",
		}}}
		graph := &codeSearchGraphDouble{}
		handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/symbols/search",
			`{"symbol":"Foo"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchSymbols"}) {
			t.Fatalf("content calls = %v, want [SearchSymbols]", store.calls)
		}
		if len(graph.calls) != 0 {
			t.Fatalf("graph calls = %v, want none: the symbol-aware read answers without the graph", graph.calls)
		}
		if !strings.Contains(rec.Body.String(), "entity-1") {
			t.Fatalf("body = %s, want the symbol row", rec.Body.String())
		}
	})

	t.Run("symbols falls back to the plain name read", func(t *testing.T) {
		t.Parallel()
		store := &codeSearchNameFallbackDouble{}
		store.FakePortContentStore = content.FakePortContentStore{Repositories: codeSearchCatalog}
		graph := &codeSearchGraphDouble{}
		handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/symbols/search",
			`{"symbol":"Foo","repo_id":"repo-1"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchEntitiesByName"}) {
			t.Fatalf("content calls = %v, want [SearchEntitiesByName]: a store without the symbol seam must fall back to the plain name read", store.calls)
		}
		if len(graph.calls) != 0 {
			t.Fatalf("graph calls = %v, want none: the name fallback answers without the graph", graph.calls)
		}
	})

	t.Run("symbols without content admits through the graph lane", func(t *testing.T) {
		t.Parallel()
		graph := &codeSearchGraphDouble{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{
				"entity_id": "entity-1", "name": "Foo", "repo_id": "repo-1",
				"file_path": "src/app.ts", "language": "typescript",
				"metadata": map[string]any{"kind": "function"},
			}}, nil
		}}
		handler := &codequery.CodeHandler{Neo4j: graph, Profile: codequery.ProfileLocalAuthoritative}
		rec := codeSearchServe(t, handler, http.MethodPost, "/api/v0/code/symbols/search",
			`{"symbol":"Foo","repo_id":"repo-1"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(graph.calls, []string{"Run"}) {
			t.Fatalf("graph calls = %v, want [Run]", graph.calls)
		}
	})
}

// TestCodeSearchReadAdmissionNeverRunsSingle asserts the OUT half of the
// code pin at the route level: across every subtest above the graph
// double never records a RunSingle call. The code search family reads
// the graph in pages only; a RunSingle here would void the pin.
func TestCodeSearchReadAdmissionNeverRunsSingle(t *testing.T) {
	t.Parallel()

	graph := &codeSearchGraphDouble{}
	store := &codeSearchContentDouble{}
	store.FakePortContentStore = content.FakePortContentStore{Repositories: codeSearchCatalog}
	handler := &codequery.CodeHandler{Neo4j: graph, Content: store, Profile: codequery.ProfileLocalAuthoritative}

	mux := http.NewServeMux()
	handler.Mount(mux)
	for _, tc := range []struct{ path, body string }{
		{"/api/v0/code/search", `{"query":"render","limit":5}`},
		{"/api/v0/code/search", `{"query":"renderApp","repo_id":"repo-1","limit":5}`},
		{"/api/v0/code/symbols/search", `{"symbol":"Foo","repo_id":"repo-1"}`},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s = %d, want %d; body = %s", tc.path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
	if slices.Contains(graph.calls, "RunSingle") {
		t.Fatalf("graph calls = %v, want no RunSingle: the code search pin keeps the single-row graph read OUT", graph.calls)
	}
}

// TestCodeSearchReadAdmissionSpecFragmentParity proves the pin's route
// list is the spec's route list and the mux's route list: every pinned
// route path appears as a fragment in the published OpenAPI code path
// fragments, and the mounted handler serves every pinned route (a
// mistyped body still reaches the handler's 400, where an unregistered
// path would fall through to the mux 404).
func TestCodeSearchReadAdmissionSpecFragmentParity(t *testing.T) {
	t.Parallel()

	fragments := map[string]string{
		"/api/v0/code/search":         code.Routes,
		"/api/v0/code/symbols/search": code.Symbols,
	}
	for _, route := range codeSearchPinnedRoutes {
		route := route
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			path := strings.TrimPrefix(route, "POST ")
			fragment := `"` + path + `":`
			if _, _, found := strings.Cut(fragments[path], fragment); !found {
				t.Errorf("OpenAPI code fragment has no %q entry: the pin, the spec, and the mux must name the same routes", path)
			}
		})
	}

	mux := http.NewServeMux()
	(&codequery.CodeHandler{Content: &codeSearchContentDouble{}}).Mount(mux)
	for _, route := range codeSearchPinnedRoutes {
		path := strings.TrimPrefix(route, "POST ")
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s with an empty body = %d, want %d: the route must be registered on the mux", path, rec.Code, http.StatusBadRequest)
		}
	}
}
