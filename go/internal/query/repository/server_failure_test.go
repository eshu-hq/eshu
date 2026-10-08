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

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// failingRepoReadStore fails the reads the stats and coverage routes issue
// after the selector resolved: the stats repository lookup and the coverage
// read.
type failingRepoReadStore struct {
	content.FakePortContentStore
	err error
}

func (s failingRepoReadStore) ResolveRepository(context.Context, string) (*querycontract.RepositoryCatalogEntry, error) {
	return nil, s.err
}

func (s failingRepoReadStore) RepositoryCoverage(context.Context, string) (querycontract.RepositoryContentCoverage, error) {
	return querycontract.RepositoryContentCoverage{}, s.err
}

type repoServerFailureCase struct {
	name          string
	err           error
	cancel        bool
	wantStatus    int
	wantSpanError bool
}

func repoServerFailureCases(deadlineStatus int) []repoServerFailureCase {
	return []repoServerFailureCase{
		{"backend failure", errors.New(`private pq: relation "content_files" does not exist`), false, http.StatusInternalServerError, true},
		{"context deadline", fmt.Errorf("private read store: %w", context.DeadlineExceeded), false, deadlineStatus, true},
		{"reader stale", fmt.Errorf("private read store: %w", db.ErrReaderStale), false, http.StatusServiceUnavailable, false},
		{"client cancel", fmt.Errorf("private read store: %w", context.Canceled), true, querycontract.StatusClientClosedRequest, false},
	}
}

// TestRepositoryReadServerFailureAnswersFixedMessage is the #7626 regression
// for the stats and coverage reads that run after the selector resolved. They
// answered 500 (stats: 504 on its own budget) with the backend error text in
// the body and left the span untouched. They must answer a fixed message with
// a span error, keep the stats 504, map a reader fence to the retryable 503,
// and answer a client cancel with 499 and no span error.
func TestRepositoryReadServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name           string
		path           string
		message        string
		deadlineStatus int
	}{
		{"stats", "/api/v0/repositories/repository:r_ok/stats", repositoryStatsQueryFailedMessage, http.StatusGatewayTimeout},
		{"coverage", "/api/v0/repositories/repository:r_ok/coverage", repositoryCoverageQueryFailedMessage, http.StatusInternalServerError},
	}
	for _, route := range routes {
		for _, tc := range repoServerFailureCases(route.deadlineStatus) {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				serveRepoServerFailure(t, &Handler{Content: failingRepoReadStore{err: tc.err}}, route.path, route.message, tc)
			})
		}
	}
}

// TestRepositoryCoverageSecondResolutionSelectorAnswer covers the coverage
// route's second resolution returning a selector answer instead of a lookup
// failure: it answered 500 with "query failed: " and the selector error text.
// It now answers the fixed coverage message.
func TestRepositoryCoverageSecondResolutionSelectorAnswer(t *testing.T) {
	t.Parallel()

	calls := &atomic.Int32{}
	store := secondCallEmptyCatalog{
		FakePortContentStore: content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
			{ID: "payments-id", Name: repoLookupSelector},
		}},
		calls: calls,
	}
	serveRepoServerFailure(t, &Handler{Content: store}, "/api/v0/repositories/"+repoLookupSelector+"/coverage",
		repositoryCoverageQueryFailedMessage, repoServerFailureCase{wantStatus: http.StatusInternalServerError, wantSpanError: true})
	if got := calls.Load(); got != 2 {
		t.Fatalf("catalog reads = %d, want 2", got)
	}
}

// secondCallEmptyCatalog answers the first catalog read from the fixture and
// every later one with no match.
type secondCallEmptyCatalog struct {
	content.FakePortContentStore
	calls *atomic.Int32
}

func (s secondCallEmptyCatalog) MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error) {
	if s.calls.Add(1) > 1 {
		return nil, nil
	}
	return s.FakePortContentStore.MatchRepositories(ctx, selector)
}

func serveRepoServerFailure(t *testing.T, handler *Handler, path, message string, tc repoServerFailureCase) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("repository-server-failure-test").Start(context.Background(), "http")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if tc.cancel {
		cancel()
	}

	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	span.End()

	if rec.Code != tc.wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "private") || strings.Contains(body, "did not match") {
		t.Fatalf("body leaked backend or selector error text: %s", body)
	}
	if tc.wantStatus == http.StatusServiceUnavailable {
		if rec.Header().Get("Retry-After") == "" {
			t.Fatal("reader fence 503 is missing Retry-After")
		}
	} else if !strings.Contains(body, message) {
		t.Fatalf("body = %s, want the fixed %q", body, message)
	}

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	hasException, hasCanceled := false, false
	for _, event := range ended[0].Events() {
		hasException = hasException || event.Name == "exception"
		hasCanceled = hasCanceled || event.Name == tracing.ClientCanceledEvent
	}
	status := ended[0].Status()
	if (status.Code == codes.Error) != tc.wantSpanError || hasException != tc.wantSpanError {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			status.Code, status.Description, hasException, tc.wantSpanError)
	}
	if tc.wantSpanError && status.Description != message {
		t.Fatalf("span status description = %q, want the fixed %q", status.Description, message)
	}
	if hasCanceled != tc.cancel {
		t.Fatalf("%s event recorded = %v, want %v", tracing.ClientCanceledEvent, hasCanceled, tc.cancel)
	}
}
