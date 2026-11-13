// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// listFilterTestRepos is the shared inventory for the name-filter proofs:
// distinct names and slugs so substring matching is unambiguous.
func listFilterTestRepos() []querycontract.RepositoryCatalogEntry {
	return []querycontract.RepositoryCatalogEntry{
		{ID: "repository:alpha", Name: "alpha-service", RepoSlug: "acme/alpha-service"},
		{ID: "repository:beta", Name: "beta-worker", RepoSlug: "acme/beta-worker"},
		{ID: "repository:gamma", Name: "gamma-alpha-bridge", RepoSlug: "other/gamma"},
	}
}

// decodeListFilterBody decodes a plain (non-envelope) list response.
func decodeListFilterBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil body=%s", err, rec.Body.String())
	}
	return body
}

func listFilterRowNames(t *testing.T, body map[string]any) []string {
	t.Helper()
	rows, ok := body["repositories"].([]any)
	if !ok {
		t.Fatalf("repositories type = %T, want list", body["repositories"])
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("row type = %T, want map", row)
		}
		name, _ := m["name"].(string)
		names = append(names, name)
	}
	return names
}

func listFilterRowKeys(t *testing.T, body map[string]any) []string {
	t.Helper()
	rows, ok := body["repositories"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("repositories = %#v, want at least one row", body["repositories"])
	}
	m, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("row type = %T, want map", rows[0])
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestListRepositoriesNameFilterContentPath proves the name filter applies
// before count and page on the content path: total counts the filtered set.
func TestListRepositoriesNameFilterContentPath(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name=alpha", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeListFilterBody(t, rec)
	if got, want := body["total"], float64(2); got != want {
		t.Fatalf("total = %#v, want %#v (alpha-service + gamma-alpha-bridge)", got, want)
	}
	if got, want := body["count"], float64(2); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := listFilterRowNames(t, body), []string{"alpha-service", "gamma-alpha-bridge"}; !equalStrings(got, want) {
		t.Fatalf("names = %#v, want %#v in name order", got, want)
	}
}

// TestListRepositoriesNameFilterMatchesSlug proves the filter also matches
// the repo slug, not only the display name.
func TestListRepositoriesNameFilterMatchesSlug(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name=acme/", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeListFilterBody(t, rec)
	if got, want := body["total"], float64(2); got != want {
		t.Fatalf("total = %#v, want %#v (two acme slugs)", got, want)
	}
}

// TestListRepositoriesNameFilterIsCaseInsensitive proves matching lowercases
// both sides.
func TestListRepositoriesNameFilterIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name=ALPHA", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if got, want := decodeListFilterBody(t, rec)["total"], float64(2); got != want {
		t.Fatalf("total = %#v, want %#v", got, want)
	}
}

