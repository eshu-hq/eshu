// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/openapi/paths/search"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// contentPointPinnedRoutes is the exact route surface the #9669 D13
// Wave-2 pin covers: one point read per route. The handler tests below
// and the spec-fragment parity test both range over this list, so a
// route added without pin review fails the seal elsewhere and a route
// dropped breaks every subtest here.
var contentPointPinnedRoutes = []string{
	"POST /api/v0/content/files/read",
	"POST /api/v0/content/files/lines",
	"POST /api/v0/content/entities/read",
}

// contentPointRouteStore is the shared-double store behind the route
// tests: it embeds the shared content.FakePortContentStore double and
// overrides only the reads the double answers with nothing. An err
// fails every pinned read for the fixed-message proof; the fixtures
// serve the line-range and entity reads the double reports as absent.
// It is a minimal local override in the sense of contentread/AGENTS.md:
// the behavior lives in the shared double, this type only steers it.
type contentPointRouteStore struct {
	content.FakePortContentStore
	err           error
	linesFixture  *querycontract.FileContent
	entityFixture *querycontract.EntityContent
}

func (s contentPointRouteStore) GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.FakePortContentStore.GetFileContent(ctx, repoID, relativePath)
}

func (s contentPointRouteStore) GetFileLines(ctx context.Context, repoID, relativePath string, startLine, endLine int) (*querycontract.FileContent, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.linesFixture != nil {
		out := *s.linesFixture
		return &out, nil
	}
	return s.FakePortContentStore.GetFileLines(ctx, repoID, relativePath, startLine, endLine)
}

func (s contentPointRouteStore) GetEntityContent(ctx context.Context, entityID string) (*querycontract.EntityContent, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.entityFixture != nil {
		out := *s.entityFixture
		return &out, nil
	}
	return s.FakePortContentStore.GetEntityContent(ctx, entityID)
}

// contentPointServe mounts a handler over store and serves one request
// through the real mux, so the tests below prove the registered routes,
// not direct method calls.
func contentPointServe(t *testing.T, store contentPointRouteStore, method, path, body string, scope func(context.Context) context.Context) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}).Mount(mux)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if scope != nil {
		req = req.WithContext(scope(req.Context()))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// contentPointDecodeData decodes one plain (non-envelope) success body
// into out. The requests below send no envelope Accept header, so the
// handler writes the wire object directly.
func contentPointDecodeData(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode data body %s: %v", rec.Body.String(), err)
	}
}

func contentPointScopedTo(repoIDs ...string) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		return auth.ContextWithAuthContext(ctx, auth.AuthContext{
			Mode:                 auth.AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: repoIDs,
		})
	}
}

