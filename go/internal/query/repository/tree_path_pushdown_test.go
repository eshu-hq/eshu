// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

type pathScopedTreeStore struct {
	content.FakePortContentStore
	scopedFiles []querycontract.FileContent
	broadCalls  int
	pathCalls   int
	pathExists  bool
	contextPath string
	gotPath     string
	gotLimit    int
}

func (s *pathScopedTreeStore) ListRepoFiles(_ context.Context, _ string, _ int) ([]querycontract.FileContent, error) {
	s.broadCalls++
	return []querycontract.FileContent{{RepoID: "repo-1", RelativePath: "aaa/unrelated.go", CommitSHA: "abc123"}}, nil
}

func (s *pathScopedTreeStore) ListRepoFilesByPath(_ context.Context, _ string, path string, limit int) ([]querycontract.FileContent, error) {
	s.pathCalls++
	s.gotPath = path
	s.gotLimit = limit
	return s.scopedFiles, nil
}

func (s *pathScopedTreeStore) RepoFilePathContext(_ context.Context, _ string, path string) (bool, string, error) {
	s.contextPath = path
	return s.pathExists || len(s.scopedFiles) > 0, "abc123", nil
}

func TestGetRepositoryTreeScopesPathBeforeFileCap(t *testing.T) {
	store := &pathScopedTreeStore{
		FakePortContentStore: content.FakePortContentStore{
			Repositories: []querycontract.RepositoryCatalogEntry{testutil.RepositoryStatsCatalogEntry()},
		},
		scopedFiles: []querycontract.FileContent{
			{RepoID: "repo-1", RelativePath: "zzz/late/deep/one.go", CommitSHA: "abc123", LineCount: 7},
			{RepoID: "repo-1", RelativePath: "zzz/late/two.go", CommitSHA: "abc123", LineCount: 3},
		},
	}
	handler := &Handler{Content: store}

	w := requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?path=zzz/late")
	resp := decodeRepositoryTree(t, w)
	entries := repositoryTreeEntries(t, resp)
	if len(entries) != 2 || entries["deep"]["child_count"] != float64(1) || entries["two.go"]["path"] != "zzz/late/two.go" {
		t.Fatalf("one-level scoped entries = %#v", entries)
	}
	if store.broadCalls != 0 || store.pathCalls != 1 || store.gotPath != "zzz/late" || store.contextPath != "" || store.gotLimit != TreeFileLimit+1 {
		t.Fatalf("read routing: broad=%d path=%d gotPath=%q contextPath=%q gotLimit=%d", store.broadCalls, store.pathCalls, store.gotPath, store.contextPath, store.gotLimit)
	}

	w = requestRepositoryTree(t, handler, "/api/v0/repositories/repo-1/tree?path=zzz/late&recursive=true&ref=abc123")
	resp = decodeRepositoryTree(t, w)
	entries = repositoryTreeEntries(t, resp)
	if len(entries) != 3 || entries["one.go"]["path"] != "zzz/late/deep/one.go" {
		t.Fatalf("recursive scoped entries = %#v", entries)
	}
}

func TestGetRepositoryTreeScopedPathMissingReturns404(t *testing.T) {
	store := &pathScopedTreeStore{FakePortContentStore: content.FakePortContentStore{
		Repositories: []querycontract.RepositoryCatalogEntry{testutil.RepositoryStatsCatalogEntry()},
	}}
	w := requestRepositoryTree(t, &Handler{Content: store}, "/api/v0/repositories/repo-1/tree?path=missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

func TestGetRepositoryTreeScopedPathFileReturns404(t *testing.T) {
	store := &pathScopedTreeStore{FakePortContentStore: content.FakePortContentStore{
		Repositories: []querycontract.RepositoryCatalogEntry{testutil.RepositoryStatsCatalogEntry()},
	}, pathExists: true}
	w := requestRepositoryTree(t, &Handler{Content: store}, "/api/v0/repositories/repo-1/tree?path=README.md")
	if w.Code != http.StatusNotFound {
		t.Fatalf("file path status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

func TestGetRepositoryTreeScopedPathCapCountsOnlyDescendants(t *testing.T) {
	files := make([]querycontract.FileContent, TreeFileLimit+1)
	for i := range files {
		files[i] = querycontract.FileContent{
			RepoID: "repo-1", RelativePath: fmt.Sprintf("late/%05d.go", i), CommitSHA: "abc123",
		}
	}
	store := &pathScopedTreeStore{
		FakePortContentStore: content.FakePortContentStore{
			Repositories: []querycontract.RepositoryCatalogEntry{testutil.RepositoryStatsCatalogEntry()},
		},
		scopedFiles: files,
	}
	resp := decodeRepositoryTree(t, requestRepositoryTree(t, &Handler{Content: store}, "/api/v0/repositories/repo-1/tree?path=late"))
	if resp["truncated"] != true {
		t.Fatalf("truncated = %v, want true", resp["truncated"])
	}
	if got := len(resp["entries"].([]any)); got != TreeFileLimit {
		t.Fatalf("entries = %d, want %d", got, TreeFileLimit)
	}
	if store.broadCalls != 0 || store.gotLimit != TreeFileLimit+1 {
		t.Fatalf("read routing: broad=%d gotLimit=%d", store.broadCalls, store.gotLimit)
	}
}
