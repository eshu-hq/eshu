// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// deadLookupSelector is non-canonical, so resolution reaches the catalog read
// instead of short-circuiting on the canonical-id shape.
const deadLookupSelector = "payments-svc"

// TestHandleDeadIaCSelectorLookupFailureMapping is the #7626 regression for
// POST /api/v0/iac/dead. A catalog failure during selector resolution that is
// not a reader fence is a server fault: it answers 500 with the fixed body and
// records the error on the handler span, never the 400 that blamed the client
// and echoed the backend text. A fence keeps 503 with no span error, and an
// unmatched or ambiguous selector keeps this route's existing 400.
func TestHandleDeadIaCSelectorLookupFailureMapping(t *testing.T) {
	// Not parallel: swaps the package-global iacHandlerTracer.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := iacHandlerTracer
	iacHandlerTracer = provider.Tracer("iac-dead-selector-test")
	t.Cleanup(func() { iacHandlerTracer = previousTracer })

	ambiguous := content.FakePortContentStore{Repositories: []querycontract.RepositoryCatalogEntry{
		{ID: "repository:r_a", Name: deadLookupSelector},
		{ID: "repository:r_b", Name: deadLookupSelector},
	}}
	cases := []struct {
		name          string
		store         querycontract.ContentStore
		wantStatus    int
		wantSpanError bool
	}{
		{"plain store error", readerFenceContent{matchErr: errors.New(`private pq: relation "repositories" does not exist`)}, http.StatusInternalServerError, true},
		{"bare reader unavailable", readerFenceContent{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderUnavailable)}, http.StatusInternalServerError, true},
		{"reader stale", readerFenceContent{matchErr: fmt.Errorf("private read store: %w", db.ErrReaderStale)}, http.StatusServiceUnavailable, false},
		{"not found", readerFenceContent{}, http.StatusBadRequest, false},
		{"ambiguous", readerFenceContent{FakePortContentStore: ambiguous}, http.StatusBadRequest, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spansBefore := len(recorder.Ended())
			mux := http.NewServeMux()
			(&Handler{Content: tc.store}).Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead",
				bytes.NewBufferString(fmt.Sprintf(`{"repo_id":%q}`, deadLookupSelector)))
			req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, "private") {
				t.Fatalf("body leaked the backend error text: %s", body)
			}
			ended := recorder.Ended()[spansBefore:]
			if len(ended) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(ended))
			}
			assertDeadLookupSpan(t, ended[0], tc.wantSpanError)
			if tc.wantStatus != http.StatusInternalServerError {
				return
			}
			if !strings.Contains(body, selector.LookupFailureMessage) {
				t.Fatalf("body = %s, want the fixed %q message", body, selector.LookupFailureMessage)
			}
			if strings.Contains(body, deadLookupSelector) {
				t.Fatalf("500 body carries the raw selector: %s", body)
			}
		})
	}
}

func assertDeadLookupSpan(t *testing.T, span sdktrace.ReadOnlySpan, want bool) {
	t.Helper()
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
	if strings.Contains(span.Status().Description, deadLookupSelector) {
		t.Fatalf("span status description %q carries the raw selector", span.Status().Description)
	}
}
