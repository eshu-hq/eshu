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

	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/search"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// contentSearchPinnedRoutes is the exact route surface the #9669 W4 pin
// covers: one search shape per route. The handler tests below and the
// spec-fragment parity test both range over this list, so a route added
// without pin review fails the seal elsewhere and a route dropped breaks
// every subtest here.
var contentSearchPinnedRoutes = []string{
	"POST /api/v0/content/files/search",
	"POST /api/v0/content/entities/search",
	"POST /api/v0/content/dockerfile-installs/search",
	"POST /api/v0/content/php-config/search",
}

// contentSearchPinDouble is the pin-satisfying double behind the route
// tests: it embeds the shared content.FakePortContentStore double and
// implements exactly the ten pinned reads, recording every call. The
// compile-time assertion below proves it satisfies the pin, so serving
// every route through it proves the frozen shape — nothing wider — is
// enough to answer the whole search surface.
type contentSearchPinDouble struct {
	content.FakePortContentStore
	calls    []string
	files    []querycontract.FileContent
	entities []querycontract.EntityContent
}

func (d *contentSearchPinDouble) record(name string) {
	d.calls = append(d.calls, name)
}

func (d *contentSearchPinDouble) SearchFiles(_ context.Context, _ string, _ []string, _ string, _, _ int) ([]querycontract.FileContent, error) {
	d.record("SearchFiles")
	return d.files, nil
}

func (d *contentSearchPinDouble) SearchEntities(_ context.Context, _ string, _ []string, _ string, _, _ int) ([]querycontract.EntityContent, error) {
	d.record("SearchEntities")
	return d.entities, nil
}

func (d *contentSearchPinDouble) SearchFileContent(_ context.Context, _, _ string, _ int) ([]querycontract.FileContent, error) {
	d.record("SearchFileContent")
	return d.files, nil
}

func (d *contentSearchPinDouble) SearchFileContentAnyRepo(_ context.Context, _ string, _ int) ([]querycontract.FileContent, error) {
	d.record("SearchFileContentAnyRepo")
	return d.files, nil
}

func (d *contentSearchPinDouble) SearchEntityContent(_ context.Context, _, _ string, _ int) ([]querycontract.EntityContent, error) {
	d.record("SearchEntityContent")
	return d.entities, nil
}

func (d *contentSearchPinDouble) SearchEntityContentAnyRepo(_ context.Context, _ string, _ int) ([]querycontract.EntityContent, error) {
	d.record("SearchEntityContentAnyRepo")
	return d.entities, nil
}

func (d *contentSearchPinDouble) SearchFilesUnscoped(_ context.Context, _ string, _, _ int, _, _ string, _ int) (querycontract.FileSearchPage, error) {
	d.record("SearchFilesUnscoped")
	return querycontract.FileSearchPage{Files: d.files}, nil
}

func (d *contentSearchPinDouble) SearchEntitiesUnscoped(_ context.Context, _ string, _, _ int, _ querycontract.SearchCursor) (querycontract.EntitySearchPage, error) {
	d.record("SearchEntitiesUnscoped")
	return querycontract.EntitySearchPage{Entities: d.entities}, nil
}

func (d *contentSearchPinDouble) SearchDockerfileInstallEntities(_ context.Context, _ string, _ []string, _, _ string, _ int, _ string, _ int) ([]querycontract.EntityContent, error) {
	d.record("SearchDockerfileInstallEntities")
	return d.entities, nil
}

func (d *contentSearchPinDouble) SearchPHPConfigEntities(_ context.Context, _ []string, _ string, _ *querycontract.PHPConfigCursor, _ int) ([]querycontract.EntityContent, error) {
	d.record("SearchPHPConfigEntities")
	return d.entities, nil
}

var _ ContentSearchReadAdmission = (*contentSearchPinDouble)(nil)

// contentSearchFallbackDouble serves the legacy per-request search loop
// the handlers fall back to when the store does not implement the paged
// or unscoped seams. It deliberately lacks SearchFiles/SearchEntities
// and both unscoped methods so the handlers route into the fallback
// branches; each fallback member carries the exact pinned signature
// (asserted below), so the recorded calls still prove the frozen shape
// serves those branches.
type contentSearchFallbackDouble struct {
	content.FakePortContentStore
	calls    []string
	files    []querycontract.FileContent
	entities []querycontract.EntityContent
}

