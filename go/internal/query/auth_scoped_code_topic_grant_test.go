// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// #5167 code-family batch 1, step 1: two-tenant grant proof for
// POST /api/v0/code/topics/investigate.
//
// The route's SQL already knew how to bind a grant -- codeTopicFilters
// (content_reader_code_topic.go) emits `repo_id = ANY($n)` from
// req.AllowedRepositoryIDs -- but only POST /api/v0/impact/change-surface
// ever populated that field (impact_change_surface_code.go). A scoped caller
// who omitted repo_id ran the topic search across the whole content-entity
// corpus.
//
// Both assertions are mutation-sensitive by construction:
//   - codeTopicGrantContentStore mirrors codeTopicFilters' real contract: it
//     applies AllowedRepositoryIDs when the list is non-empty and returns
//     everything when it is empty, exactly as the shipped SQL does. Drop the
//     handler's binding and the field arrives empty, the other tenant's row
//     comes back, and the "never contains the other tenant's repo id"
//     assertion fails.
//   - Drop the access.Empty() short-circuit and the empty-grant caller reaches
//     the store with an empty AllowedRepositoryIDs list -- which the SQL reads
//     as "unrestricted" -- so queried flips true and the whole corpus is
//     returned.
//
// The two ids are canonical repository ids (the repo:// form
// queryselector.LooksCanonicalRepositoryID recognises), so a route that takes
// a repository selector resolves them through the grant rather than through a
// catalog or graph lookup the fakes do not implement.
const (
	codeGrantGrantedRepo = "repo://tenant-a/granted-service"
	codeGrantOtherRepo   = "repo://tenant-b/other-service"
)

type codeTopicGrantContentStore struct {
	fakePortContentStore
	rows     []codeTopicEvidenceRow
	requests []codeTopicInvestigationRequest
	queried  bool
}