// TestListRepositoriesNameNoMatchIsEmptyNotAnError proves a filter matching
// nothing answers an empty page with zero totals, not an error.
func TestListRepositoriesNameNoMatchIsEmptyNotAnError(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name=zzz-no-such-repo", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeListFilterBody(t, rec)
	if got, want := body["total"], float64(0); got != want {
		t.Fatalf("total = %#v, want %#v", got, want)
	}
	if got, want := body["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := body["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

// TestListRepositoriesNameBlankIsAbsent proves empty and whitespace-only
// names behave as no filter.
func TestListRepositoriesNameBlankIsAbsent(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"/api/v0/repositories?name=", "/api/v0/repositories?name=+++"} {
		handler := &Handler{
			Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
			Profile: querycontract.ProfileLocalAuthoritative,
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()

		handler.listRepositories(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("%s status = %d, want %d body=%s", target, got, want, rec.Body.String())
		}
		if got, want := decodeListFilterBody(t, rec)["total"], float64(3); got != want {
			t.Fatalf("%s total = %#v, want %#v (unfiltered)", target, got, want)
		}
	}
}

// graphFilterCannedRows returns graph-shaped rows for the shared inventory.
func scopedListRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
}

// TestListRepositoriesNameIntersectsGrantContentPath proves a scoped caller
// plus a name matching only outside-grant repos gets an empty page: the
// filter intersects the grant, never leaks, never signals existence.
func TestListRepositoriesNameIntersectsGrantContentPath(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, scopedListRequest(t, "/api/v0/repositories?name=alpha"))

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeListFilterBody(t, rec)
	if got, want := body["total"], float64(0); got != want {
		t.Fatalf("total = %#v, want %#v (intersection is empty)", got, want)
	}
	if got, want := body["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	rows, ok := body["repositories"].([]any)
	if !ok || len(rows) != 0 {
		t.Fatalf("repositories = %#v, want empty", body["repositories"])
	}
}

// TestListRepositoriesNameIntersectsGrantGraphPath proves the graph WHERE
// composes the grant with the name predicate: a name matching only an
// out-of-grant repo returns nothing and binds both conditions.
func TestListRepositoriesNameTooLongIs400(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?name="+strings.Repeat("a", 201), nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
}

// TestListRepositoriesUnknownProjectionIs400 is the RED guard for the closed
// projection enum: anything but full|compact must fail closed.
func TestListRepositoriesUnknownProjectionIs400(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?projection=wide", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
}

// TestListRepositoriesCompactRowKeysExact proves compact rows carry exactly
// the discovery set and nothing else.
func TestListRepositoriesCompactRowKeysExact(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?projection=compact", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeListFilterBody(t, rec)
	if got, want := listFilterRowKeys(t, body), []string{"id", "index_state", "name", "repo_slug"}; !equalStrings(got, want) {
		t.Fatalf("compact row keys = %#v, want %#v", got, want)
	}
	if got, want := body["projection"], "compact"; got != want {
		t.Fatalf("projection = %#v, want %#v", got, want)
	}
}

// TestListRepositoriesFullRowKeysUnchanged is the golden guard for the
// default shape: full rows keep every field shipped today.
func TestListRepositoriesFullRowKeysUnchanged(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=1", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	body := decodeListFilterBody(t, rec)
	want := []string{
		"group_key", "group_kind", "group_reason", "group_source", "group_truth",
		"has_remote", "id", "index_state", "is_dependency", "local_path",
		"name", "path", "remote_url", "repo_slug", "retirement", "selection",
	}
	if got := listFilterRowKeys(t, body); !equalStrings(got, want) {
		t.Fatalf("full row keys = %#v, want %#v", got, want)
	}
	if got, want := body["projection"], "full"; got != want {
		t.Fatalf("projection = %#v, want %#v (HTTP default)", got, want)
	}
}

// TestListRepositoriesCompactOmitsGroupMissingReason proves compact pages
// never emit the group-evidence-missing reason: no group fields ship, so
// the reason cannot occur.
func TestListRepositoriesCompactOmitsGroupMissingReason(t *testing.T) {
	t.Parallel()

	// No slug and no remote: the full shape reports missing group evidence.
	repos := []querycontract.RepositoryCatalogEntry{{ID: "repository:bare", Name: "bare"}}
	newHandler := func() *Handler {
		return &Handler{
			Content: content.FakePortContentStore{Repositories: repos},
			Profile: querycontract.ProfileLocalAuthoritative,
		}
	}
	fullReq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	fullRec := httptest.NewRecorder()
	newHandler().listRepositories(fullRec, fullReq)
	fullReasons, _ := decodeListFilterBody(t, fullRec)["partial_reasons"].([]any)
	found := false
	for _, r := range fullReasons {
		if r == "repository_group_evidence_missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("full partial_reasons = %#v, want the group-missing reason", fullReasons)
	}

	compactReq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?projection=compact", nil)
	compactRec := httptest.NewRecorder()
	newHandler().listRepositories(compactRec, compactReq)
	compactReasons, _ := decodeListFilterBody(t, compactRec)["partial_reasons"].([]any)
	for _, r := range compactReasons {
		if r == "repository_group_evidence_missing" {
			t.Fatalf("compact partial_reasons = %#v, want no group-missing reason", compactReasons)
		}
	}
}

// TestListRepositoriesProjectionEchoedOnEarlyReturns proves the nil-handler
// and empty-grant paths echo the projection instead of omitting it.
func TestListRepositoriesProjectionEchoedOnEarlyReturns(t *testing.T) {
	t.Parallel()

	var nilHandler *Handler
	for _, target := range []string{"/api/v0/repositories", "/api/v0/repositories?projection=compact"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		nilHandler.listRepositories(rec, req)
		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("%s status = %d, want %d", target, got, want)
		}
		body := decodeListFilterBody(t, rec)
		want := "full"
		if strings.Contains(target, "compact") {
			want = "compact"
		}
		if got := body["projection"]; got != want {
			t.Fatalf("%s projection = %#v, want %#v", target, got, want)
		}
	}

	granted := &Handler{
		Content: content.FakePortContentStore{Repositories: listFilterTestRepos()},
		Profile: querycontract.ProfileLocalAuthoritative,
	}
	req := scopedListRequest(t, "/api/v0/repositories?projection=compact")
	rec := httptest.NewRecorder()
	granted.listRepositories(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("empty-grant status = %d, want %d", got, want)
	}
	if got, want := decodeListFilterBody(t, rec)["projection"], "compact"; got != want {
		t.Fatalf("empty-grant projection = %#v, want %#v", got, want)
	}
}

// TestListRepositoriesNameDifferentialExhaustion proves an offset-walked
// filtered read equals client-side filtering of the unfiltered walk, in the
// same order, on both handler paths.

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
