// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

type contextCoverageProbeStore struct {
	content.FakePortContentStore
	narrowCoverage            querycontract.RepositoryContentCoverage
	narrowErr                 error
	narrowCalls, genericCalls int
}

func (s *contextCoverageProbeStore) RepositoryContextCoverage(context.Context, string) (querycontract.RepositoryContentCoverage, error) {
	s.narrowCalls++
	return s.narrowCoverage, s.narrowErr
}

func (s *contextCoverageProbeStore) RepositoryCoverage(ctx context.Context, repoID string) (querycontract.RepositoryContentCoverage, error) {
	s.genericCalls++
	return s.FakePortContentStore.RepositoryCoverage(ctx, repoID)
}

func TestRepositoryContextCoverageUsesNarrowRead(t *testing.T) {
	t.Parallel()

	store := &contextCoverageProbeStore{
		FakePortContentStore: content.FakePortContentStore{Coverage: querycontract.RepositoryContentCoverage{Available: true, FileCount: 999}},
		narrowCoverage: querycontract.RepositoryContentCoverage{
			Available: true,
			FileCount: 3,
			Languages: []querycontract.RepositoryLanguageCount{{Language: "go", FileCount: 2}, {Language: "yaml", FileCount: 1}},
		},
	}
	coverage, err := loadRepositoryContentCoverage(t.Context(), store, "repo-1")
	if err != nil {
		t.Fatalf("loadRepositoryContentCoverage() error = %v", err)
	}
	if store.narrowCalls != 1 || store.genericCalls != 0 {
		t.Fatalf("coverage calls: narrow=%d generic=%d, want 1 and 0", store.narrowCalls, store.genericCalls)
	}
	if coverage == nil || coverage.FileCount != 3 || len(coverage.Languages) != 2 {
		t.Fatalf("coverage = %#v, want narrow file count and language groups", coverage)
	}
}

func TestRepositoryContextCoverageFallbackAndReadError(t *testing.T) {
	t.Parallel()

	fallback := content.FakePortContentStore{Coverage: querycontract.RepositoryContentCoverage{
		Available: true, FileCount: 4,
		Languages: []querycontract.RepositoryLanguageCount{{Language: "go", FileCount: 4}},
	}}
	coverage, err := loadRepositoryContentCoverage(t.Context(), fallback, "repo-1")
	if err != nil || coverage == nil || coverage.FileCount != 4 {
		t.Fatalf("generic fallback coverage = %#v, error = %v", coverage, err)
	}

	store := &contextCoverageProbeStore{narrowErr: errors.New("context summary unavailable")}
	coverage, err = loadRepositoryContentCoverage(t.Context(), store, "repo-1")
	if coverage != nil || err == nil || store.narrowCalls != 1 || store.genericCalls != 0 {
		t.Fatalf("narrow error: coverage=%#v error=%v narrow=%d generic=%d", coverage, err, store.narrowCalls, store.genericCalls)
	}
}

type contextFileProbeStore struct {
	content.FakePortContentStore
	fileCount int
	fileErr   error
	limitSeen int
}

func (s *contextFileProbeStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.limitSeen = limit
	if s.fileErr != nil {
		return nil, s.fileErr
	}
	files := make([]querycontract.FileContent, 0, min(s.fileCount, limit))
	for i := 0; i < s.fileCount && i < limit; i++ {
		files = append(files, querycontract.FileContent{RepoID: repoID, RelativePath: fmt.Sprintf("src/f%05d.go", i)})
	}
	return files, nil
}

func TestRepositoryContextDisclosesFileListTruncation(t *testing.T) {
	t.Parallel()

	const limit = querycontract.RepositorySemanticEntityLimit
	for _, tc := range []struct {
		name          string
		fileCount     int
		wantTruncated bool
	}{
		{name: "exact limit", fileCount: limit},
		{name: "sentinel row", fileCount: limit + 1, wantTruncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &contextFileProbeStore{fileCount: tc.fileCount}
			reader := graph.FakeGraphReader{
				RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
					if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
						return map[string]any{"id": "repo-1", "name": "example"}, nil
					}
					return nil, nil
				},
				RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, "RETURN count(") {
						return []map[string]any{{"count": int64(0)}}, nil
					}
					return nil, nil
				},
			}
			var logs bytes.Buffer
			handler := &Handler{Neo4j: reader, Content: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/context", nil)
			req.SetPathValue("repo_id", "repo-1")
			rec := httptest.NewRecorder()
			handler.getRepositoryContext(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			if store.limitSeen != limit+1 {
				t.Fatalf("file read limit = %d, want %d sentinel read", store.limitSeen, limit+1)
			}
			if !strings.Contains(logs.String(), `"stage":"content_coverage"`) {
				t.Fatalf("stage logs lack content coverage timing: %s", logs.String())
			}
			body := decodeRepositoryAuthzBody(t, rec)
			reasons := testutil.RequireStringAnySlice(t, body, "partial_reasons")
			if got := testutil.AnySliceContains(reasons, "repository_context_file_read_truncated_at_5000"); got != tc.wantTruncated {
				t.Fatalf("partial_reasons = %#v, truncation=%v, want %v", reasons, got, tc.wantTruncated)
			}
			if tc.wantTruncated && !strings.Contains(logs.String(), `"truncated":true`) {
				t.Fatalf("stage logs lack truncated=true: %s", logs.String())
			}
		})
	}
}

