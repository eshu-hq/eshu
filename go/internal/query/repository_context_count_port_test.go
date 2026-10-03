// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type contextCountPortStore struct {
	fakePortContentStore
	counts querycontract.RepositoryReadModelCounts
	err    error
	calls  int
}

func (s *contextCountPortStore) RepositoryReadModelCounts(_ context.Context, _ string) (querycontract.RepositoryReadModelCounts, error) {
	s.calls++
	return s.counts, s.err
}

func (s *contextCountPortStore) RepositoryReadModelSummary(context.Context, string) (RepositoryReadModelSummary, error) {
	panic("context must not load workload names through the full summary")
}

func TestRepositoryContextUsesCountPortAndPreservesGraphWorkloadTruth(t *testing.T) {
	store := &contextCountPortStore{counts: querycontract.RepositoryReadModelCounts{Available: true, PlatformCount: 0, DependencyCount: 0}}
	store.coverage = RepositoryContentCoverage{Available: true, FileCount: 7}
	h := &RepositoryHandler{Content: store, Neo4j: fakeRepoGraphReader{
		runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"id": "repo-count", "name": "count"}, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(DISTINCT w) AS count") {
				return []map[string]any{{"count": int64(2)}}, nil
			}
			if strings.Contains(cypher, "RETURN count(DISTINCT p) AS count") || strings.Contains(cypher, "RETURN count(DISTINCT dep) AS count") {
				t.Fatalf("zero count port was ignored: %s", cypher)
			}
			return nil, nil
		},
	}}
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-count/context", nil)
	req.SetPathValue("repo_id", "repo-count")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || body["workload_count"] != float64(2) || body["platform_count"] != float64(0) || body["dependency_count"] != float64(0) {
		t.Fatalf("calls = %d, counts = %v/%v/%v", store.calls, body["workload_count"], body["platform_count"], body["dependency_count"])
	}
}

func TestContentReaderRepositoryReadModelCountsSkipsNames(t *testing.T) {
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{columns: []string{"scope_id"}, rows: [][]driver.Value{{"scope-one"}}, queryContains: []string{"FROM ingestion_scopes"}, wantArgs: []driver.Value{"repo-one"}},
		{columns: []string{"count"}, rows: [][]driver.Value{{int64(11)}}, queryContains: []string{"reducer_platform_materialization"}, wantArgs: []driver.Value{"scope-one"}},
		{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}, queryContains: []string{"FROM resolved_relationships"}, wantArgs: []driver.Value{"repo-one"}},
	})
	got, err := NewContentReader(db).RepositoryReadModelCounts(t.Context(), "repo-one")
	if err != nil || !got.Available || got.PlatformCount != 11 || got.DependencyCount != 0 {
		t.Fatalf("counts = %+v, error = %v", got, err)
	}
}

func TestContentReaderRepositoryReadModelCountsRequiredReadError(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []contentReaderQueryResult
	}{
		{name: "scope", results: []contentReaderQueryResult{{columns: []string{"scope_id"}, err: errors.New("scope failed")}}},
		{name: "platform", results: []contentReaderQueryResult{{columns: []string{"scope_id"}, rows: [][]driver.Value{{"scope-one"}}}, {columns: []string{"count"}, err: errors.New("platform failed")}}},
		{name: "dependency", results: []contentReaderQueryResult{{columns: []string{"scope_id"}, rows: [][]driver.Value{{"scope-one"}}}, {columns: []string{"count"}, rows: [][]driver.Value{{int64(11)}}}, {columns: []string{"count"}, err: errors.New("dependency failed")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openContentReaderTestDB(t, tc.results)
			got, err := NewContentReader(db).RepositoryReadModelCounts(t.Context(), "repo-one")
			if got.Available || err == nil || !strings.Contains(err.Error(), "failed") {
				t.Fatalf("counts = %+v, error = %v; want unavailable and read error", got, err)
			}
		})
	}
}

func TestRepositoryContextCountPortFailureUsesGraphWithoutSummaryRetry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		counts querycontract.RepositoryReadModelCounts
		err    error
	}{
		{name: "unavailable"},
		{name: "read error", err: errors.New("count read failed")},
		{name: "canceled read", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &contextCountPortStore{counts: tc.counts, err: tc.err}
			store.coverage = RepositoryContentCoverage{Available: true}
			var graphCounts int
			h := &RepositoryHandler{Content: store, Neo4j: fakeRepoGraphReader{
				runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
					return map[string]any{"id": "repo-one", "name": "one"}, nil
				},
				run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					if strings.Contains(cypher, "RETURN count(DISTINCT p) AS count") {
						graphCounts++
						return []map[string]any{{"count": int64(5)}}, nil
					}
					if strings.Contains(cypher, "RETURN count(DISTINCT dep) AS count") {
						graphCounts++
						return []map[string]any{{"count": int64(6)}}, nil
					}
					return []map[string]any{{"count": int64(0)}}, nil
				},
			}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-one/context", nil)
			req.SetPathValue("repo_id", "repo-one")
			w := httptest.NewRecorder()
			h.GetRepositoryContext(w, req)
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || store.calls != 1 || graphCounts != 2 || body["platform_count"] != float64(5) || body["dependency_count"] != float64(6) {
				t.Fatalf("status = %d, calls = %d, graph counts = %d, body = %v", w.Code, store.calls, graphCounts, body)
			}
		})
	}
}