func (d *contentSearchFallbackDouble) SearchFileContent(_ context.Context, _, _ string, _ int) ([]querycontract.FileContent, error) {
	d.calls = append(d.calls, "SearchFileContent")
	return d.files, nil
}

func (d *contentSearchFallbackDouble) SearchFileContentAnyRepo(_ context.Context, _ string, _ int) ([]querycontract.FileContent, error) {
	d.calls = append(d.calls, "SearchFileContentAnyRepo")
	return d.files, nil
}

func (d *contentSearchFallbackDouble) SearchEntityContent(_ context.Context, _, _ string, _ int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntityContent")
	return d.entities, nil
}

func (d *contentSearchFallbackDouble) SearchEntityContentAnyRepo(_ context.Context, _ string, _ int) ([]querycontract.EntityContent, error) {
	d.calls = append(d.calls, "SearchEntityContentAnyRepo")
	return d.entities, nil
}

// contentSearchFallbackSignaturesMatchPin pins each fallback member to
// its exact pin-member signature: a drifted double fails compilation
// here instead of silently testing a different shape.
var (
	_ func(context.Context, string, string, int) ([]querycontract.FileContent, error)   = (*contentSearchFallbackDouble)(nil).SearchFileContent
	_ func(context.Context, string, int) ([]querycontract.FileContent, error)           = (*contentSearchFallbackDouble)(nil).SearchFileContentAnyRepo
	_ func(context.Context, string, string, int) ([]querycontract.EntityContent, error) = (*contentSearchFallbackDouble)(nil).SearchEntityContent
	_ func(context.Context, string, int) ([]querycontract.EntityContent, error)         = (*contentSearchFallbackDouble)(nil).SearchEntityContentAnyRepo
)

