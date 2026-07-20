// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestListRepositoriesByLanguageReturnsCountAndBoundedRows(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 23, 14, 30, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			languageRepos: []RepositoryLanguageRepository{
				{
					Repository: RepositoryCatalogEntry{ID: "repository:web", Name: "web", RepoSlug: "acme/web", HasRemote: true},
					Languages:  []RepositoryLanguageCount{{Language: "typescript", FileCount: 10}, {Language: "tsx", FileCount: 2}},
					FileCount:  12,
					IndexedAt:  indexedAt,
				},
				{
					Repository: RepositoryCatalogEntry{ID: "repository:api", Name: "api", RepoSlug: "acme/api", HasRemote: true},
					Languages:  []RepositoryLanguageCount{{Language: "typescript", FileCount: 3}},
					FileCount:  3,
					IndexedAt:  indexedAt.Add(-time.Minute),
				},
			},
			languageCounts: map[string]RepositoryLanguageAggregate{
				"typescript,tsx": {RepositoryCount: 2, FileCount: 15, LastIndexedAt: indexedAt},
			},
		},
		Profile: ProfileProduction,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/by-language?language=typescript&limit=1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositoriesByLanguage(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["language"], "typescript"; got != want {
		t.Fatalf("language = %#v, want %#v", got, want)
	}
	if got, want := data["repository_count"], float64(2); got != want {
		t.Fatalf("repository_count = %#v, want %#v", got, want)
	}
	if got, want := data["file_count"], float64(15); got != want {
		t.Fatalf("file_count = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	repositories := data["repositories"].([]any)
	if got, want := len(repositories), 1; got != want {
		t.Fatalf("len(repositories) = %d, want %d", got, want)
	}
	repo := repositories[0].(map[string]any)
	if got, want := repo["repo_slug"], "acme/web"; got != want {
		t.Fatalf("repo_slug = %#v, want %#v", got, want)
	}
	if got, want := repo["file_count"], float64(12); got != want {
		t.Fatalf("repo file_count = %#v, want %#v", got, want)
	}
	if envelope.Truth == nil || envelope.Truth.Basis != TruthBasisContentIndex {
		t.Fatalf("truth = %#v, want content-index truth", envelope.Truth)
	}
}

// TestListRepositoriesByLanguageScopedEmptyGrantReturnsEmptyWithoutQuery is
// the #5167 counterpart to the cloud-inventory/kubernetes/observability-coverage
// empty-grant precedents: a scoped caller with no granted repository or
// ingestion scope must never reach Postgres.
func TestListRepositoriesByLanguageScopedEmptyGrantReturnsEmptyWithoutQuery(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, nil)
	handler := &RepositoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/by-language?language=typescript&limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a"}))
	rec := httptest.NewRecorder()

	handler.listRepositoriesByLanguage(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(recorder.queries) != 0 {
		t.Fatalf("Postgres received %d queries, want 0 for an empty-grant scoped caller", len(recorder.queries))
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["repository_count"], float64(0); got != want {
		t.Fatalf("repository_count = %#v, want %#v", got, want)
	}
}

// TestListRepositoriesByLanguageScopedGrantHitsRealStoreAndReturnsRowData
// proves the #5167 fix against the ACTUAL production backend (ContentReader
// over a real *sql.DB): a scoped caller with a matching grant reaches
// Postgres, both dispatched queries (count, then list) carry the
// access-scoping predicate with the caller's granted repo id bound as an arg,
// and the response surfaces the real row data the fake driver returned.
func TestListRepositoriesByLanguageScopedGrantHitsRealStoreAndReturnsRowData(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC)
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"repository_count", "file_count", "last_indexed_at"},
			rows:    [][]driver.Value{{int64(1), int64(7), indexedAt}},
		},
		{
			columns: []string{
				"repo_id", "name", "path", "local_path", "remote_url", "repo_slug", "has_remote",
				"language", "file_count", "last_indexed_at",
			},
			rows: [][]driver.Value{
				{"repository:tenant-a-web", "web", "/src/web", "/src/web", "https://example.test/web", "tenant-a/web", true, "typescript", int64(7), indexedAt},
			},
		},
	})
	handler := &RepositoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/by-language?language=typescript&limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repository:tenant-a-web"},
	}))
	rec := httptest.NewRecorder()

	handler.listRepositoriesByLanguage(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(recorder.queries), 2; got != want {
		t.Fatalf("Postgres received %d queries, want exactly %d", got, want)
	}
	for i, dispatched := range recorder.queries {
		if !strings.Contains(dispatched, "repo_id = ANY(") {
			t.Fatalf("query[%d] missing #5167 access-scoping predicate:\n%s", i, dispatched)
		}
		found := false
		for _, arg := range recorder.args[i] {
			if s := fmt.Sprintf("%v", arg); strings.Contains(s, "repository:tenant-a-web") {
				found = true
			}
		}
		if !found {
			t.Fatalf("query[%d] bound args %#v did not include the granted repository id", i, recorder.args[i])
		}
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	repositories := data["repositories"].([]any)
	if len(repositories) != 1 {
		t.Fatalf("len(repositories) = %d, want 1; body = %s", len(repositories), rec.Body.String())
	}
	repo := repositories[0].(map[string]any)
	if got, want := repo["repo_slug"], "tenant-a/web"; got != want {
		t.Fatalf("repo_slug = %#v, want %#v (real row data from the fake driver)", got, want)
	}
}

// TestListRepositoriesByLanguageUnscopedQueriesStayUnfiltered is the
// no-regression counterpart: a shared/admin caller (no AuthContext) must
// still issue the byte-identical unscoped queries with no access-scoping
// predicate.
func TestListRepositoriesByLanguageUnscopedQueriesStayUnfiltered(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 23, 14, 0, 0, 0, time.UTC)
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"repository_count", "file_count", "last_indexed_at"},
			rows:    [][]driver.Value{{int64(1), int64(7), indexedAt}},
		},
		{
			columns: []string{
				"repo_id", "name", "path", "local_path", "remote_url", "repo_slug", "has_remote",
				"language", "file_count", "last_indexed_at",
			},
			rows: [][]driver.Value{
				{"repository:web", "web", "/src/web", "/src/web", "https://example.test/web", "acme/web", true, "typescript", int64(7), indexedAt},
			},
		},
	})
	handler := &RepositoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/by-language?language=typescript&limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositoriesByLanguage(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	for i, dispatched := range recorder.queries {
		if strings.Contains(dispatched, "repo_id = ANY(") {
			t.Fatalf("unscoped/admin query[%d] must stay unfiltered, got:\n%s", i, dispatched)
		}
	}
}

