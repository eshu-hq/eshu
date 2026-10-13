// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// unscopedEntitySearchStore serves the bounded unscoped entity seam from a
// canned page and records the arguments the handler passed.
type unscopedEntitySearchStore struct {
	content.FakePortContentStore
	page   querycontract.EntitySearchPage
	err    error
	calls  int
	gotArg struct {
		pattern string
		limit   int
		offset  int
		cursor  querycontract.SearchCursor
	}
}

func (s *unscopedEntitySearchStore) SearchEntitiesUnscoped(
	_ context.Context, pattern string, limit, offset int, cursor querycontract.SearchCursor,
) (querycontract.EntitySearchPage, error) {
	s.calls++
	s.gotArg.pattern, s.gotArg.limit, s.gotArg.offset = pattern, limit, offset
	s.gotArg.cursor = cursor
	return s.page, s.err
}

type entitySearchEnvelope struct {
	Data struct {
		Results   []querycontract.EntityContent `json:"results"`
		Count     int                           `json:"count"`
		Truncated bool                          `json:"truncated"`
		Partial   *querycontract.SearchPartial  `json:"partial"`
	} `json:"data"`
	Truth *querycontract.TruthEnvelope `json:"truth"`
}

func postEntitySearch(t *testing.T, h *ContentHandler, ctx context.Context, body string) (*httptest.ResponseRecorder, entitySearchEnvelope) {
	t.Helper()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/entities/search", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var env entitySearchEnvelope
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v; body = %s", err, rec.Body.String())
		}
	}
	return rec, env
}

func TestSearchEntitiesUnscopedExactPageKeepsTheDerivedEnvelope(t *testing.T) {
	t.Parallel()

	store := &unscopedEntitySearchStore{page: querycontract.EntitySearchPage{
		Entities: []querycontract.EntityContent{{EntityID: "entity-1", RepoID: "r1", RelativePath: "a.go"}},
		More:     true,
	}}
	h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}

	rec, env := postEntitySearch(t, h, context.Background(), `{"query":"render","limit":1,"offset":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.gotArg.pattern != "render" || store.gotArg.limit != 1 || store.gotArg.offset != 3 {
		t.Fatalf("store args = %+v, want pattern render, limit 1, offset 3 (the store reads its own look-ahead row)", store.gotArg)
	}
	if !env.Data.Truncated || env.Data.Partial != nil {
		t.Fatalf("truncated=%v partial=%+v, want truncated by the next row and no partial", env.Data.Truncated, env.Data.Partial)
	}
	if env.Truth.Level != querycontract.TruthLevelDerived {
		t.Fatalf("truth level = %q, want derived", env.Truth.Level)
	}
}

// A budget-cut entity search is HTTP 200 with the explicit partial marker:
// truncated true, truth level partial, the reason, and the four-part resume
// cursor. It is never an error and never truncated false.
func TestSearchEntitiesUnscopedPartialIsExplicit(t *testing.T) {
	t.Parallel()

	partial := &querycontract.SearchPartial{
		Reason:             querycontract.SearchPartialCandidateBudgetExceeded,
		RowsScannedInOrder: 7700,
		RowsMatched:        1,
		Cursor:             querycontract.SearchCursor{RepoID: "r9", RelativePath: "z.go", StartLine: 12, EntityID: "entity-9"},
		BudgetMS:           800,
		ElapsedMS:          790,
		Hint:               querycontract.SearchPartialHint,
	}
	store := &unscopedEntitySearchStore{page: querycontract.EntitySearchPage{
		Entities: []querycontract.EntityContent{{EntityID: "entity-1", RepoID: "r1", RelativePath: "a.go"}},
		Partial:  partial,
	}}
	h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}

	rec, env := postEntitySearch(t, h, context.Background(), `{"query":"render"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if !env.Data.Truncated {
		t.Fatal("data.truncated = false on a partial result")
	}
	if env.Data.Partial == nil || *env.Data.Partial != *partial {
		t.Fatalf("data.partial = %+v, want %+v", env.Data.Partial, partial)
	}
	if env.Truth.Level != querycontract.TruthLevelPartial || !env.Truth.Truncated || env.Truth.Reason != partial.Reason {
		t.Fatalf("truth = %+v, want level partial, truncated, reason %q", env.Truth, partial.Reason)
	}
	if env.Data.Count != 1 {
		t.Fatalf("count = %d, want the rows found so far", env.Data.Count)
	}
}

func TestSearchEntitiesUnscopedPassesRequestCursor(t *testing.T) {
	t.Parallel()

	store := &unscopedEntitySearchStore{}
	h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}

	rec, _ := postEntitySearch(t, h, context.Background(),
		`{"query":"render","cursor":{"repo_id":"r9","relative_path":"z.go","start_line":12,"entity_id":"entity-9"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if want := (querycontract.SearchCursor{RepoID: "r9", RelativePath: "z.go", StartLine: 12, EntityID: "entity-9"}); store.gotArg.cursor != want {
		t.Fatalf("cursor = %+v, want %+v", store.gotArg.cursor, want)
	}
}

// Scoped entity searches keep their existing shape: they never reach the
// unscoped seam, and a cursor on them is refused instead of silently ignored.
func TestSearchEntitiesScopedRequestsDoNotUseUnscopedSeam(t *testing.T) {
	t.Parallel()

	store := &unscopedEntitySearchStore{}
	h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}
	rec, _ := postEntitySearch(t, h, context.Background(),
		`{"query":"render","repo_id":"repository:r_ok","cursor":{"repo_id":"r9","relative_path":"z.go","start_line":3,"entity_id":"entity-9"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("scoped request with cursor: status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errCursorNeedsUnscopedSearch.Error()) {
		t.Fatalf("body = %s, want the scoped-search refusal", rec.Body.String())
	}
	if store.calls != 0 {
		t.Fatalf("unscoped seam called %d times for a scoped request", store.calls)
	}
}

// A scoped token without a repository filter searches its authorized
// repositories, so it is not an unscoped search; an empty grant short-circuits.
func TestSearchEntitiesScopedTokenNeverUsesUnscopedSeam(t *testing.T) {
	t.Parallel()

	store := &unscopedEntitySearchStore{}
	h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}
	ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
	})
	postEntitySearch(t, h, ctx, `{"query":"render"}`)
	if store.calls != 0 {
		t.Fatalf("unscoped seam called %d times for a scoped token", store.calls)
	}
}

func TestSearchEntitiesUnscopedReadinessFailureStaysUnavailable(t *testing.T) {
	t.Parallel()

	store := &unscopedEntitySearchStore{err: querycontract.ErrContentSubstringIndexesNotReady}
	h := &ContentHandler{Content: store, Profile: querycontract.ProfileLocalAuthoritative}
	rec, _ := postEntitySearch(t, h, context.Background(), `{"query":"render"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}
