// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"
)

// fakeLanguageListerContentStore embeds the base fake and additionally implements
// repoFileLanguageLister, so the tree handler takes the pushed-down path where the
// language predicate and path/ref lookup run in the content store (and the file
// cap applies to the matching set) rather than the in-Go post-cap filter.
type fakeLanguageListerContentStore struct {
	fakePortContentStore
	byLanguage    []FileContent
	byLanguageErr error
	pathExists    bool
	indexedRef    string
	pathCtxErr    error
	gotLanguages  []string
	gotPath       string
	gotLimit      int
}

func (f *fakeLanguageListerContentStore) ListRepoFilesByLanguage(
	_ context.Context, _ string, languages []string, pathPrefix string, limit int,
) ([]FileContent, error) {
	f.gotLanguages = languages
	f.gotPath = pathPrefix
	f.gotLimit = limit
	return f.byLanguage, f.byLanguageErr
}

func (f *fakeLanguageListerContentStore) RepoFilePathContext(
	_ context.Context, _, _ string,
) (bool, string, error) {
	return f.pathExists, f.indexedRef, f.pathCtxErr
}

func languageListerTreeStore() *fakeLanguageListerContentStore {
	return &fakeLanguageListerContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
		},
		pathExists: true,
		indexedRef: "abc123",
	}
}

// TestGetRepositoryTreeLanguageFilterPushesPredicateBelowCap proves the language
// predicate is pushed into the content-store read so a matching file that would
// sort beyond the repository file cap is still returned (the cap applies to the
// matching set, not the whole repository).
func TestGetRepositoryTreeLanguageFilterPushesPredicateBelowCap(t *testing.T) {
	store := languageListerTreeStore()
	store.byLanguage = []FileContent{
		{RepoID: "repo-1", RelativePath: "zzz/late_sorting.py", CommitSHA: "abc123", LineCount: 5, Language: "python"},
	}
	handler := &RepositoryHandler{Content: store}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?recursive=true&language=python")
	resp := decodeRepositoryTree(t, w)
	entries := repositoryTreeEntries(t, resp)
	if _, ok := entries["late_sorting.py"]; !ok {
		t.Fatalf("pushed-down language listing missing late-sorting python file; got %v", entries)
	}
	if len(store.gotLanguages) == 0 || store.gotLanguages[0] != "python" {
		t.Fatalf("language predicate not pushed down; got %v", store.gotLanguages)
	}
	if store.gotLimit != repositoryTreeFileLimit+1 {
		t.Fatalf("pushed-down limit = %d, want %d", store.gotLimit, repositoryTreeFileLimit+1)
	}
	if got := resp["ref"]; got != "abc123" {
		t.Fatalf("ref = %v, want abc123", got)
	}
}

// TestGetRepositoryTreeLanguageFilterRealPathZeroMatchesEmptyNot404 proves a real
// directory with zero files in the requested language returns an empty 200, not a
// 404, because path existence is resolved unfiltered.
func TestGetRepositoryTreeLanguageFilterRealPathZeroMatchesEmptyNot404(t *testing.T) {
	store := languageListerTreeStore()
	store.pathExists = true
	store.byLanguage = nil
	handler := &RepositoryHandler{Content: store}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?path=cmd/app&language=rust")
	resp := decodeRepositoryTree(t, w)
	entries := repositoryTreeEntries(t, resp)
	if len(entries) != 0 {
		t.Fatalf("zero-match listing must be empty; got %v", entries)
	}
	// The path scope must be pushed into the read (applied before the cap), not
	// just in Go after fetching the whole repo.
	if store.gotPath != "cmd/app" {
		t.Fatalf("path scope not pushed down; gotPath = %q, want cmd/app", store.gotPath)
	}
}

// TestGetRepositoryTreeLanguageFilterUnknownPathReturns404 proves an unknown path
// still 404s on the pushed-down path.
func TestGetRepositoryTreeLanguageFilterUnknownPathReturns404(t *testing.T) {
	store := languageListerTreeStore()
	store.pathExists = false
	handler := &RepositoryHandler{Content: store}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?path=nope&language=go")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}
