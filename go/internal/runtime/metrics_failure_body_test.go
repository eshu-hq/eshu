// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const metricsFailureCanary = "pq: password authentication failed for user eshu host=db.internal.example"

// captureMetricsFailureLogs routes the default slog logger into a buffer for
// the test, so the canary error text does not reach test stderr.
func captureMetricsFailureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// TestStatusMetricsHandlerReadFailureHidesBackendText proves a failing status
// reader answers the fixed metrics message, never the backend text, records
// the fault (exception event and Error status) on the request span, and logs
// it server-side.
func TestStatusMetricsHandlerReadFailureHidesBackendText(t *testing.T) {
	logs := captureMetricsFailureLogs(t)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	handler, err := NewStatusMetricsHandler("api", &fakeStatusReader{err: errors.New(metricsFailureCanary)})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v", err)
	}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx))
	span.End()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "db.internal") {
		t.Fatalf("body leaks backend text: %q", rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != metricsLoadFailureMessage {
		t.Fatalf("body = %q, want %q", got, metricsLoadFailureMessage)
	}
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Status().Code != codes.Error {
		t.Fatalf("span status not Error: %+v", ended)
	}
	var sawException bool
	for _, ev := range ended[0].Events() {
		sawException = sawException || ev.Name == "exception"
	}
	if !sawException {
		t.Fatalf("span lost the exception event: %+v", ended[0].Events())
	}
	if !strings.Contains(logs.String(), "password authentication failed") {
		t.Fatalf("server-side log lost the error: %q", logs.String())
	}
}

// TestStatusMetricsHandlerDeadlineExceededIsAServerFault proves a read that
// hit its deadline answers 500 with the fixed body and a server-side log, not
// the 499 reserved for a caller cancel.
func TestStatusMetricsHandlerDeadlineExceededIsAServerFault(t *testing.T) {
	logs := captureMetricsFailureLogs(t)
	err := fmt.Errorf("%s: %w", metricsFailureCanary, context.DeadlineExceeded)
	handler, herr := NewStatusMetricsHandler("api", &fakeStatusReader{err: err})
	if herr != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v", herr)
	}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != metricsLoadFailureMessage {
		t.Fatalf("body = %q, want %q", got, metricsLoadFailureMessage)
	}
	if !strings.Contains(logs.String(), "password authentication failed") {
		t.Fatalf("deadline fault was not logged: %q", logs.String())
	}
}

// TestStatusMetricsHandlerClientCancelIsNotAServerFault proves a canceled
// scrape answers 499, not a 500 server fault.
func TestStatusMetricsHandlerClientCancelIsNotAServerFault(t *testing.T) {
	logs := captureMetricsFailureLogs(t)
	err := fmt.Errorf("%s: %w", metricsFailureCanary, context.Canceled)
	handler, herr := NewStatusMetricsHandler("api", &fakeStatusReader{err: err})
	if herr != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v", herr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil).WithContext(ctx))

	if rec.Code != statuspkg.StatusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, statuspkg.StatusClientClosedRequest)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("body leaks backend text: %q", rec.Body.String())
	}
	if logs.Len() != 0 {
		t.Fatalf("client cancel logged as a fault: %q", logs.String())
	}
}
