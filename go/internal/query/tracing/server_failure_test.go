// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tracing

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
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	serverFailureMessage = "example read failed"
	serverFailureSecret  = "private pq: password authentication failed for user eshu"
)

// serverFailureCase is one input to the shared server-failure answer and the
// status, span status, and span events it must produce.
type serverFailureCase struct {
	name          string
	status        int
	err           error
	cancelRequest bool
	wantStatus    int
	wantSpanError bool
	wantCanceled  bool
}

func serverFailureCases() []serverFailureCase {
	return []serverFailureCase{
		{"backend failure answers 500", http.StatusInternalServerError, errors.New(serverFailureSecret), false, http.StatusInternalServerError, true, false},
		{"route budget answers 504", http.StatusGatewayTimeout, fmt.Errorf("%s: %w", serverFailureSecret, context.DeadlineExceeded), false, http.StatusGatewayTimeout, true, false},
		{"unsupported status answers 500", http.StatusBadRequest, errors.New(serverFailureSecret), false, http.StatusInternalServerError, true, false},
		{"client cancel answers 499", http.StatusInternalServerError, fmt.Errorf("%s: %w", serverFailureSecret, context.Canceled), true, querycontract.StatusClientClosedRequest, false, true},
		{"client cancel wins over 504", http.StatusGatewayTimeout, fmt.Errorf("%s: %w", serverFailureSecret, context.Canceled), true, querycontract.StatusClientClosedRequest, false, true},
		{"canceled error on a live request answers 500", http.StatusInternalServerError, fmt.Errorf("%s: %w", serverFailureSecret, context.Canceled), false, http.StatusInternalServerError, true, false},
		{"other error on a canceled request answers 500", http.StatusInternalServerError, errors.New(serverFailureSecret), true, http.StatusInternalServerError, true, false},
	}
}

// TestWriteServerFailure proves the writer answers a fixed body, never the
// backend text, marks the span as an error for a server fault, and reports a
// client cancel as 499 with an event instead of a span error.
func TestWriteServerFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range serverFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder, ctx, span := startServerFailureSpan(t)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if tc.cancelRequest {
				cancel()
			}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/example", nil).WithContext(ctx)
			rec := httptest.NewRecorder()

			WriteServerFailure(rec, req, tc.err, tc.status, serverFailureMessage)
			span.End()

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			assertServerFailureBody(t, rec.Body.String())
			assertServerFailureSpan(t, recorder.Ended(), tc)
		})
	}
}

// TestServerFailureEnvelope proves the envelope seam gives in-process callers
// the same status, fixed message, and span signal as the writer.
func TestServerFailureEnvelope(t *testing.T) {
	t.Parallel()

	for _, tc := range serverFailureCases() {
		if tc.status != http.StatusInternalServerError {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorder, ctx, span := startServerFailureSpan(t)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if tc.cancelRequest {
				cancel()
			}

			status, errEnv := ServerFailureEnvelope(ctx, tc.err, serverFailureMessage, "example.capability")
			span.End()

			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", status, tc.wantStatus)
			}
			if errEnv == nil {
				t.Fatal("errEnv = nil, want an envelope")
			}
			if errEnv.Code != querycontract.ErrorCodeInternalError {
				t.Fatalf("errEnv.Code = %q, want %q", errEnv.Code, querycontract.ErrorCodeInternalError)
			}
			if errEnv.Message != serverFailureMessage {
				t.Fatalf("errEnv.Message = %q, want the fixed %q", errEnv.Message, serverFailureMessage)
			}
			if errEnv.Capability != "example.capability" {
				t.Fatalf("errEnv.Capability = %q, want %q", errEnv.Capability, "example.capability")
			}
			assertServerFailureSpan(t, recorder.Ended(), tc)
		})
	}
}

func startServerFailureSpan(t *testing.T) (*tracetest.SpanRecorder, context.Context, trace.Span) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer(tracerName).Start(context.Background(), "query.example")
	return recorder, ctx, span
}

func assertServerFailureBody(t *testing.T, body string) {
	t.Helper()
	if !strings.Contains(body, serverFailureMessage) {
		t.Fatalf("body = %s, want the fixed %q", body, serverFailureMessage)
	}
	if strings.Contains(body, "private") {
		t.Fatalf("body = %s leaks the backend error text", body)
	}
}

func assertServerFailureSpan(t *testing.T, ended []sdktrace.ReadOnlySpan, tc serverFailureCase) {
	t.Helper()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	span := ended[0]
	hasException, hasCanceled := false, false
	for _, event := range span.Events() {
		switch event.Name {
		case "exception":
			hasException = true
		case ClientCanceledEvent:
			hasCanceled = true
			if len(event.Attributes) != 0 {
				t.Fatalf("%s event carries attributes %v, want none", ClientCanceledEvent, event.Attributes)
			}
		}
	}
	status := span.Status()
	if (status.Code == codes.Error) != tc.wantSpanError || hasException != tc.wantSpanError {
		t.Fatalf("span status = %v (%q), exception recorded = %v; want error recorded = %v",
			status.Code, status.Description, hasException, tc.wantSpanError)
	}
	if !tc.wantSpanError && status.Code != codes.Unset {
		t.Fatalf("span status = %v, want Unset for a client cancel", status.Code)
	}
	if tc.wantSpanError && status.Description != serverFailureMessage {
		t.Fatalf("span status description = %q, want the fixed %q", status.Description, serverFailureMessage)
	}
	if hasCanceled != tc.wantCanceled {
		t.Fatalf("%s event recorded = %v, want %v", ClientCanceledEvent, hasCanceled, tc.wantCanceled)
	}
}
