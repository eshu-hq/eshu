// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	// The capability registry the route's profile gate reads is filled by
	// this package's init functions; without it every request answers 501.
	_ "github.com/eshu-hq/eshu/go/internal/query/contract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// lookupFailureSelector is a non-canonical selector, so resolution reaches the
// catalog and graph reads instead of short-circuiting on the canonical shape.
const lookupFailureSelector = "payments-svc"

// lookupFailureCatalog fails the selector's catalog read with err.
type lookupFailureCatalog struct {
	content.FakePortContentStore
	err error
}

func (f lookupFailureCatalog) MatchRepositories(context.Context, string) ([]querycontract.RepositoryCatalogEntry, error) {
	return nil, f.err
}

// TestLanguageQuerySelectorLookupFailureAnswers500 is the #7626 regression for
// POST /api/v0/code/language-query, which resolves repo_id through
// codequery.ApplyRepositorySelectorForAccess. A store or graph failure during
// resolution answers 500 with the fixed selector body and marks the route's
// span, instead of a 400 that echoed the backend error text. Fence verdicts
// keep 503/504 and an unmatched selector keeps the family's 400.
//
// Not parallel: it swaps the package-level languageHandlerTracer, the span
// this route starts and the one the lookup failure must land on.
func TestLanguageQuerySelectorLookupFailureAnswers500(t *testing.T) {
	runFails := func(err error) graph.FakeGraphReader {
		return graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, err
		}}
	}
	fallbackFails := graph.FakeGraphReader{
		RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) { return nil, nil },
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, errors.New("private neo4j: connection reset")
		},
	}
	cases := []struct {
		name          string
		graph         querycontract.GraphQuery
		content       querycontract.ContentStore
		wantStatus    int
		wantSpanError bool
	}{
		{"store plain sql error", nil, lookupFailureCatalog{err: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"graph plain driver error on ordered read", runFails(errors.New("private neo4j: connection reset")), nil, http.StatusInternalServerError, true},
		{"graph plain driver error on fallback read", fallbackFails, nil, http.StatusInternalServerError, true},
		{"store reader stale", nil, lookupFailureCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"graph unavailable", runFails(fmt.Errorf("private: %w", querycontract.ErrGraphUnavailable)), nil, http.StatusServiceUnavailable, false},
		{"graph deadline", runFails(fmt.Errorf("private: %w", querycontract.ErrGraphReadDeadline)), nil, http.StatusGatewayTimeout, false},
		{"not found", runFails(nil), nil, http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			previousTracer := languageHandlerTracer
			languageHandlerTracer = provider.Tracer("language-lookup-failure-test")
			t.Cleanup(func() { languageHandlerTracer = previousTracer })

			handler := &Handler{Neo4j: tc.graph, Content: tc.content, Profile: querycontract.ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
				strings.NewReader(`{"language":"go","entity_type":"function","query":"Foo","repo_id":"`+lookupFailureSelector+`"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			body := rec.Body.String()
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, body)
			}
			if tc.wantStatus == http.StatusInternalServerError {
				if !strings.Contains(body, selector.LookupFailureMessage) {
					t.Fatalf("body = %s, want the fixed %q message", body, selector.LookupFailureMessage)
				}
				if strings.Contains(body, "private") || strings.Contains(body, lookupFailureSelector) {
					t.Fatalf("body = %s leaks the backend error text or the raw selector", body)
				}
			}
			ended := recorder.Ended()
			if len(ended) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(ended))
			}
			hasException := false
			for _, event := range ended[0].Events() {
				if event.Name == "exception" {
					hasException = true
				}
			}
			status := ended[0].Status()
			if (status.Code == codes.Error) != tc.wantSpanError || hasException != tc.wantSpanError {
				t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
					status.Code, status.Description, hasException, tc.wantSpanError)
			}
			if tc.wantSpanError && status.Description != selector.LookupFailureMessage {
				t.Fatalf("span status description = %q, want the fixed %q", status.Description, selector.LookupFailureMessage)
			}
		})
	}
}
