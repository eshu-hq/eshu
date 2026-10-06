// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
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
	"github.com/eshu-hq/eshu/go/internal/query/service"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// serviceLookupFailureCase is one outcome of resolving the service routes'
// repo selector and the answer every service route must give for it.
type serviceLookupFailureCase struct {
	name          string
	graph         querycontract.GraphQuery
	content       querycontract.ContentStore
	wantStatus    int
	wantSpanError bool
}

func serviceLookupFailureCases() []serviceLookupFailureCase {
	ambiguous := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: lookupFailureSelector},
		{ID: "repository:r_b", Name: lookupFailureSelector},
	}}
	return []serviceLookupFailureCase{
		{"store plain sql error", nil, lookupFailureCatalog{err: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"store bare reader unavailable", nil, lookupFailureCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, http.StatusInternalServerError, true},
		{"graph plain driver error", graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, errors.New("private neo4j: connection reset")
		}}, nil, http.StatusInternalServerError, true},
		{"store reader stale", nil, lookupFailureCatalog{err: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"not found", nil, content.FakePortContentStore{}, http.StatusNotFound, false},
		{"ambiguous", nil, ambiguous, http.StatusConflict, false},
	}
}

// TestServiceRoutesSelectorLookupFailureAnswers500 is the #7626 regression for
// the service investigation and service story routes, which resolve the
// optional repo selector through selector.ResolveExactForAccess. A store or
// graph failure during that resolution already answered 500, but its body was
// "query failed: " plus the LookupError text, which carries the backend error,
// and the request span recorded nothing. It must answer the fixed selector
// body and mark the span; fence, not-found, and ambiguous answers are unchanged.
func TestServiceRoutesSelectorLookupFailureAnswers500(t *testing.T) {
	t.Parallel()

	routes := []string{
		"/api/v0/investigations/services/payments?repo=" + lookupFailureSelector,
		"/api/v0/services/payments/story?repo=" + lookupFailureSelector,
	}
	for _, route := range routes {
		for _, tc := range serviceLookupFailureCases() {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				recorder := tracetest.NewSpanRecorder()
				provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
				t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
				ctx, span := provider.Tracer("service-lookup-failure-test").Start(context.Background(), "request")

				handler := &Handler{Neo4j: tc.graph, Content: tc.content, Profile: querycontract.ProfileLocalAuthoritative}
				mux := http.NewServeMux()
				handler.Mount(mux)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, route, nil).WithContext(ctx))
				span.End()

				assertServiceLookupAnswer(t, rec.Code, rec.Body.String(), recorder.Ended(), tc)
			})
		}
	}
}

// TestBuildServiceStoryEnvelopeSelectorLookupFailure covers the envelope seam
// directly, the path in-process callers such as the service intelligence
// report use without an HTTP response writer.
func TestBuildServiceStoryEnvelopeSelectorLookupFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range serviceLookupFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ctx, span := provider.Tracer("service-story-seam-lookup-failure-test").Start(context.Background(), "request")

			handler := &Handler{Neo4j: tc.graph, Content: tc.content, Profile: querycontract.ProfileLocalAuthoritative}
			_, _, status, errEnv := handler.BuildServiceStoryEnvelope(ctx, service.WorkloadSelector{
				ServiceName: "payments",
				Repository:  lookupFailureSelector,
			}, "service_story")
			span.End()

			if errEnv == nil {
				t.Fatalf("errEnv = nil, want an error envelope (status %d)", status)
			}
			assertServiceLookupAnswer(t, status, errEnv.Message, recorder.Ended(), tc)
			if tc.wantStatus == http.StatusInternalServerError && errEnv.Code != querycontract.ErrorCodeInternalError {
				t.Fatalf("errEnv.Code = %q, want %q", errEnv.Code, querycontract.ErrorCodeInternalError)
			}
		})
	}
}

// assertServiceLookupAnswer checks the status, that a 500 carries only the
// fixed selector message, and that exactly the lookup failures mark the span.
func assertServiceLookupAnswer(t *testing.T, status int, body string, ended []sdktrace.ReadOnlySpan, tc serviceLookupFailureCase) {
	t.Helper()
	if status != tc.wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", status, tc.wantStatus, body)
	}
	if tc.wantStatus == http.StatusInternalServerError {
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
	spanStatus := ended[0].Status()
	if (spanStatus.Code == codes.Error) != tc.wantSpanError || hasException != tc.wantSpanError {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			spanStatus.Code, spanStatus.Description, hasException, tc.wantSpanError)
	}
	if tc.wantSpanError && spanStatus.Description != selector.LookupFailureMessage {
		t.Fatalf("span status description = %q, want the fixed %q", spanStatus.Description, selector.LookupFailureMessage)
	}
}
