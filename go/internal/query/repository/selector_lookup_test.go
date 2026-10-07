// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// repoLookupSelector is non-canonical, so resolution reaches the catalog and
// graph reads instead of short-circuiting on the canonical-id shape.
const repoLookupSelector = "payments-svc"

// repoLookupStore fails the selector's catalog read with matchErr from the
// failFrom-th call on (1-based; 0 fails from the first call), and otherwise
// answers from the embedded fixture catalog.
type repoLookupStore struct {
	content.FakePortContentStore
	matchErr error
	failFrom int32
	calls    *atomic.Int32
}

func (s repoLookupStore) MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error) {
	call := int32(1)
	if s.calls != nil {
		call = s.calls.Add(1)
	}
	if s.matchErr != nil && call >= max(s.failFrom, 1) {
		return nil, s.matchErr
	}
	return s.FakePortContentStore.MatchRepositories(ctx, selector)
}

func repoLookupGraph(err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, err
		},
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, err
		},
	}
}

type repoLookupCase struct {
	name          string
	graph         querycontract.GraphQuery
	store         querycontract.ContentStore
	wantStatus    int
	wantSpanError bool
}

func repoLookupCases(deadlineStatus int) []repoLookupCase {
	ambiguous := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: repoLookupSelector},
		{ID: "repository:r_b", Name: repoLookupSelector},
	}}
	return []repoLookupCase{
		{"plain store error", nil, repoLookupStore{matchErr: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"bare reader unavailable", nil, repoLookupStore{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, http.StatusInternalServerError, true},
		{"plain graph driver error", repoLookupGraph(errors.New("private neo4j: connection reset")), nil, http.StatusInternalServerError, true},
		{"store context deadline", nil, repoLookupStore{matchErr: fmt.Errorf("private read store: %w", context.DeadlineExceeded)}, deadlineStatus, deadlineStatus == http.StatusInternalServerError},
		{"reader stale", nil, repoLookupStore{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"graph unavailable", repoLookupGraph(fmt.Errorf("private: %w", querycontract.ErrGraphUnavailable)), nil, http.StatusServiceUnavailable, false},
		{"graph deadline", repoLookupGraph(fmt.Errorf("private: %w", querycontract.ErrGraphReadDeadline)), nil, http.StatusGatewayTimeout, false},
		{"not found", nil, content.FakePortContentStore{}, http.StatusNotFound, false},
		{"ambiguous", nil, ambiguous, http.StatusBadRequest, false},
	}
}

// TestRepositorySelectorLookupFailureMapping is the #7626 regression for the
// repository {repo_id} routes. A catalog or graph failure during selector
// resolution that is not a fence or graph-availability verdict is a server
// fault: it answers 500 with the fixed body and records the error on the
// request span, never the 400 that blamed the client and echoed backend text.
// Fence verdicts keep 503/504, an unmatched selector keeps 404, and an
// ambiguous one keeps 400. The stats route keeps its existing 504 for a
// selector read that ran out its route budget (context.DeadlineExceeded).
func TestRepositorySelectorLookupFailureMapping(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name           string
		path           string
		deadlineStatus int
	}{
		{"content", "/api/v0/repositories/" + repoLookupSelector + "/content?path=src/app.go", http.StatusInternalServerError},
		{"tree", "/api/v0/repositories/" + repoLookupSelector + "/tree", http.StatusInternalServerError},
		{"coverage", "/api/v0/repositories/" + repoLookupSelector + "/coverage", http.StatusInternalServerError},
		{"stats", "/api/v0/repositories/" + repoLookupSelector + "/stats", http.StatusGatewayTimeout},
	}
	for _, route := range routes {
		for _, tc := range repoLookupCases(route.deadlineStatus) {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				serveRepoLookup(t, &Handler{Neo4j: tc.graph, Content: tc.store}, route.path, tc.wantStatus, tc.wantSpanError)
			})
		}
	}
}

// TestRepositoryCoverageSecondResolutionLookupFailure proves a LookupError can
// reach getRepositoryCoverage's own resolveCoverageRepositoryID call: the
// path selector resolves to a catalog id that is not canonical-shaped, so the
// second resolution reads the catalog again, and that read fails. It answers
// the same fixed 500 as the first resolution, not "query failed: <backend
// text>".
func TestRepositoryCoverageSecondResolutionLookupFailure(t *testing.T) {
	t.Parallel()

	calls := &atomic.Int32{}
	store := repoLookupStore{
		FakePortContentStore: content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
			{ID: "payments-id", Name: repoLookupSelector},
		}},
		matchErr: errors.New("private pq: connection reset by peer"),
		failFrom: 2,
		calls:    calls,
	}
	serveRepoLookup(t, &Handler{Content: store}, "/api/v0/repositories/"+repoLookupSelector+"/coverage",
		http.StatusInternalServerError, true)
	if got := calls.Load(); got != 2 {
		t.Fatalf("catalog reads = %d, want 2 (the path selector, then the coverage re-resolution)", got)
	}
}

func serveRepoLookup(t *testing.T, handler *Handler, path string, wantStatus int, wantSpanError bool) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("repository-selector-test").Start(context.Background(), "http")

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	span.End()

	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "private") {
		t.Fatalf("body leaked the backend error text: %s", body)
	}
	assertRepoLookupSpan(t, recorder.Ended(), wantSpanError)
	if wantStatus != http.StatusInternalServerError {
		return
	}
	if !strings.Contains(body, selector.LookupFailureMessage) {
		t.Fatalf("body = %s, want the fixed %q message", body, selector.LookupFailureMessage)
	}
	if strings.Contains(body, repoLookupSelector) {
		t.Fatalf("500 body carries the raw selector: %s", body)
	}
}

func assertRepoLookupSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, want bool) {
	t.Helper()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	span := ended[0]
	hasException := false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			hasException = true
		}
	}
	if isError := span.Status().Code == codes.Error; isError != want || hasException != want {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			span.Status().Code, span.Status().Description, hasException, want)
	}
	if strings.Contains(span.Status().Description, repoLookupSelector) {
		t.Fatalf("span status description %q carries the raw selector", span.Status().Description)
	}
}