// TestGetRepositoryLanguageInventoryScopedEmptyGrantReturnsEmptyWithoutQuery
// is the language-inventory counterpart of the by-language empty-grant test.
func TestGetRepositoryLanguageInventoryScopedEmptyGrantReturnsEmptyWithoutQuery(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, nil)
	handler := &RepositoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/language-inventory?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a"}))
	rec := httptest.NewRecorder()

	handler.getRepositoryLanguageInventory(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(recorder.queries) != 0 {
		t.Fatalf("Postgres received %d queries, want 0 for an empty-grant scoped caller", len(recorder.queries))
	}
}

// TestGetRepositoryLanguageInventoryScopedGrantHitsRealStoreAndReturnsRowData
// proves the #5167 fix against the ACTUAL production backend (ContentReader
// over a real *sql.DB): a scoped caller with a matching grant reaches
// Postgres, the dispatched query carries the access-scoping predicate with
// the caller's granted repo id bound as an arg, and the response surfaces the
// real row data the fake driver returned.
func TestGetRepositoryLanguageInventoryScopedGrantHitsRealStoreAndReturnsRowData(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 23, 14, 45, 0, 0, time.UTC)
	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"language", "repository_count", "file_count", "last_indexed_at"},
			rows:    [][]driver.Value{{"typescript", int64(1), int64(7), indexedAt}},
		},
	})
	handler := &RepositoryHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/language-inventory?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		AllowedRepositoryIDs: []string{"repository:tenant-a-web"},
	}))
	rec := httptest.NewRecorder()

	handler.getRepositoryLanguageInventory(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("Postgres received %d queries, want exactly %d", got, want)
	}
	dispatched := recorder.queries[0]
	if !strings.Contains(dispatched, "repo_id = ANY(") {
		t.Fatalf("dispatched query missing #5167 access-scoping predicate:\n%s", dispatched)
	}
	found := false
	for _, arg := range recorder.args[0] {
		if s := fmt.Sprintf("%v", arg); strings.Contains(s, "repository:tenant-a-web") {
			found = true
		}
	}
	if !found {
		t.Fatalf("bound args %#v did not include the granted repository id", recorder.args[0])
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	languages := data["languages"].([]any)
	if len(languages) != 1 {
		t.Fatalf("len(languages) = %d, want 1; body = %s", len(languages), rec.Body.String())
	}
	language := languages[0].(map[string]any)
	if got, want := language["language"], "typescript"; got != want {
		t.Fatalf("language = %#v, want %#v (real row data from the fake driver)", got, want)
	}
}

func sameStringSet(got []string, want []string) bool {
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	if len(gotCopy) != len(wantCopy) {
		return false
	}
	for i := range gotCopy {
		if gotCopy[i] != wantCopy[i] {
			return false
		}
	}
	return true
}

func TestRepositoryLanguageInventoryReturnsAggregates(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 23, 14, 45, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			languageInventory: []RepositoryLanguageInventoryRow{
				{Language: "typescript", RepositoryCount: 135, FileCount: 7140, LastIndexedAt: indexedAt},
				{Language: "go", RepositoryCount: 3, FileCount: 30, LastIndexedAt: indexedAt.Add(-time.Hour)},
			},
		},
		Profile: ProfileProduction,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/language-inventory?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.getRepositoryLanguageInventory(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	languages := data["languages"].([]any)
	if got, want := len(languages), 2; got != want {
		t.Fatalf("len(languages) = %d, want %d", got, want)
	}
	first := languages[0].(map[string]any)
	if got, want := first["language"], "typescript"; got != want {
		t.Fatalf("language = %#v, want %#v", got, want)
	}
	if got, want := first["repository_count"], float64(135); got != want {
		t.Fatalf("repository_count = %#v, want %#v", got, want)
	}
}

func TestRepositoryLanguageFamilyAliases(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		input string
		want  []string
	}{
		{input: "typescript", want: []string{"typescript", "tsx"}},
		{input: "ts", want: []string{"typescript", "tsx"}},
		{input: "javascript", want: []string{"javascript", "jsx"}},
		{input: "terraform", want: []string{"terraform", "hcl", "tfvars"}},
		{input: "go", want: []string{"go"}},
	} {
		got := repositoryLanguageFamily(tt.input)
		if !sameStringSet(got, tt.want) {
			t.Fatalf("repositoryLanguageFamily(%q) = %#v, want %#v", tt.input, got, tt.want)
		}
	}
}
