// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// requestTestSelector is the raw, non-canonical selector the request tests
// send, so resolution reaches the catalog and graph reads instead of
// short-circuiting on LooksCanonicalRepositoryID.
const requestTestSelector = "payments-svc"

// failingCatalog fails the catalog read with err.
type failingCatalog struct {
	content.FakePortContentStore
	err error
}

func (f failingCatalog) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, f.err
}

func graphRunFailing(err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
		return nil, err
	}}
}

// graphFallbackFailing answers the ordered read with no rows and fails the
// RunSingle fallback read, the second of the two graph lookups.
func graphFallbackFailing(err error) graph.FakeGraphReader {
	return graph.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, nil
		},
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, err
		},
	}
}

// TestResolveForRequestWithAccessMapsLookupFailureTo500 is the #7626
// regression for the shared request helper. A store or graph failure that is
// not a reader fence or graph-availability verdict is a server fault, so it
// answers 500 with a fixed body and records the error on the request span,
// instead of the 400 that told the client its request was malformed. Fence
// verdicts keep 503/504, an unmatched selector keeps 404, an ambiguous one
// keeps 400, and none of those touch the span.
func TestResolveForRequestWithAccessMapsLookupFailureTo500(t *testing.T) {
	t.Parallel()

	ambiguous := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: requestTestSelector},
		{ID: "repository:r_b", Name: requestTestSelector},
	}}
	cases := []struct {
		name          string
		graph         querycontract.GraphQuery
		content       querycontract.ContentStore
		wantStatus    int
		wantSpanError bool
	}{
		{"store bare reader unavailable", nil, failingCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, http.StatusInternalServerError, true},
		{"store plain sql error", nil, failingCatalog{err: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"graph plain driver error on ordered read", graphRunFailing(errors.New("private neo4j: connection reset")), nil, http.StatusInternalServerError, true},
		{"graph plain driver error on fallback read", graphFallbackFailing(errors.New("private neo4j: connection reset")), nil, http.StatusInternalServerError, true},
		{"store reader stale", nil, failingCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"graph unavailable", graphRunFailing(fmt.Errorf("private: %w", querycontract.ErrGraphUnavailable)), nil, http.StatusServiceUnavailable, false},
		{"graph deadline", graphRunFailing(fmt.Errorf("private: %w", querycontract.ErrGraphReadDeadline)), nil, http.StatusGatewayTimeout, false},
		{"not found", graphRunFailing(nil), nil, http.StatusNotFound, false},
		{"ambiguous", nil, ambiguous, http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ctx, span := provider.Tracer("selector-request-test").Start(context.Background(), "handler")

			req := httptest.NewRequest(http.MethodGet, "/route?repository_id="+requestTestSelector, nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			repoID, ok := ResolveForRequestWithAccess(rec, req, tc.graph, tc.content, requestTestSelector,
				querycontract.RepositoryAccessFilter{AllScopes: true}, "code_search.fuzzy_symbol")
			span.End()

			if ok || repoID != "" {
				t.Fatalf("ResolveForRequestWithAccess() = (%q, %v), want (\"\", false)", repoID, ok)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			ended := recorder.Ended()
			if len(ended) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(ended))
			}
			assertRequestSpanError(t, ended[0], tc.wantSpanError)
			if tc.wantStatus != http.StatusInternalServerError {
				return
			}
			body := rec.Body.String()
			if !strings.Contains(body, lookupFailureMessage) {
				t.Fatalf("body = %s, want the fixed %q message", body, lookupFailureMessage)
			}
			if strings.Contains(body, requestTestSelector) || strings.Contains(body, "private") {
				t.Fatalf("body = %s leaks the raw selector or the backend error text", body)
			}
		})
	}
}

func assertRequestSpanError(t *testing.T, span sdktrace.ReadOnlySpan, want bool) {
	t.Helper()
	hasException := false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			hasException = true
		}
	}
	isError := span.Status().Code == codes.Error
	if isError != want || hasException != want {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			span.Status().Code, span.Status().Description, hasException, want)
	}
	if want && span.Status().Description != lookupFailureMessage {
		t.Fatalf("span status description = %q, want the fixed %q", span.Status().Description, lookupFailureMessage)
	}
	if strings.Contains(span.Status().Description, requestTestSelector) {
		t.Fatalf("span status description %q carries the raw selector", span.Status().Description)
	}
}
