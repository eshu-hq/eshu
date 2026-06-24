// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func requestRepositoryBranches(t *testing.T, handler *RepositoryHandler, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestGetRepositoryBranchesReturnsSingleIndexedRef(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    []FileContent{{RepoID: "repo-1", RelativePath: "main.go", CommitSHA: "abc123"}},
			coverage:     RepositoryContentCoverage{Available: true, FileCount: 1, FileIndexedAt: indexedAt},
		},
	}

	w := requestRepositoryBranches(t, handler, "/api/v0/repositories/repo-1/branches")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	branches, ok := resp["branches"].([]any)
	if !ok || len(branches) != 1 {
		t.Fatalf("branches = %#v, want 1 indexed ref", resp["branches"])
	}
	entry := branches[0].(map[string]any)
	if got, want := entry["head_sha"], "abc123"; got != want {
		t.Fatalf("head_sha = %#v, want %#v", got, want)
	}
	if entry["last_indexed_at"] == "" || entry["last_indexed_at"] == nil {
		t.Fatalf("last_indexed_at missing: %#v", entry)
	}
}

func TestGetRepositoryBranchesReturnsSourceBackedRefs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: []RepositoryRef{
				{
					Name:       "main",
					Kind:       "branch",
					HeadSHA:    "abc123",
					Default:    true,
					ObservedAt: observedAt,
					IndexedAt:  indexedAt,
				},
				{
					Name:       "release",
					Kind:       "branch",
					HeadSHA:    "def456",
					ObservedAt: observedAt,
					IndexedAt:  indexedAt,
				},
			},
		},
	}

	w := requestRepositoryBranches(t, handler, "/api/v0/repositories/repo-1/branches")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, want := resp["default_branch"], "main"; got != want {
		t.Fatalf("default_branch = %#v, want %#v", got, want)
	}
	branches, ok := resp["branches"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("branches = %#v, want 2 source-backed refs", resp["branches"])
	}
	entry := branches[0].(map[string]any)
	if got, want := entry["name"], "main"; got != want {
		t.Fatalf("branches[0].name = %#v, want %#v", got, want)
	}
	if got, want := entry["kind"], "branch"; got != want {
		t.Fatalf("branches[0].kind = %#v, want %#v", got, want)
	}
	if got, want := entry["head_sha"], "abc123"; got != want {
		t.Fatalf("branches[0].head_sha = %#v, want %#v", got, want)
	}
	if got := entry["is_default"]; got != true {
		t.Fatalf("branches[0].is_default = %#v, want true", got)
	}
	if entry["observed_at"] == "" || entry["observed_at"] == nil {
		t.Fatalf("observed_at missing: %#v", entry)
	}
	if entry["last_indexed_at"] == "" || entry["last_indexed_at"] == nil {
		t.Fatalf("last_indexed_at missing: %#v", entry)
	}
}

func TestGetRepositoryBranchesEmptyWhenNoCommitIndexed(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
		},
	}
	w := requestRepositoryBranches(t, handler, "/api/v0/repositories/repo-1/branches")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	branches, ok := resp["branches"].([]any)
	if !ok || len(branches) != 0 {
		t.Fatalf("branches = %#v, want empty when no commit indexed", resp["branches"])
	}
}

func TestGetRepositoryBranchesUnknownRepoReturns404(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{Content: fakePortContentStore{}}
	w := requestRepositoryBranches(t, handler, "/api/v0/repositories/repo-ghost/branches")
	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}