func TestRepositoryContextCanceledCountReadPreservesGraphDeadline(t *testing.T) {
	store := &contextCountPortStore{err: context.Canceled}
	store.coverage = RepositoryContentCoverage{Available: true}
	var graphContext context.Context
	h := &RepositoryHandler{Content: store, Neo4j: fakeRepoGraphReader{
		runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"id": "repo-one", "name": "one"}, nil
		},
		run: func(ctx context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN count(DISTINCT w) AS count") {
				graphContext = ctx
				return nil, querycontract.ErrGraphReadDeadline
			}
			return []map[string]any{{"count": int64(0)}}, nil
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-one/context", nil).WithContext(ctx)
	req.SetPathValue("repo_id", "repo-one")
	w := httptest.NewRecorder()
	h.GetRepositoryContext(w, req)
	if w.Code != http.StatusGatewayTimeout || store.calls != 1 || graphContext != ctx {
		t.Fatalf("status = %d, count calls = %d, graph context same = %t; body = %s", w.Code, store.calls, graphContext == ctx, w.Body.String())
	}
}

func TestRepositoryContextCountPortLegacySummaryCompatibility(t *testing.T) {
	legacy := fakePortContentStore{summary: RepositoryReadModelSummary{
		Available: true, WorkloadNames: []string{"story-name"}, PlatformCount: 3, DependencyCount: 4,
	}}
	got := querycontract.LoadRepositoryContextCounts(t.Context(), legacy, "repo-one")
	if got == nil || !got.Available || got.PlatformCount != 3 || got.DependencyCount != 4 {
		t.Fatalf("legacy counts = %+v, want 3 platform and 4 dependency", got)
	}
	if got := querycontract.LoadRepositoryContextCounts(t.Context(), legacy, ""); got != nil {
		t.Fatalf("empty repo counts = %+v, want nil", got)
	}
}

func TestContentReaderRepositoryReadModelCountsEmptyAndDependencyOnly(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deps      int64
		available bool
	}{
		{name: "empty", deps: 0, available: false},
		{name: "dependency only", deps: 2, available: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openContentReaderTestDB(t, []contentReaderQueryResult{
				{columns: []string{"scope_id"}, rows: nil, queryContains: []string{"FROM ingestion_scopes"}},
				{columns: []string{"count"}, rows: [][]driver.Value{{tc.deps}}, queryContains: []string{"FROM resolved_relationships"}},
			})
			got, err := NewContentReader(db).RepositoryReadModelCounts(t.Context(), "repo-one")
			if err != nil || got.Available != tc.available || got.PlatformCount != 0 || got.DependencyCount != int(tc.deps) {
				t.Fatalf("counts = %+v, error = %v", got, err)
			}
		})
	}
	for _, reader := range []*ContentReader{nil, {}} {
		got, err := reader.RepositoryReadModelCounts(t.Context(), "repo-one")
		if err != nil || got.Available {
			t.Fatalf("nil reader or DB: counts = %+v, error = %v", got, err)
		}
	}
}

func TestContentReaderRepositoryReadModelCountsCanceledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := NewContentReader(openContentReaderTestDB(t, nil))
	got, err := reader.RepositoryReadModelCounts(ctx, "repo-one")
	if got.Available || !errors.Is(err, context.Canceled) {
		t.Fatalf("counts = %+v, error = %v; want canceled required read", got, err)
	}
}

func TestContentReaderRepositoryReadModelCountsRecordsBoundedSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	reader := NewContentReader(openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"scope_id"}, err: errors.New("scope unavailable"),
	}}))
	reader.tracer = provider.Tracer("repository-counts-test")
	if _, err := reader.RepositoryReadModelCounts(t.Context(), "private-repo-id"); err == nil {
		t.Fatal("required scope read error was lost")
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "postgres.query" {
		t.Fatalf("spans = %+v, want one postgres.query span", spans)
	}
	attrs := make(map[string]string)
	for _, attr := range spans[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	if attrs["db.system"] != "postgresql" || attrs["db.operation"] != "repository_context_counts" {
		t.Fatalf("span attributes = %+v", attrs)
	}
	if _, leaked := attrs["repo_id"]; leaked {
		t.Fatal("repository ID leaked into span attributes")
	}
	if events := spans[0].Events(); len(events) != 1 || events[0].Name != "exception" {
		t.Fatalf("span events = %+v, want one error event", events)
	}
}
