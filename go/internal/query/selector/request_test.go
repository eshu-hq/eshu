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
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
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
			if !strings.Contains(body, LookupFailureMessage) {
				t.Fatalf("body = %s, want the fixed %q message", body, LookupFailureMessage)
			}
			if strings.Contains(body, requestTestSelector) || strings.Contains(body, "private") {
				t.Fatalf("body = %s leaks the raw selector or the backend error text", body)
			}
		})
	}
}

// TestWriteLookupFailureAnswersOnlyLookupFailures pins the helper every
// selector caller outside ResolveForRequestWithAccess uses (#7626): a
// LookupError, bare or wrapped, answers 500 with the fixed body and records the
// error on the request span; every other error writes nothing, touches no span,
// and reports false so the caller's own 404/400 mapping runs.
func TestWriteLookupFailureAnswersOnlyLookupFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		err   error
		wantW bool
	}{
		{"lookup error", LookupError{Err: errors.New("private neo4j: connection reset")}, true},
		{"wrapped lookup error", fmt.Errorf("resolve: %w", LookupError{Err: errors.New("private pq: boom")}), true},
		{"not found", NotFoundError{Selector: requestTestSelector}, false},
		{"ambiguous", AmbiguousError{Selector: requestTestSelector, Matches: []string{"a", "b"}}, false},
		{"plain error", errors.New("private other"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ctx, span := provider.Tracer("selector-request-test").Start(context.Background(), "handler")

			req := httptest.NewRequest(http.MethodGet, "/route", nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			wrote := WriteLookupFailure(rec, req, tc.err)
			span.End()

			if wrote != tc.wantW {
				t.Fatalf("WriteLookupFailure() = %v, want %v", wrote, tc.wantW)
			}
			assertRequestSpanError(t, recorder.Ended()[0], tc.wantW)
			body := rec.Body.String()
			if !tc.wantW {
				if body != "" {
					t.Fatalf("body = %q, want nothing written", body)
				}
				return
			}
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			if !strings.Contains(body, LookupFailureMessage) || strings.Contains(body, "private") {
				t.Fatalf("body = %s, want the fixed %q message and no backend text", body, LookupFailureMessage)
			}
		})
	}
}

// TestWriteLookupFailureClientCancelAnswers499 proves a lookup that failed
// because the caller canceled its own request is not a server fault: it
// answers 499 with the fixed body, leaves the span status unset, records no
// exception, and adds only the client-cancel event.
func TestWriteLookupFailureClientCancelAnswers499(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("selector-request-test").Start(context.Background(), "handler")
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/route", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	err := LookupError{Err: fmt.Errorf("private match repositories: %w", context.Canceled)}
	if !WriteLookupFailure(rec, req, err) {
		t.Fatal("WriteLookupFailure() = false, want true for a LookupError")
	}
	span.End()

	if rec.Code != querycontract.StatusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, querycontract.StatusClientClosedRequest)
	}
	if body := rec.Body.String(); !strings.Contains(body, LookupFailureMessage) || strings.Contains(body, "private") {
		t.Fatalf("body = %s, want the fixed %q message and no backend text", body, LookupFailureMessage)
	}
	ended := recorder.Ended()[0]
	if ended.Status().Code != codes.Unset {
		t.Fatalf("span status = %v (%q), want Unset for a client cancel", ended.Status().Code, ended.Status().Description)
	}
	canceled := false
	for _, event := range ended.Events() {
		if event.Name == "exception" {
			t.Fatal("exception event recorded for a client cancel")
		}
		if event.Name == tracing.ClientCanceledEvent {
			canceled = true
		}
	}
	if !canceled {
		t.Fatalf("%s event missing", tracing.ClientCanceledEvent)
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
	if want && span.Status().Description != LookupFailureMessage {
		t.Fatalf("span status description = %q, want the fixed %q", span.Status().Description, LookupFailureMessage)
	}
	if strings.Contains(span.Status().Description, requestTestSelector) {
		t.Fatalf("span status description %q carries the raw selector", span.Status().Description)
	}
}
