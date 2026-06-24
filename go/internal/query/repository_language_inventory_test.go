// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
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
