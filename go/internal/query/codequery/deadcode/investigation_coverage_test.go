// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// coverageProbeDeadCodeStore records which coverage reads the investigation
// issues. The full RepositoryCoverage read aggregates every content_entities
// row of the repository (#7525), so the investigation must never call it when
// the narrow file reads exist.
type coverageProbeDeadCodeStore struct {
	contentCandidateDeadCodeStore
	narrowCoverage    querycontract.RepositoryContentCoverage
	narrowErr         error
	filesIndexedAt    time.Time
	filesIndexedAtErr error
	fullCalls         int
	narrowCalls       int
	filesIndexedCalls int
}

func (s *coverageProbeDeadCodeStore) RepositoryCoverage(
	ctx context.Context,
	repoID string,
) (querycontract.RepositoryContentCoverage, error) {
	s.fullCalls++
	return s.contentCandidateDeadCodeStore.RepositoryCoverage(ctx, repoID)
}

func (s *coverageProbeDeadCodeStore) RepositoryContextCoverage(
	context.Context,
	string,
) (querycontract.RepositoryContentCoverage, error) {
	s.narrowCalls++
	return s.narrowCoverage, s.narrowErr
}

func (s *coverageProbeDeadCodeStore) RepositoryFilesLastIndexedAt(context.Context, string) (time.Time, error) {
	s.filesIndexedCalls++
	return s.filesIndexedAt, s.filesIndexedAtErr
}

func investigateWithCoverageProbe(t *testing.T, store *coverageProbeDeadCodeStore) (int, []byte) {
	t.Helper()
	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph.FakeGraphReader{},
		Content: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/investigate",
		bytes.NewBufferString(`{"repo_id":"payments","limit":10}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func newCoverageProbeStore(filesIndexedAt time.Time) *coverageProbeDeadCodeStore {
	return &coverageProbeDeadCodeStore{
		contentCandidateDeadCodeStore: contentCandidateDeadCodeStore{
			fakeDeadCodeContentStore: fakeDeadCodeContentStore{
				FakePortContentStore: content.FakePortContentStore{
					// The full coverage values differ from the narrow ones so a
					// read of the wrong source is visible in the response.
					Coverage: deadcode.RepositoryContentCoverage{
						Available:       true,
						FileCount:       999,
						EntityCount:     888,
						FileIndexedAt:   filesIndexedAt.Add(-time.Hour),
						EntityIndexedAt: filesIndexedAt.Add(time.Hour),
					},
					Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-1", Name: "payments"}},
				},
			},
		},
		narrowCoverage: querycontract.RepositoryContentCoverage{
			Available: true,
			FileCount: 12,
			Languages: []querycontract.RepositoryLanguageCount{
				{Language: "go", FileCount: 8},
				{Language: "python", FileCount: 4},
			},
		},
		filesIndexedAt: filesIndexedAt,
	}
}

func TestHandleDeadCodeInvestigationCoverageSkipsEntityAggregate(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	store := newCoverageProbeStore(indexedAt)
	status, body := investigateWithCoverageProbe(t, store)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", status, body)
	}

	if store.fullCalls != 0 {
		t.Fatalf("RepositoryCoverage calls = %d, want 0: the entity aggregate must not run", store.fullCalls)
	}
	if store.narrowCalls != 1 || store.filesIndexedCalls != 1 {
		t.Fatalf("narrow coverage calls = %d, files indexed_at calls = %d, want 1 and 1", store.narrowCalls, store.filesIndexedCalls)
	}

	data := testutil.DecodeEnvelopeData(t, body)
	coverage := requireDeadCodeInvestigationMap(t, data, "coverage")
	if _, present := coverage["entity_count"]; present {
		t.Fatalf("coverage.entity_count = %#v, want the key absent", coverage["entity_count"])
	}
	if got, want := coverage["content_coverage_available"], true; got != want {
		t.Fatalf("coverage.content_coverage_available = %#v, want %#v", got, want)
	}
	if got, want := coverage["file_count"], float64(12); got != want {
		t.Fatalf("coverage.file_count = %#v, want %#v", got, want)
	}
	wantLanguages := []any{
		map[string]any{"language": "go", "file_count": float64(8)},
		map[string]any{"language": "python", "file_count": float64(4)},
	}
	if got := coverage["languages"]; !reflect.DeepEqual(got, wantLanguages) {
		t.Fatalf("coverage.languages = %#v, want %#v", got, wantLanguages)
	}
	if got, want := coverage["content_last_indexed_at"], indexedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("coverage.content_last_indexed_at = %#v, want files max %#v", got, want)
	}
	if got, want := coverage["freshness_state"], "content_index_available"; got != want {
		t.Fatalf("coverage.freshness_state = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeInvestigationCoverageWithoutFilesReportsNoTimestamp(t *testing.T) {
	t.Parallel()

	store := newCoverageProbeStore(time.Time{})
	status, body := investigateWithCoverageProbe(t, store)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", status, body)
	}
	data := testutil.DecodeEnvelopeData(t, body)
	coverage := requireDeadCodeInvestigationMap(t, data, "coverage")
	if _, present := coverage["content_last_indexed_at"]; present {
		t.Fatalf("coverage.content_last_indexed_at = %#v, want absent when no file is indexed", coverage["content_last_indexed_at"])
	}
	if got, want := coverage["freshness_state"], "not_reported"; got != want {
		t.Fatalf("coverage.freshness_state = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeInvestigationCoverageContextCoverageErrorFailsRequest(t *testing.T) {
	t.Parallel()

	store := newCoverageProbeStore(time.Time{})
	store.narrowErr = errors.New("context coverage unavailable")
	status, body := investigateWithCoverageProbe(t, store)
	if status == http.StatusOK {
		t.Fatalf("status = 200, want an error when the context coverage read fails body=%s", body)
	}
	if store.fullCalls != 0 {
		t.Fatalf("RepositoryCoverage calls = %d, want 0 after a narrow read error", store.fullCalls)
	}
	if store.filesIndexedCalls != 0 {
		t.Fatalf("files indexed_at calls = %d, want 0 after the context coverage read failed", store.filesIndexedCalls)
	}
}

func TestHandleDeadCodeInvestigationCoverageFilesIndexedAtErrorFailsRequest(t *testing.T) {
	t.Parallel()

	store := newCoverageProbeStore(time.Time{})
	store.filesIndexedAtErr = errors.New("files indexed_at unavailable")
	status, body := investigateWithCoverageProbe(t, store)
	if status == http.StatusOK {
		t.Fatalf("status = 200, want an error when the files indexed_at read fails body=%s", body)
	}
	if store.fullCalls != 0 {
		t.Fatalf("RepositoryCoverage calls = %d, want 0 after a narrow read error", store.fullCalls)
	}
}
