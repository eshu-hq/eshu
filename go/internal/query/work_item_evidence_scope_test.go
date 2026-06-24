// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// failingWorkItemEvidenceStore proves the scoped empty-grant path never reads
// the work-item evidence store.
type failingWorkItemEvidenceStore struct {
	called bool
}

func (s *failingWorkItemEvidenceStore) ListWorkItemEvidence(
	context.Context,
	WorkItemEvidenceFilter,
) ([]WorkItemEvidenceRow, error) {
	s.called = true
	return nil, errors.New("broad work-item evidence read")
}

func TestAuthMiddlewareWithScopedTokensAllowsWorkItemEvidenceRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/work-items/evidence?work_item_key=OPS-123&limit=10", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensRejectsAdjacentWorkItemRoutes(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name   string
		method string
		target string
	}{
		// The admin work-items query stays admin-only; scoped tokens never reach it.
		{name: "admin-query", method: http.MethodPost, target: "/api/v0/admin/work-items/query"},
		// A sibling work-item sub-resource path is not the gated evidence route.
		{name: "sibling", method: http.MethodGet, target: "/api/v0/work-items/evidence/explain?limit=10"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusForbidden; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestWorkItemEvidenceScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingWorkItemEvidenceStore{}
	handler := &WorkItemHandler{Evidence: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/work-items/evidence?work_item_key=OPS-123&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	assertEmptyWorkItemEvidencePage(t, rec.Body.Bytes())
	if store.called {
		t.Fatal("evidence store was called for empty scoped grants")
	}
}

func TestWorkItemEvidenceScopedHandlerPassesGrantSet(t *testing.T) {
	t.Parallel()

	store := &recordingWorkItemEvidenceStore{
		rows: []WorkItemEvidenceRow{
			{FactID: "fact-1", FactKind: "work_item.external_link", LinkedRepositoryID: "repo://example/api"},
		},
	}
	handler := &WorkItemHandler{Evidence: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://example/api"},
		AllowedScopeIDs:      []string{"git-repository-scope:example/api"},
	}
	// The grant predicate intersects linked_repository_id with the union of
	// granted repository and scope ids.
	wantGrants := []string{"git-repository-scope:example/api", "repo://example/api"}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/work-items/evidence?work_item_key=OPS-123&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), auth))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got := store.lastFilter.AllowedRepositoryIDs; !equalScopeStringSlices(got, wantGrants) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, wantGrants)
	}
}

func TestWorkItemEvidenceScopedSharedTokenUnchanged(t *testing.T) {
	t.Parallel()

	store := &recordingWorkItemEvidenceStore{
		rows: []WorkItemEvidenceRow{
			{FactID: "fact-1", FactKind: "work_item.record", WorkItemKey: "OPS-123"},
		},
	}
	handler := &WorkItemHandler{Evidence: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/work-items/evidence?work_item_key=OPS-123&limit=10", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:      AuthModeShared,
		TenantID:  "tenant-a",
		AllScopes: true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	// Shared/admin/local pass no grant set: the store sees an empty allowlist so
	// the SQL grant predicate degrades to the unscoped all-rows branch.
	if got := store.lastFilter.AllowedRepositoryIDs; len(got) != 0 {
		t.Fatalf("AllowedRepositoryIDs = %#v, want empty for shared token", got)
	}
}

func TestWorkItemEvidenceSQLAppliesLinkedRepositoryGrantPredicate(t *testing.T) {
	t.Parallel()

	const predicate = "fact.payload->>'linked_repository_id' = ANY($9::text[])"
	if !strings.Contains(listWorkItemEvidenceQuery, predicate) {
		t.Fatalf("query missing linked_repository_id grant predicate %q:\n%s", predicate, listWorkItemEvidenceQuery)
	}
	if !strings.Contains(listWorkItemEvidenceQuery, "cardinality($9::text[]) = 0") {
		t.Fatalf("query missing empty-grant unscoped branch:\n%s", listWorkItemEvidenceQuery)
	}
	if strings.Index(listWorkItemEvidenceQuery, predicate) > strings.Index(listWorkItemEvidenceQuery, "ORDER BY") {
		t.Fatalf("grant predicate %q appears after ORDER BY:\n%s", predicate, listWorkItemEvidenceQuery)
	}
	// The LIMIT placeholder must shift past the grant array so pagination stays
	// bounded after the predicate is appended.
	if !strings.Contains(listWorkItemEvidenceQuery, "LIMIT $10") {
		t.Fatalf("LIMIT must shift to $10 after the grant array:\n%s", listWorkItemEvidenceQuery)
	}
}

// argCapturingWorkItemQueryer records the bound query args, then returns an
// error so the test never constructs a driver-backed *sql.Rows. It proves the
// grant array reaches Postgres at the linked_repository_id predicate position
// ($9) intact, so a multi-repo grant set lets ANY granted linked repository
// match (partial-grant visibility) and the LIMIT binds at $10.
type argCapturingWorkItemQueryer struct {
	args []any
}

func (q *argCapturingWorkItemQueryer) QueryContext(
	_ context.Context,
	_ string,
	args ...any,
) (*sql.Rows, error) {
	q.args = append([]any(nil), args...)
	return nil, errors.New("stop after capturing args")
}

func TestWorkItemEvidenceStoreBindsMultiRepoGrantArrayBeforeLimit(t *testing.T) {
	t.Parallel()

	queryer := &argCapturingWorkItemQueryer{}
	store := NewPostgresWorkItemEvidenceStore(queryer)

	grants := []string{"repo://example/api", "repo://example/web"}
	_, err := store.ListWorkItemEvidence(context.Background(), WorkItemEvidenceFilter{
		WorkItemKey:          "OPS-123",
		Limit:                11,
		AllowedRepositoryIDs: grants,
	})
	if err == nil {
		t.Fatal("ListWorkItemEvidence() error = nil, want capture stop error")
	}
	if len(queryer.args) != 10 {
		t.Fatalf("bound arg count = %d, want 10; args = %#v", len(queryer.args), queryer.args)
	}
	// $9 is the grant array; it must carry the full multi-repo grant set so a
	// partial grant still matches any of the work item's granted linked repos.
	gotGrants, ok := queryer.args[8].(interface{ Value() (driver.Value, error) })
	if !ok {
		t.Fatalf("arg[8] = %#v, want a pq.Array-wrapped grant slice", queryer.args[8])
	}
	value, err := gotGrants.Value()
	if err != nil {
		t.Fatalf("grant array Value() error = %v", err)
	}
	for _, repo := range grants {
		if !strings.Contains(fmt.Sprint(value), repo) {
			t.Fatalf("grant array %v missing %q", value, repo)
		}
	}
	// $10 is the bounded internal limit (caller limit + 1).
	if got, want := queryer.args[9], 11; got != want {
		t.Fatalf("arg[9] (LIMIT) = %v, want %d", got, want)
	}
}

func equalScopeStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertEmptyWorkItemEvidencePage(t *testing.T, body []byte) {
	t.Helper()

	var resp struct {
		Evidence        []json.RawMessage `json:"evidence"`
		Count           int               `json:"count"`
		Truncated       bool              `json:"truncated"`
		MissingEvidence bool              `json:"missing_evidence"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode work-item evidence response: %v; body = %s", err, string(body))
	}
	if resp.Count != 0 || len(resp.Evidence) != 0 || resp.Truncated {
		t.Fatalf("empty scoped work-item evidence page = %#v, want zero evidence", resp)
	}
	if !resp.MissingEvidence {
		t.Fatalf("missing_evidence = false, want true for empty scoped grants")
	}
}
