// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/runtime"
)

// stubRepositoryCatalog answers MatchRepositories from a selector map.
type stubRepositoryCatalog struct {
	matches map[string][]querycontract.RepositoryCatalogEntry
	err     error
}

func (s stubRepositoryCatalog) MatchRepositories(_ context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.matches[selector], nil
}

// stubRepositoryReindexer records each request and stamps every scope with
// requestedAt, sorted like the store.
type stubRepositoryReindexer struct {
	requestedAt time.Time
	err         error
	calls       [][]string
}

func (s *stubRepositoryReindexer) RequestRepositoryReindex(_ context.Context, scopeIDs []string) ([]runtime.RepositoryReindexRequest, error) {
	s.calls = append(s.calls, append([]string(nil), scopeIDs...))
	if s.err != nil {
		return nil, s.err
	}
	sorted := slices.Clone(scopeIDs)
	slices.Sort(sorted)
	out := make([]runtime.RepositoryReindexRequest, 0, len(sorted))
	for _, scopeID := range slices.Compact(sorted) {
		out = append(out, runtime.RepositoryReindexRequest{ScopeID: scopeID, RequestedAt: s.requestedAt})
	}
	return out, nil
}

func catalogEntry(id, scopeID string) querycontract.RepositoryCatalogEntry {
	return querycontract.RepositoryCatalogEntry{ID: id, Name: id, ScopeID: scopeID}
}

func repositoryCatalogFixture() stubRepositoryCatalog {
	payments := catalogEntry("repository:r_payments", "git-repository-scope:repository:r_payments")
	orders := catalogEntry("repository:r_orders", "git-repository-scope:repository:r_orders")
	return stubRepositoryCatalog{matches: map[string][]querycontract.RepositoryCatalogEntry{
		"payments":              {payments},
		"repository:r_payments": {payments},
		"orders":                {orders},
		"shared": {
			catalogEntry("repository:r_one", "git-repository-scope:repository:r_one"),
			catalogEntry("repository:r_two", "git-repository-scope:repository:r_two"),
		},
		"other-collector": {catalogEntry("repository:r_other", "other-scope:repository:r_other")},
		"ref-scope":       {catalogEntry("repository:r_ref", "git-repository-scope:repository:r_ref@release")},
	}}
}