// TestContentPointReadAdmissionRoutesServeReads proves the three pinned
// routes serve their reads through the shared handler double: the
// file read answers the full wire object, the line-range and entity
// reads answer their fixtures, the grant checks fail closed (an
// out-of-grant file read 404s at the selector, an empty-grant entity
// read 404s before touching the store), and every pinned read answers
// its fixed failure message — never backend text — through the shared
// failure harness.
func TestContentPointReadAdmissionRoutesServeReads(t *testing.T) {
	t.Parallel()

	t.Run("files/read serves the shared-double row", func(t *testing.T) {
		t.Parallel()
		fixture := querycontract.FileContent{
			RepoID: "repo-1", RelativePath: "src/app.go", CommitSHA: "abc123",
			Content: "package app\n", ContentHash: "h-1", LineCount: 2, Language: "go",
		}
		store := contentPointRouteStore{
			FakePortContentStore: content.FakePortContentStore{RepoFiles: []querycontract.FileContent{fixture}},
		}
		rec := contentPointServe(t, store, http.MethodPost, "/api/v0/content/files/read",
			`{"repo_id":"repo-1","relative_path":"src/app.go"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got querycontract.FileContent
		contentPointDecodeData(t, rec, &got)
		if want, gotBytes := marshalContentPointWire(t, fixture), marshalContentPointWire(t, got); want != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", want, gotBytes)
		}
	})

	t.Run("files/read denies the out-of-grant repo", func(t *testing.T) {
		t.Parallel()
		store := contentPointRouteStore{
			FakePortContentStore: content.FakePortContentStore{RepoFiles: []querycontract.FileContent{
				{RepoID: "repo-1", RelativePath: "src/app.go", Content: "package app\n", ContentHash: "h-1", LineCount: 2},
			}},
		}
		rec := contentPointServe(t, store, http.MethodPost, "/api/v0/content/files/read",
			`{"repo_id":"repo-1","relative_path":"src/app.go"}`, contentPointScopedTo("repo-2"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})

	t.Run("files/lines serves the line-range fixture", func(t *testing.T) {
		t.Parallel()
		fixture := querycontract.FileContent{
			RepoID: "repo-1", RelativePath: "src/app.go",
			Content: "l2\nl3", ContentHash: "h-1", LineCount: 2, Language: "go",
		}
		store := contentPointRouteStore{linesFixture: &fixture}
		rec := contentPointServe(t, store, http.MethodPost, "/api/v0/content/files/lines",
			`{"repo_id":"repo-1","relative_path":"src/app.go","start_line":2,"end_line":3}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got querycontract.FileContent
		contentPointDecodeData(t, rec, &got)
		if want, gotBytes := marshalContentPointWire(t, fixture), marshalContentPointWire(t, got); want != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", want, gotBytes)
		}
	})

	t.Run("entities/read serves the fixture entity", func(t *testing.T) {
		t.Parallel()
		fixture := querycontract.EntityContent{
			EntityID: "entity-1", RepoID: "repo-1", RelativePath: "src/app.ts",
			EntityType: "Function", EntityName: "renderApp",
			StartLine: 10, EndLine: 24, Language: "typescript",
			SourceCache: "function renderApp() {}",
			Metadata:    map[string]any{"docstring": "Renders the app."},
		}
		store := contentPointRouteStore{entityFixture: &fixture}
		rec := contentPointServe(t, store, http.MethodPost, "/api/v0/content/entities/read",
			`{"entity_id":"entity-1"}`, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got querycontract.EntityContent
		contentPointDecodeData(t, rec, &got)
		if want, gotBytes := marshalContentPointWire(t, fixture), marshalContentPointWire(t, got); want != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", want, gotBytes)
		}
	})

	t.Run("entities/read fails closed on an empty grant", func(t *testing.T) {
		t.Parallel()
		store := contentPointRouteStore{entityFixture: &querycontract.EntityContent{EntityID: "entity-1"}}
		rec := contentPointServe(t, store, http.MethodPost, "/api/v0/content/entities/read",
			`{"entity_id":"entity-1"}`, contentPointScopedTo())
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "entity not found") {
			t.Fatalf("body = %s, want the fixed entity-not-found message", rec.Body.String())
		}
	})

	t.Run("fixed-message fail-closed", func(t *testing.T) {
		route := func(name, path, body, message string) failure.Route {
			return failure.Route{
				Name: name, Method: http.MethodPost, Path: path, Body: body, Message: message,
				Handler: func(err error) failure.Mounter {
					return &ContentHandler{Content: contentPointRouteStore{err: err}}
				},
			}
		}
		failure.Run(t, failure.Suite{Routes: []failure.Route{
			route("file read", "/api/v0/content/files/read", `{"repo_id":"repo-1","relative_path":"src/app.go"}`, "content file read failed"),
			route("file lines", "/api/v0/content/files/lines", `{"repo_id":"repo-1","relative_path":"src/app.go","start_line":1,"end_line":2}`, "content file read failed"),
			route("entity read", "/api/v0/content/entities/read", `{"entity_id":"entity-1"}`, "content entity read failed"),
		}})
	})
}

// TestContentPointReadAdmissionSpecFragmentParity proves the pin's
// route list is the spec's route list and the mux's route list: every
// pinned route path appears as a fragment in the published OpenAPI
// content path fragment, and the mounted handler serves every pinned
// route (a mistyped body still reaches the handler's 400, where an
// unregistered path would fall through to the mux 404).
func TestContentPointReadAdmissionSpecFragmentParity(t *testing.T) {
	t.Parallel()

	for _, route := range contentPointPinnedRoutes {
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
	(&ContentHandler{Content: contentPointRouteStore{}}).Mount(mux)
	for _, route := range contentPointPinnedRoutes {
		path := strings.TrimPrefix(route, "POST ")
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s with an empty body = %d, want %d: the route must be registered on the mux", path, rec.Code, http.StatusBadRequest)
		}
	}
}