// contentSearchServe mounts a content handler over store and serves one
// request through the real mux, so the tests below prove the registered
// routes, not direct method calls.
func contentSearchServe(t *testing.T, store querycontract.ContentStore, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}).Mount(mux)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestContentSearchReadAdmissionRoutesServeSearches proves the four
// pinned routes serve their reads through the pin-satisfying double:
// each route answers HTTP 200 through the real mux and records exactly
// the pinned method its branch admits through — the paged seams for
// scoped searches, the bounded walks for unscoped ones, the fallback
// twins when the store lacks the seams, and the entity pages for the
// installer and PHP routes.
func TestContentSearchReadAdmissionRoutesServeSearches(t *testing.T) {
	t.Parallel()

	fileFixture := []querycontract.FileContent{{
		RepoID: "repo-1", RelativePath: "src/app.go", CommitSHA: "abc123",
		Content: "package app", ContentHash: "h-1", LineCount: 1, Language: "go",
	}}
	entityFixture := []querycontract.EntityContent{{
		EntityID: "entity-1", RepoID: "repo-1", RelativePath: "src/app.ts",
		EntityType: "Function", EntityName: "renderApp",
		StartLine: 10, EndLine: 24, Language: "typescript",
		SourceCache: "function renderApp() {}",
		Metadata:    map[string]any{"docstring": "Renders the app."},
	}}

	t.Run("files/search scoped admits through the paged seam", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchPinDouble{files: fileFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/files/search",
			`{"repo_id":"repo-1","query":"package","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchFiles"}) {
			t.Fatalf("calls = %v, want [SearchFiles]: the scoped file route must admit through the paged seam only", store.calls)
		}
		if !strings.Contains(rec.Body.String(), "src/app.go") {
			t.Fatalf("body = %s, want the fixture file row", rec.Body.String())
		}
	})

	t.Run("files/search scoped falls back to the single-repo read", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchFallbackDouble{files: fileFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/files/search",
			`{"repo_id":"repo-1","query":"package","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchFileContent"}) {
			t.Fatalf("calls = %v, want [SearchFileContent]: a store without the paged seam must fall back to the single-repo read", store.calls)
		}
	})

	t.Run("files/search unscoped admits through the bounded walk", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchPinDouble{files: fileFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/files/search",
			`{"query":"package","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchFilesUnscoped"}) {
			t.Fatalf("calls = %v, want [SearchFilesUnscoped]: the unscoped file route must admit through the bounded walk only", store.calls)
		}
	})

	t.Run("files/search unscoped falls back to the corpus-wide read", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchFallbackDouble{files: fileFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/files/search",
			`{"query":"package","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchFileContentAnyRepo"}) {
			t.Fatalf("calls = %v, want [SearchFileContentAnyRepo]: a store without the seams must fall back to the corpus-wide read", store.calls)
		}
	})

	t.Run("entities/search scoped admits through the paged seam", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchPinDouble{entities: entityFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/entities/search",
			`{"repo_id":"repo-1","query":"render","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchEntities"}) {
			t.Fatalf("calls = %v, want [SearchEntities]: the scoped entity route must admit through the paged seam only", store.calls)
		}
		if !strings.Contains(rec.Body.String(), "renderApp") {
			t.Fatalf("body = %s, want the fixture entity row", rec.Body.String())
		}
	})

	t.Run("entities/search scoped falls back to the single-repo read", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchFallbackDouble{entities: entityFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/entities/search",
			`{"repo_id":"repo-1","query":"render","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchEntityContent"}) {
			t.Fatalf("calls = %v, want [SearchEntityContent]: a store without the paged seam must fall back to the single-repo read", store.calls)
		}
	})

	t.Run("entities/search unscoped admits through the bounded walk", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchPinDouble{entities: entityFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/entities/search",
			`{"query":"render","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchEntitiesUnscoped"}) {
			t.Fatalf("calls = %v, want [SearchEntitiesUnscoped]: the unscoped entity route must admit through the bounded walk only", store.calls)
		}
	})

	t.Run("entities/search unscoped falls back to the corpus-wide read", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchFallbackDouble{entities: entityFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/entities/search",
			`{"query":"render","limit":10}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchEntityContentAnyRepo"}) {
			t.Fatalf("calls = %v, want [SearchEntityContentAnyRepo]: a store without the seams must fall back to the corpus-wide read", store.calls)
		}
	})

	t.Run("dockerfile-installs/search admits through the install page", func(t *testing.T) {
		t.Parallel()
		install := []querycontract.EntityContent{{
			EntityID: "entity-9", RepoID: "repo-1", RelativePath: "Dockerfile",
			EntityType: "DockerfileInstall", EntityName: "curl",
			StartLine: 3, EndLine: 3,
			Metadata: map[string]any{"manager": "apt", "category": "package", "stage": "build"},
		}}
		store := &contentSearchPinDouble{entities: install}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/dockerfile-installs/search",
			`{"package":"curl"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchDockerfileInstallEntities"}) {
			t.Fatalf("calls = %v, want [SearchDockerfileInstallEntities]", store.calls)
		}
		if !strings.Contains(rec.Body.String(), "curl") {
			t.Fatalf("body = %s, want the fixture install row", rec.Body.String())
		}
	})

	t.Run("php-config/search admits through the PHP page", func(t *testing.T) {
		t.Parallel()
		store := &contentSearchPinDouble{entities: entityFixture}
		rec := contentSearchServe(t, store, http.MethodPost, "/api/v0/content/php-config/search",
			`{"directive":"display_errors"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !slices.Equal(store.calls, []string{"SearchPHPConfigEntities"}) {
			t.Fatalf("calls = %v, want [SearchPHPConfigEntities]", store.calls)
		}
		if !strings.Contains(rec.Body.String(), "renderApp") {
			t.Fatalf("body = %s, want the fixture entity row", rec.Body.String())
		}
	})
}

// TestContentSearchReadAdmissionSpecFragmentParity proves the pin's
// route list is the spec's route list and the mux's route list: every
// pinned route path appears as a fragment in the published OpenAPI
// content path fragment, and the mounted handler serves every pinned
// route (a mistyped body still reaches the handler's 400, where an
// unregistered path would fall through to the mux 404).
func TestContentSearchReadAdmissionSpecFragmentParity(t *testing.T) {
	t.Parallel()

	for _, route := range contentSearchPinnedRoutes {
		route := route
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			path := strings.TrimPrefix(route, "POST ")
			fragment := `"` + path + `":`
			if _, _, found := strings.Cut(search.Content, fragment); !found {
				t.Errorf("OpenAPI content fragment has no %q entry: the pin, the spec, and the mux must name the same routes", path)
			}
		})
	}

	mux := http.NewServeMux()
	(&ContentHandler{Content: &contentSearchPinDouble{}}).Mount(mux)
	for _, route := range contentSearchPinnedRoutes {
		path := strings.TrimPrefix(route, "POST ")
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s with an empty body = %d, want %d: the route must be registered on the mux", path, rec.Code, http.StatusBadRequest)
		}
	}
}