// TestAdminHandler_RepositoryReindexAccepted pins the repository-scoped 202:
// selectors resolve to their git scopes, duplicates collapse by scope, the
// store is called once, and each repository comes back with its stored time.
func TestAdminHandler_RepositoryReindexAccepted(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 10, 7, 8, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	reindexer := &stubRepositoryReindexer{requestedAt: stored}
	fleet := &stubReindexRequester{}
	h := &Handler{Reindexer: fleet, Repositories: repositoryCatalogFixture(), RepositoryReindexer: reindexer}
	w := postRawReindex(h, `{"scope":"repository","repositories":["payments","orders","repository:r_payments"]}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", w.Code, w.Body.String())
	}
	if len(reindexer.calls) != 1 || len(reindexer.calls[0]) != 2 {
		t.Fatalf("RequestRepositoryReindex calls = %v, want one call with 2 deduplicated scopes", reindexer.calls)
	}
	if len(fleet.ingesters) != 0 {
		t.Fatalf("fleet RequestReindex called %d times, want 0 for a repository-scoped request", len(fleet.ingesters))
	}
	got := decodeBody(t, w)
	for key, value := range map[string]any{"status": "accepted", "ingester": "repository", "scope": "repository", "force": true} {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}
	if _, ok := got["requested_at"]; ok {
		t.Errorf("requested_at = %v, want it absent: each repository carries its own time", got["requested_at"])
	}
	repositories, _ := got["repositories"].([]any)
	want := []map[string]any{
		{"repository_id": "repository:r_orders", "scope_id": "git-repository-scope:repository:r_orders", "requested_at": "2026-10-07T12:00:00Z"},
		{"repository_id": "repository:r_payments", "scope_id": "git-repository-scope:repository:r_payments", "requested_at": "2026-10-07T12:00:00Z"},
	}
	if len(repositories) != len(want) {
		t.Fatalf("repositories = %v, want %v", repositories, want)
	}
	for i, entry := range repositories {
		row, _ := entry.(map[string]any)
		for key, value := range want[i] {
			if row[key] != value {
				t.Errorf("repositories[%d].%s = %v, want %v", i, key, row[key], value)
			}
		}
	}
	detail, _ := got["detail"].(string)
	for _, phrase := range []string{"requested_at", "full re-parse", "no separate completion status", "filesystem", "webhook"} {
		if !strings.Contains(detail, phrase) {
			t.Errorf("detail %q lacks %q", detail, phrase)
		}
	}
}

// TestAdminHandler_RepositoryReindexRejects: a repository-scoped request is
// all or nothing. Any selector that is unknown, ambiguous, or not a git
// default-branch scope fails the whole request with every bad selector named,
// and nothing is recorded.
func TestAdminHandler_RepositoryReindexRejects(t *testing.T) {
	t.Parallel()

	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("%q", fmt.Sprintf("repo-%d", i))
	}
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"repository scope without repositories", `{"scope":"repository"}`, []string{"repositories", "1 to 100"}},
		{"empty repositories", `{"scope":"repository","repositories":[]}`, []string{"1 to 100"}},
		{"too many repositories", `{"scope":"repository","repositories":[` + strings.Join(tooMany, ",") + `]}`, []string{"1 to 100"}},
		{"repositories with workspace scope", `{"scope":"workspace","repositories":["payments"]}`, []string{`scope "repository"`}},
		{"repositories with default scope", `{"repositories":["payments"]}`, []string{`scope "repository"`}},
		{"blank selector", `{"scope":"repository","repositories":["payments"," "]}`, []string{"blank"}},
		{"unknown scope", `{"scope":"tenant"}`, []string{`"workspace" or "repository"`}},
		{
			"unresolved selectors", `{"scope":"repository","repositories":["payments","missing","shared","other-collector","ref-scope"]}`,
			[]string{
				`"missing": no repository matched`, `"shared": matched multiple repositories: repository:r_one, repository:r_two`,
				`"other-collector": not a git default-branch repository`, `"ref-scope": not a git default-branch repository`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reindexer := &stubRepositoryReindexer{}
			fleet := &stubReindexRequester{}
			h := &Handler{Reindexer: fleet, Repositories: repositoryCatalogFixture(), RepositoryReindexer: reindexer}
			w := postRawReindex(h, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
			}
			detail, _ := decodeBody(t, w)["detail"].(string)
			for _, want := range tc.want {
				if !strings.Contains(detail, want) {
					t.Errorf("detail = %q, want it to mention %q", detail, want)
				}
			}
			if len(reindexer.calls) != 0 || len(fleet.ingesters) != 0 {
				t.Fatalf("recorded %v / %v, want nothing for a rejected request", reindexer.calls, fleet.ingesters)
			}
		})
	}
}

// TestAdminHandler_RepositoryReindexFailures: a catalog or store failure is a
// 500, not a 400 blaming the selector, and an unwired resolver or store is a
// 503.
func TestAdminHandler_RepositoryReindexFailures(t *testing.T) {
	t.Parallel()

	body := `{"scope":"repository","repositories":["payments"]}`
	catalogDown := &Handler{Repositories: stubRepositoryCatalog{err: errors.New("postgres down")}, RepositoryReindexer: &stubRepositoryReindexer{}}
	if w := postRawReindex(catalogDown, body); w.Code != http.StatusInternalServerError {
		t.Fatalf("catalog failure status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	storeDown := &Handler{Repositories: repositoryCatalogFixture(), RepositoryReindexer: &stubRepositoryReindexer{err: errors.New("postgres down")}}
	if w := postRawReindex(storeDown, body); w.Code != http.StatusInternalServerError {
		t.Fatalf("store failure status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	for name, h := range map[string]*Handler{
		"no resolver": {RepositoryReindexer: &stubRepositoryReindexer{}},
		"no store":    {Repositories: repositoryCatalogFixture()},
	} {
		if w := postRawReindex(h, body); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503", name, w.Code)
		}
	}
}