// InvestigateCodeTopic mirrors codeTopicFilters' grant branch: a non-empty
// AllowedRepositoryIDs list restricts the scan, an empty one does not. Keeping
// the fake faithful to the shipped SQL is what makes the leak assertion fail
// when the handler stops binding the grant.
func (s *codeTopicGrantContentStore) InvestigateCodeTopic(
	_ context.Context,
	req codeTopicInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	s.queried = true
	s.requests = append(s.requests, req)
	allowed := make(map[string]struct{}, len(req.AllowedRepositoryIDs))
	for _, id := range req.AllowedRepositoryIDs {
		allowed[id] = struct{}{}
	}
	rows := make([]codeTopicEvidenceRow, 0, len(s.rows))
	for _, row := range s.rows {
		if repoID := strings.TrimSpace(req.RepoID); repoID != "" && row.RepoID != repoID {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[row.RepoID]; !ok {
				continue
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func codeTopicGrantRows() []codeTopicEvidenceRow {
	return []codeTopicEvidenceRow{
		{
			SourceKind:   "entity",
			RepoID:       codeGrantGrantedRepo,
			RelativePath: "internal/auth/session.go",
			EntityID:     "entity-granted",
			EntityName:   "RefreshSession",
			EntityType:   "Function",
			Language:     "go",
			MatchedTerms: []string{"session"},
			Score:        2,
		},
		{
			SourceKind:   "entity",
			RepoID:       codeGrantOtherRepo,
			RelativePath: "internal/auth/session.go",
			EntityID:     "entity-other",
			EntityName:   "RefreshSession",
			EntityType:   "Function",
			Language:     "go",
			MatchedTerms: []string{"session"},
			Score:        2,
		},
	}
}

// codeGrantScopedAuthContext builds the scoped-token AuthContext the whole
// #5167 code-family batch-1 proof set shares: tenant-a, granted exactly the
// repository ids passed in (nil for the empty-grant fail-closed case).
func codeGrantScopedAuthContext(allowedRepositoryIDs []string) AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
}

// newCodeGrantRouteRequest builds an envelope-accepting POST request for one
// code route, carrying auth as the request's AuthContext when non-nil (nil
// means an unscoped shared-key caller).
func newCodeGrantRouteRequest(t *testing.T, path string, body map[string]any, auth *AuthContext) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal(body) error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Accept", EnvelopeMIMEType)
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	return req
}

func TestCodeTopicInvestigationFiltersByRepositoryGrant(t *testing.T) {
	t.Parallel()

	store := &codeTopicGrantContentStore{rows: codeTopicGrantRows()}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	req := newCodeGrantRouteRequest(t, "/api/v0/code/topics/investigate", map[string]any{"topic": "session refresh"}, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(store.requests) != 1 {
		t.Fatalf("store.requests = %d, want 1", len(store.requests))
	}
	if got := store.requests[0].AllowedRepositoryIDs; len(got) != 1 || got[0] != codeGrantGrantedRepo {
		t.Fatalf("AllowedRepositoryIDs = %#v, want [%q] bound into the content query", got, codeGrantGrantedRepo)
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedRepo) {
		t.Fatalf("response missing the granted repository %q: %s", codeGrantGrantedRepo, body)
	}
	if strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("response leaked the out-of-grant repository %q: %s", codeGrantOtherRepo, body)
	}
}

func TestCodeTopicInvestigationEmptyGrantSkipsTheContentRead(t *testing.T) {
	t.Parallel()

	store := &codeTopicGrantContentStore{rows: codeTopicGrantRows()}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext(nil)
	req := newCodeGrantRouteRequest(t, "/api/v0/code/topics/investigate", map[string]any{"topic": "session refresh"}, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.queried {
		t.Fatal("queried = true, want false -- an empty scoped grant must skip the content read entirely, not query then filter to empty")
	}
	body := rec.Body.String()
	if strings.Contains(body, codeGrantGrantedRepo) || strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("response leaked evidence for an empty-grant caller: %s", body)
	}
}

func TestCodeTopicInvestigationSharedKeyReadIsUnchanged(t *testing.T) {
	t.Parallel()

	store := &codeTopicGrantContentStore{rows: codeTopicGrantRows()}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newCodeGrantRouteRequest(t, "/api/v0/code/topics/investigate", map[string]any{"topic": "session refresh"}, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(store.requests) != 1 {
		t.Fatalf("store.requests = %d, want 1", len(store.requests))
	}
	if got := store.requests[0].AllowedRepositoryIDs; len(got) != 0 {
		t.Fatalf("AllowedRepositoryIDs = %#v, want empty for an unscoped caller -- the shared-key read must stay unrestricted", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, codeGrantGrantedRepo) || !strings.Contains(body, codeGrantOtherRepo) {
		t.Fatalf("unscoped response lost rows: %s", body)
	}
}

// TestCodeTopicFiltersBindTheGrantInTheShippedSQL is the guard the handler
// tests above cannot be. They drive a fake store, so the SQL text is never
// built and never executed: delete codeTopicFilters' grant branch and those
// tests still pass. This asserts the predicate against the shipped builder
// itself -- the same reason
// TestFreshnessGrantPredicatesArePresentInTheShippedSQL exists
// (go/internal/storage/postgres/generation_lifecycle_grant_test.go).
func TestCodeTopicFiltersBindTheGrantInTheShippedSQL(t *testing.T) {
	t.Parallel()

	filters, args, _ := codeTopicFilters(codeTopicInvestigationRequest{
		AllowedRepositoryIDs: []string{codeGrantGrantedRepo},
	})
	if !slices.Contains(filters, "repo_id = ANY($1)") {
		t.Fatalf("codeTopicFilters() = %#v, want a repo_id = ANY($1) grant predicate; without it a scoped caller's grant is resolved but never applied", filters)
	}
	assertBoundRepositoryGrantArray(t, args, []string{codeGrantGrantedRepo})

	unscoped, _, _ := codeTopicFilters(codeTopicInvestigationRequest{})
	for _, filter := range unscoped {
		if strings.Contains(filter, "repo_id = ANY(") {
			t.Fatalf("codeTopicFilters() = %#v, want no grant predicate for an unscoped caller", unscoped)
		}
	}
}

// assertBoundRepositoryGrantArray proves the grant predicate's placeholder is
// actually bound to the caller's id list, not left dangling: a predicate whose
// parameter never arrives fails at execution, and a predicate bound to the
// wrong list silently widens the scan.
func assertBoundRepositoryGrantArray(t *testing.T, args []any, want []string) {
	t.Helper()
	for _, arg := range args {
		bound, ok := arg.(*pgarray.StringArray)
		if !ok {
			continue
		}
		if got := []string(*bound); slices.Equal(got, want) {
			return
		}
		t.Fatalf("bound grant array = %#v, want %#v", []string(*bound), want)
	}
	t.Fatalf("args = %#v, want one bound *pgarray.StringArray carrying %#v", args, want)
}
