// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestWriteResolveFailureAnswersSelectorAnswersWithTheirOwnText pins the
// #7674 body contract of the shared selector helper. An unmatched selector
// answers 404 and an ambiguous one 400, each with its typed error's text,
// which quotes only the caller's selector and the repository ids its grant
// may read. An error that is none of the classified answers is a server
// fault: it answers 500 with LookupFailureMessage and records the error on
// the request span, never echoing its text in a 400.
func TestWriteResolveFailureAnswersSelectorAnswersWithTheirOwnText(t *testing.T) {
	t.Parallel()

	notFound := NotFoundError{Selector: requestTestSelector}
	ambiguous := AmbiguousError{Selector: requestTestSelector, Matches: []string{"repository:r_a", "repository:r_b"}}
	cases := []struct {
		name          string
		err           error
		wantStatus    int
		wantMessage   string
		wantSpanError bool
	}{
		{"not found", notFound, http.StatusNotFound, notFound.Error(), false},
		{"ambiguous", ambiguous, http.StatusBadRequest, ambiguous.Error(), false},
		{"unclassified", errors.New("private: password=hunter2 MATCH (n)"), http.StatusInternalServerError, LookupFailureMessage, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			ctx, span := provider.Tracer("selector-server-failure-test").Start(context.Background(), "handler")
			req := httptest.NewRequest(http.MethodGet, "/route", nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			writeResolveFailure(rec, req, tc.err, "code_search.fuzzy_symbol")
			span.End()

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var body struct {
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Detail != tc.wantMessage {
				t.Fatalf("body = %s (decode err %v), want detail %q", rec.Body.String(), err, tc.wantMessage)
			}
			if strings.Contains(rec.Body.String(), "hunter2") {
				t.Fatalf("body leaked the backend error text: %s", rec.Body.String())
			}
			ended := recorder.Ended()[0]
			if got := ended.Status().Code == codes.Error; got != tc.wantSpanError {
				t.Fatalf("span status = %v (%q), want error recorded = %v",
					ended.Status().Code, ended.Status().Description, tc.wantSpanError)
			}
		})
	}
}
