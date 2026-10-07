// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

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

// lookupFailureCase is one selector-resolution outcome and the answer the
// route must give for it.
type lookupFailureCase struct {
	name          string
	graph         GraphQuery
	content       ContentStore
	wantStatus    int
	wantSpanError bool
}

// lookupFailureCases covers a plain backend failure on each of the selector's
// three reads (500), the fence and graph-availability verdicts (503/504), and
// an unmatched selector, which this family answers with 400.
func lookupFailureCases() []lookupFailureCase {
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
	return []lookupFailureCase{
		{"store plain sql error", nil, lookupFailureCatalog{err: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"store bare reader unavailable", nil, lookupFailureCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, http.StatusInternalServerError, true},
		{"graph plain driver error on ordered read", runFails(errors.New("private neo4j: connection reset")), nil, http.StatusInternalServerError, true},
		{"graph plain driver error on fallback read", fallbackFails, nil, http.StatusInternalServerError, true},
		{"store reader stale", nil, lookupFailureCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"graph unavailable", runFails(fmt.Errorf("private: %w", querycontract.ErrGraphUnavailable)), nil, http.StatusServiceUnavailable, false},
		{"graph deadline", runFails(fmt.Errorf("private: %w", querycontract.ErrGraphReadDeadline)), nil, http.StatusGatewayTimeout, false},
		{"not found", runFails(nil), nil, http.StatusBadRequest, false},
	}
}

// TestCodeRouteSelectorLookupFailureAnswers500 is the #7626 regression for the
// CodeHandler routes that resolve repo_id through
// applyRepositorySelectorForCapability. A store or graph failure during
// resolution is a server fault, so it answers 500 with the fixed selector body
// and records the error on the request span, instead of the 400 that told the
// client its request was malformed and echoed the backend error text.
func TestCodeRouteSelectorLookupFailureAnswers500(t *testing.T) {
	t.Parallel()

	for _, tc := range lookupFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ctx, span := provider.Tracer("code-lookup-failure-test").Start(context.Background(), "request")

			handler := &CodeHandler{Neo4j: tc.graph, Content: tc.content, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search",
				bytes.NewBufferString(`{"query":"handler","repo_id":"`+lookupFailureSelector+`"}`)).WithContext(ctx)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			span.End()

			assertSelectorLookupAnswer(t, rec, recorder.Ended(), tc.wantStatus, tc.wantSpanError)
		})
	}
}

// assertSelectorLookupAnswer checks the status, that a 500 carries only the
// fixed selector message, and that exactly the lookup failures mark the span.
func assertSelectorLookupAnswer(t *testing.T, rec *httptest.ResponseRecorder, ended []sdktrace.ReadOnlySpan, wantStatus int, wantSpanError bool) {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, wantStatus, body)
	}
	if wantStatus == http.StatusInternalServerError {
		if !strings.Contains(body, selector.LookupFailureMessage) {
			t.Fatalf("body = %s, want the fixed %q message", body, selector.LookupFailureMessage)
		}
		if strings.Contains(body, "private") || strings.Contains(body, lookupFailureSelector) {
			t.Fatalf("body = %s leaks the backend error text or the raw selector", body)
		}
	}
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
	if (status.Code == codes.Error) != wantSpanError || hasException != wantSpanError {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			status.Code, status.Description, hasException, wantSpanError)
	}
	if wantSpanError && status.Description != selector.LookupFailureMessage {
		t.Fatalf("span status description = %q, want the fixed %q", status.Description, selector.LookupFailureMessage)
	}
}