func TestRepositoryContextDisclosesFileListFailure(t *testing.T) {
	t.Parallel()

	store := &contextFileProbeStore{fileErr: errors.New("read unavailable")}
	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repo-1", "name": "example"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return nil, nil
		},
	}
	var logs bytes.Buffer
	handler := &Handler{Neo4j: reader, Content: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/context", nil)
	req.SetPathValue("repo_id", "repo-1")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	reasons := testutil.RequireStringAnySlice(t, body, "partial_reasons")
	if !testutil.AnySliceContains(reasons, "repository_context_file_read_degraded") {
		t.Fatalf("partial_reasons = %#v, want file-read degradation", reasons)
	}
	if !strings.Contains(logs.String(), `"failure_class":"repository_context_file_read_degraded"`) {
		t.Fatalf("stage logs lack file-read failure class: %s", logs.String())
	}
}

func TestRepositoryContextNarrowCoveragePreservesResponse(t *testing.T) {
	t.Parallel()

	fullCoverage := querycontract.RepositoryContentCoverage{
		Available: true,
		FileCount: 3,
		Languages: []querycontract.RepositoryLanguageCount{{Language: "go", FileCount: 2}, {Language: "yaml", FileCount: 1}},
	}
	files := []querycontract.FileContent{
		{RepoID: "repo-1", RelativePath: "src/main.go"},
		{RepoID: "repo-1", RelativePath: "src/lib.go"},
		{RepoID: "repo-1", RelativePath: "config/app.yaml"},
	}
	generic := content.FakePortContentStore{Coverage: fullCoverage, RepoFiles: files}
	narrow := &contextCoverageProbeStore{
		FakePortContentStore: generic,
		narrowCoverage: querycontract.RepositoryContentCoverage{
			Available: true, FileCount: fullCoverage.FileCount, Languages: fullCoverage.Languages,
		},
	}
	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repo-1", "name": "example"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return nil, nil
		},
	}
	response := func(store querycontract.ContentStore) string {
		t.Helper()
		handler := &Handler{Neo4j: reader, Content: store}
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/context", nil)
		req.SetPathValue("repo_id", "repo-1")
		rec := httptest.NewRecorder()
		handler.getRepositoryContext(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	want := response(generic)
	got := response(narrow)
	if got != want {
		t.Fatalf("narrow context response differs from full coverage:\nfull: %s\nnarrow: %s", want, got)
	}
	if narrow.narrowCalls != 1 || narrow.genericCalls != 0 {
		t.Fatalf("narrow coverage calls: narrow=%d generic=%d", narrow.narrowCalls, narrow.genericCalls)
	}
}

func TestRepositoryContextDisclosesContentCoverageFailure(t *testing.T) {
	t.Parallel()

	store := &contextCoverageProbeStore{narrowErr: errors.New("content summary unavailable")}
	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{"id": "repo-1", "name": "example"}, nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(") {
				return []map[string]any{{"count": int64(0)}}, nil
			}
			return nil, nil
		},
	}
	var logs bytes.Buffer
	handler := &Handler{Neo4j: reader, Content: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/context", nil)
	req.SetPathValue("repo_id", "repo-1")
	rec := httptest.NewRecorder()
	handler.getRepositoryContext(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	reasons := testutil.RequireStringAnySlice(t, body, "partial_reasons")
	if !testutil.AnySliceContains(reasons, "repository_context_content_coverage_degraded") {
		t.Fatalf("partial_reasons = %#v, want content coverage degradation", reasons)
	}
	if !strings.Contains(logs.String(), `"failure_class":"repository_context_content_coverage_degraded"`) {
		t.Fatalf("stage logs lack content coverage failure class: %s", logs.String())
	}
}
