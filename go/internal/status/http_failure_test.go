// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

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
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const failureCanary = "pq: password authentication failed for user eshu host=db.internal.example"

// failureReader fails every snapshot read with err.
type failureReader struct{ err error }

func (r failureReader) ReadStatusSnapshot(context.Context, time.Time) (RawSnapshot, error) {
	return RawSnapshot{}, r.err
}

func (r failureReader) ReadStatusSnapshotFiltered(
	context.Context, time.Time, SnapshotSelection,
) (RawSnapshot, error) {
	return RawSnapshot{}, r.err
}

// spanRequest returns a GET request whose context carries a recording span, and
// the recorder that captures it.
func spanRequest(t *testing.T, target string) (*http.Request, *tracetest.SpanRecorder, trace.Span, context.CancelFunc) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	ctx, cancel := context.WithCancel(ctx)
	return httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx), recorder, span, cancel
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// TestServeHTTPLoadFailureHidesBackendText proves a failing reader answers the
// fixed load message, never the backend text, records the fault on the request
// span, and logs it server-side.
func TestServeHTTPLoadFailureHidesBackendText(t *testing.T) {
	logs := captureLogs(t)
	handler, err := NewHTTPHandler(failureReader{err: errors.New(failureCanary)}, HTTPHandlerOptions{})
	if err != nil {
		t.Fatalf("NewHTTPHandler() error = %v", err)
	}
	req, recorder, span, cancel := spanRequest(t, "/admin/status")
	defer cancel()
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	span.End()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "db.internal") {
		t.Fatalf("body leaks backend text: %q", rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != LoadFailureMessage {
		t.Fatalf("body = %q, want %q", got, LoadFailureMessage)
	}
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Status().Code != codes.Error {
		t.Fatalf("span status not Error: %+v", ended)
	}
	if !strings.Contains(logs.String(), "password authentication failed") {
		t.Fatalf("server-side log lost the error: %q", logs.String())
	}
}

// TestServeHTTPRenderFailureHidesBackendText proves a failing renderer answers
// the fixed render message and never the renderer's error text.
func TestServeHTTPRenderFailureHidesBackendText(t *testing.T) {
	captureLogs(t)
	opts := HTTPHandlerOptions{render: func(Report, string) ([]byte, string, error) {
		return nil, "", errors.New(failureCanary)
	}}
	handler, err := NewHTTPHandler(failureReader{}, opts)
	if err != nil {
		t.Fatalf("NewHTTPHandler() error = %v", err)
	}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/status", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("body leaks renderer text: %q", rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != RenderFailureMessage {
		t.Fatalf("body = %q, want %q", got, RenderFailureMessage)
	}
}

// TestServeHTTPClientCancelIsNotAServerFault proves a canceled request answers
// 499 with the fixed body, adds the client-cancel event, and leaves the span
// status and the server log untouched.
func TestServeHTTPClientCancelIsNotAServerFault(t *testing.T) {
	logs := captureLogs(t)
	err := fmt.Errorf("%s: %w", failureCanary, context.Canceled)
	handler, herr := NewHTTPHandler(failureReader{err: err}, HTTPHandlerOptions{})
	if herr != nil {
		t.Fatalf("NewHTTPHandler() error = %v", herr)
	}
	req, recorder, span, cancel := spanRequest(t, "/admin/status")
	cancel()
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	span.End()

	if rec.Code != StatusClientClosedRequest {
		t.Fatalf("status = %d, want %d", rec.Code, StatusClientClosedRequest)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("body leaks backend text: %q", rec.Body.String())
	}
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Status().Code == codes.Error {
		t.Fatalf("client cancel marked span as error: %+v", ended)
	}
	var sawCancel bool
	for _, ev := range ended[0].Events() {
		if ev.Name == "exception" {
			t.Fatal("client cancel recorded an exception event")
		}
		sawCancel = sawCancel || ev.Name == ClientCanceledEvent
	}
	if !sawCancel {
		t.Fatal("client cancel event missing")
	}
	if logs.Len() != 0 {
		t.Fatalf("client cancel logged as a fault: %q", logs.String())
	}
}

// TestWriteReadFailureCanceledErrorOnLiveRequestIsAFault proves a
// context.Canceled from an inner context on a live request stays a 500.
func TestWriteReadFailureCanceledErrorOnLiveRequestIsAFault(t *testing.T) {
	captureLogs(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	WriteReadFailure(rec, req, fmt.Errorf("inner: %w", context.Canceled), "boom failed")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
