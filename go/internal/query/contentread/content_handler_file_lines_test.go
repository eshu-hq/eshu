// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// selectorAwareContentStore is a local copy of root package query's
// selector-aware test double (content_handler_selector_test.go), which cannot
// be imported across the package boundary. It embeds the shared
// querytestutil.FakePortContentStore double and records the resolved
// repository each content call ran against; keep it minimal and mirror the
// root original if either changes.
type selectorAwareContentStore struct {
	querytestutil.FakePortContentStore
	fileRepoID          string
	fileLinesRepoID     string
	searchFileRepoIDs   []string
	searchEntityRepoIDs []string
}

func (s *selectorAwareContentStore) GetFileContent(_ context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	s.fileRepoID = repoID
	return &querycontract.FileContent{RepoID: repoID, RelativePath: relativePath}, nil
}

func (s *selectorAwareContentStore) GetFileLines(_ context.Context, repoID, relativePath string, startLine, endLine int) (*querycontract.FileContent, error) {
	s.fileLinesRepoID = repoID
	return &querycontract.FileContent{RepoID: repoID, RelativePath: relativePath, Content: "selected"}, nil
}

func (s *selectorAwareContentStore) SearchFileContent(_ context.Context, repoID, pattern string, limit int) ([]querycontract.FileContent, error) {
	s.searchFileRepoIDs = append(s.searchFileRepoIDs, repoID)
	return []querycontract.FileContent{{RepoID: repoID, RelativePath: "src/app.go"}}, nil
}

func (s *selectorAwareContentStore) SearchEntityContent(_ context.Context, repoID, pattern string, limit int) ([]querycontract.EntityContent, error) {
	s.searchEntityRepoIDs = append(s.searchEntityRepoIDs, repoID)
	return []querycontract.EntityContent{{RepoID: repoID, RelativePath: "src/app.go", EntityName: "handler"}}, nil
}

func TestContentHandlerReadFileLinesResolvesRepositorySelector(t *testing.T) {
	t.Parallel()

	store := &selectorAwareContentStore{
		FakePortContentStore: querytestutil.FakePortContentStore{
			Repositories: []querycontract.RepositoryCatalogEntry{{
				ID:       "repo-1",
				RepoSlug: "acme/payments",
			}},
		},
	}
	handler := &ContentHandler{Content: store}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/lines",
		bytes.NewBufferString(`{"repo_id":"acme/payments","relative_path":"src/app.go","start_line":2,"end_line":4}`),
	)
	rec := httptest.NewRecorder()

	handler.readFileLines(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.fileLinesRepoID, "repo-1"; got != want {
		t.Fatalf("GetFileLines repo_id = %q, want %q", got, want)
	}
}
