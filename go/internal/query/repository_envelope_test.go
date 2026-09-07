// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

func TestGetRepositoryStoryReturnsEnvelopeWhenRequested(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
					t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
				}
				if got, want := params["repo_id"], "repo-story"; got != want {
					t.Fatalf("repo_id param = %#v, want %#v", got, want)
				}
				return map[string]any{
					"id":         "repo-story",
					"name":       "story-service",
					"path":       "/repos/story-service",
					"local_path": "/repos/story-service",
					"has_remote": false,
				}, nil
			},
			run: querytestutil.StoryEnvelopeGraphRows(t, "repo-story"),
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	envelope := decodeRepositoryResponseEnvelope(t, w)
	if envelope.Truth == nil {
		t.Fatal("truth is nil, want repository story truth envelope")
	}
	if got, want := envelope.Truth.Capability, "platform_impact.context_overview"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Level, TruthLevelDerived; got != want {
		t.Fatalf("truth.level = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisHybrid; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	data := repositoryEnvelopeData(t, envelope)
	repository := querytestutil.MustMapField(t, data, "repository")
	if got, want := repository["id"], "repo-story"; got != want {
		t.Fatalf("repository.id = %#v, want %#v", got, want)
	}
	if story := StringVal(data, "story"); !strings.Contains(story, "story-service") {
		t.Fatalf("story = %q, want repository narrative", story)
	}
}

func TestGetRepositoryStatsReturnsEnvelopeWhenRequested(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Neo4j: fakeRepoGraphReader{
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
					t.Fatalf("RunSingle cypher = %q, want repository base lookup", cypher)
				}
				if got, want := params["repo_id"], "repo-1"; got != want {
					t.Fatalf("repo_id param = %#v, want %#v", got, want)
				}
				return querytestutil.RepositoryStatsGraphRow(), nil
			},
		},
		Content: fakePortContentStore{
			coverage: RepositoryContentCoverage{
				Available:       true,
				FileCount:       42,
				EntityCount:     7,
				FileIndexedAt:   indexedAt.Add(-time.Minute),
				EntityIndexedAt: indexedAt,
				Languages: []RepositoryLanguageCount{
					{Language: "go", FileCount: 30},
					{Language: "yaml", FileCount: 12},
				},
				EntityTypes: []RepositoryEntityTypeCount{
					{EntityType: "Function", Count: 5},
					{EntityType: "TerraformResource", Count: 2},
				},
			},
			repositories: []RepositoryCatalogEntry{querytestutil.RepositoryStatsCatalogEntry()},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/order-service/stats", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	envelope := decodeRepositoryResponseEnvelope(t, w)
	if envelope.Truth == nil {
		t.Fatal("truth is nil, want repository stats truth envelope")
	}
	if got, want := envelope.Truth.Capability, "platform_impact.context_overview"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisContentIndex; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	data := repositoryEnvelopeData(t, envelope)
	if got, want := data["file_count"], float64(42); got != want {
		t.Fatalf("file_count = %#v, want %#v", got, want)
	}
	coverage := querytestutil.MustMapField(t, data, "coverage")
	if got, want := coverage["query_shape"], repository.RepositoryStatsContentCoverageShape; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func decodeRepositoryResponseEnvelope(t *testing.T, w *httptest.ResponseRecorder) ResponseEnvelope {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	return envelope
}

func repositoryEnvelopeData(t *testing.T, envelope ResponseEnvelope) map[string]any {
	t.Helper()

	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", envelope.Data)
	}
	return data
}
