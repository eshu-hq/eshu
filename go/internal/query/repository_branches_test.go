// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
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
	// Legacy fallback carries an empty tags array, not a missing key.
	tags, ok := resp["tags"].([]any)
	if !ok || len(tags) != 0 {
		t.Fatalf("tags = %#v, want [] (empty, not absent)", resp["tags"])
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
	// Non-default branch always carries is_default: false.
	release := branches[1].(map[string]any)
	if got, ok := release["is_default"]; !ok || got != false {
		t.Fatalf("branches[1].is_default = %#v (ok=%v), want false, always present", got, ok)
	}
}

func TestGetRepositoryBranchesReturnsTagsSeparately(t *testing.T) {
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
				{
					Name:       "v1.0.0",
					Kind:       "tag",
					HeadSHA:    "abc123",
					ObservedAt: observedAt,
					IndexedAt:  indexedAt,
				},
				{
					Name:       "v1.1.0",
					Kind:       "tag",
					HeadSHA:    "def456",
					ObservedAt: observedAt,
					IndexedAt:  indexedAt,
				},
				{
					Name:       "v2.0.0",
					Kind:       "tag",
					HeadSHA:    "fff999",
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

	// Branches still present.
	branches, ok := resp["branches"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("branches = %#v, want 2", resp["branches"])
	}

	// Tags returned separately.
	tags, ok := resp["tags"].([]any)
	if !ok || len(tags) != 3 {
		t.Fatalf("tags = %#v, want 3", resp["tags"])
	}
	firstTag := tags[0].(map[string]any)
	if got, want := firstTag["name"], "v1.0.0"; got != want {
		t.Fatalf("tags[0].name = %#v, want %#v", got, want)
	}
	if got, want := firstTag["kind"], "tag"; got != want {
		t.Fatalf("tags[0].kind = %#v, want %#v", got, want)
	}
	if got, want := firstTag["head_sha"], "abc123"; got != want {
		t.Fatalf("tags[0].head_sha = %#v, want %#v", got, want)
	}
	if got, ok := firstTag["is_default"]; ok && got == true {
		t.Fatal("tags[0].is_default is true, want absent or false")
	}

	// default_branch unchanged.
	if got, want := resp["default_branch"], "main"; got != want {
		t.Fatalf("default_branch = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryBranchesTagSameNameAsBranch(t *testing.T) {
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
					Name:       "main",
					Kind:       "tag",
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

	// Branch "main" is default.
	branches, ok := resp["branches"].([]any)
	if !ok || len(branches) != 1 {
		t.Fatalf("branches = %#v, want 1", resp["branches"])
	}
	if got, want := branches[0].(map[string]any)["name"], "main"; got != want {
		t.Fatalf("branch name = %#v, want %#v", got, want)
	}

	// Tag "main" is NOT default and appears in tags, not branches.
	tags, ok := resp["tags"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("tags = %#v, want 1", resp["tags"])
	}
	tagEntry := tags[0].(map[string]any)
	if got, want := tagEntry["name"], "main"; got != want {
		t.Fatalf("tag name = %#v, want %#v", got, want)
	}
	if got, want := tagEntry["kind"], "tag"; got != want {
		t.Fatalf("tag kind = %#v, want %#v", got, want)
	}
	if got, ok := tagEntry["is_default"]; ok && got == true {
		t.Fatal("tag is_default is true, want absent or false")
	}
}

// TestGetRepositoryBranchesTagsExceedingCapAreTruncated exercises the old
// tag-cap scenario (600 tags) under the #5503 combined-paging contract: the
// per-tag cap is retired in favor of one limit+cursor over the combined
// branches+tags stream (default limit 100), and tags_truncated keeps its
// original meaning -- computed from the exact in-memory remainder -- but is
// now page-relative rather than a fixed 500-tag cap.
func TestGetRepositoryBranchesTagsExceedingCapAreTruncated(t *testing.T) {
	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC)

	refs := []RepositoryRef{
		{Name: "main", Kind: "branch", HeadSHA: "abc123", Default: true, ObservedAt: observedAt, IndexedAt: indexedAt},
	}
	for i := 0; i < 600; i++ {
		refs = append(refs, RepositoryRef{
			Name:       fmt.Sprintf("v1.%d.0", i),
			Kind:       "tag",
			HeadSHA:    "abc123",
			ObservedAt: observedAt,
			IndexedAt:  indexedAt,
		})
	}
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: refs,
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

	// Default page size is 100: 1 default branch + 99 tags fill the page.
	branches, ok := resp["branches"].([]any)
	if !ok || len(branches) != 1 {
		t.Fatalf("branches = %#v, want 1", resp["branches"])
	}
	tags, ok := resp["tags"].([]any)
	if !ok {
		t.Fatal("tags key missing")
	}
	if got, want := len(tags), 99; got != want {
		t.Fatalf("len(tags) = %d, want %d (page fill after 1 branch)", got, want)
	}
	if truncated, _ := resp["truncated"].(bool); !truncated {
		t.Fatal("truncated = false, want true when more refs remain")
	}
	if _, ok := resp["next_cursor"].(string); !ok {
		t.Fatal("next_cursor missing on a truncated page")
	}
	// Deprecated field keeps its original meaning: more tags exist beyond
	// what tags[] carries in this page.
	truncated, _ := resp["tags_truncated"].(bool)
	if !truncated {
		t.Fatal("tags_truncated = false, want true when more tags remain beyond this page")
	}
	// Default branch still correct.
	if got, want := resp["default_branch"], "main"; got != want {
		t.Fatalf("default_branch = %#v, want %#v", got, want)
	}
}

func TestGetRepositoryBranchesTagsWithinCapNotTruncated(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: []RepositoryRef{
				{Name: "main", Kind: "branch", HeadSHA: "abc123", Default: true, ObservedAt: observedAt, IndexedAt: indexedAt},
				{Name: "v1.0.0", Kind: "tag", HeadSHA: "abc123", ObservedAt: observedAt, IndexedAt: indexedAt},
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

	tags, ok := resp["tags"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("tags = %#v, want 1", resp["tags"])
	}
	if truncated, _ := resp["tags_truncated"].(bool); truncated {
		t.Fatal("tags_truncated = true, want false/absent when within cap")
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

func TestGetRepositoryBranches_LocalLightweightReturnsBranches(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Profile: ProfileLocalLightweight,
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    []FileContent{{RepoID: "repo-1", RelativePath: "main.go", CommitSHA: "abc123"}},
			coverage:     RepositoryContentCoverage{Available: true, FileCount: 1, FileIndexedAt: indexedAt},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/branches", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	handler.Mount(mux)
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var env ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v", err)
	}
	if env.Truth == nil {
		t.Fatal("truth envelope is nil")
	}
	if got, want := string(env.Truth.Profile), string(ProfileLocalLightweight); got != want {
		t.Fatalf("truth profile = %s, want %s", got, want)
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
