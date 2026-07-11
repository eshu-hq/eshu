// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentHandlerReadFileLinesResolvesRepositorySelector(t *testing.T) {
	t.Parallel()

	store := &selectorAwareContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
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
