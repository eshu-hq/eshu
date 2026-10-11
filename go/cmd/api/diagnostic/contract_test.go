// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package diagnostic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func newStatusDiagnosticFixture(t *testing.T) (*Collector, string, *sdktrace.TracerProvider) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	provider := sdktrace.NewTracerProvider()
	diag, err := New(func(string) string { return path }, provider)
	if err != nil {
		t.Fatal(err)
	}
	return diag, path, provider
}

func readStatusDiagnosticReport(t *testing.T, path string) statusDiagnosticReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report statusDiagnosticReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestStatusDiagnosticsConcurrentCorrelationAndPrivacy(t *testing.T) {
	diag, path, provider := newStatusDiagnosticFixture(t)
	tracer := provider.Tracer("fixture")
	inner := diag.PostAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryCtx := db.WithQuerySummary(r.Context(), "PRIVATE_QUERY_TEXT")
		recordStatusDiagnosticStages(r.Context(), tracer)
		_, span := tracer.Start(queryCtx, "postgres.reader_access", trace.WithAttributes(
			attribute.String("role", "reader"), attribute.String("stage", "reader_borrow"),
			attribute.String("outcome", "ok"), attribute.String("secret", "DO_NOT_COPY")))
		span.End()
		w.WriteHeader(http.StatusOK)
	}))
	handler := diag.Middleware(otelhttp.NewHandler(inner, "eshu-api", otelhttp.WithTracerProvider(provider)))
	var group sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, statusDiagnosticPath, nil))
			if recorder.Code != http.StatusOK {
				t.Errorf("status = %d", recorder.Code)
			}
			ids <- recorder.Header().Get(statusDiagnosticHeader)
		}()
	}
	group.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if _, err := strconv.ParseUint(id, 10, 64); err != nil || id == "0" || seen[id] {
			t.Fatalf("invalid or duplicate id %q", id)
		}
		seen[id] = true
	}
	if err := diag.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	report := readStatusDiagnosticReport(t, path)
	if !report.Complete || len(report.Requests) != 20 || len(report.Spans) != 140 {
		t.Fatalf("incomplete correlation: complete=%v requests=%d spans=%d errors=%v", report.Complete, len(report.Requests), len(report.Spans), report.Errors)
	}
	roots := map[string]string{}
	for _, span := range report.Spans {
		if span.Name == statusDiagnosticRoot {
			roots[span.RequestID] = span.SpanID
		}
		if span.Role == "reader" && span.Stage != "reader_borrow" {
			t.Fatalf("stage %q", span.Stage)
		}
	}
	for _, span := range report.Spans {
		if span.Name == "eshu-api" && span.ParentSpanID != roots[span.RequestID] {
			t.Fatalf("server span detached from request %s", span.RequestID)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "DO_NOT_COPY") || strings.Contains(string(data), "secret") || strings.Contains(string(data), "PRIVATE_QUERY_TEXT") {
		t.Fatal("private span attribute leaked")
	}
}

func TestStatusDiagnosticsAuthAndCheckpointFailures(t *testing.T) {
	diag, path, _ := newStatusDiagnosticFixture(t)
	checkpoint := diag.PostAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	handler := diag.Middleware(otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "deny" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		checkpoint.ServeHTTP(w, r)
	}), "eshu-api", otelhttp.WithTracerProvider(diag.provider)))
	for _, denied := range []bool{true, false} {
		req := httptest.NewRequest(http.MethodGet, statusDiagnosticPath, nil)
		if denied {
			req.Header.Set("Authorization", "deny")
		}
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	if err := diag.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	report := readStatusDiagnosticReport(t, path)
	if !report.Complete || len(report.Requests) != 2 || report.Requests[0].StatusCode != http.StatusUnauthorized || report.Requests[0].PostAuthReached || report.Requests[1].StatusCode != http.StatusServiceUnavailable || !report.Requests[1].PostAuthReached {
		t.Fatalf("failure classification: %+v", report.Requests)
	}
}

func TestStatusDiagnosticsOverflowAndUnfinishedFailClosed(t *testing.T) {
	diag, path, _ := newStatusDiagnosticFixture(t)
	handler := diag.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	for i := 0; i <= statusDiagnosticMaxRequests; i++ {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, statusDiagnosticPath, nil))
	}
	if err := diag.Flush(context.Background()); err == nil {
		t.Fatal("overflow flush succeeded")
	}
	report := readStatusDiagnosticReport(t, path)
	if report.Complete || !report.Overflow || len(report.Requests) != statusDiagnosticMaxRequests {
		t.Fatalf("overflow report: %+v", report)
	}

	unfinished, unfinishedPath, provider := newStatusDiagnosticFixture(t)
	tracer := provider.Tracer("fixture")
	var active trace.Span
	unfinished.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, active = tracer.Start(r.Context(), "postgres.status_snapshot")
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, statusDiagnosticPath, nil))
	if err := unfinished.Flush(context.Background()); err == nil {
		t.Fatal("unfinished span flush succeeded")
	}
	invalid := readStatusDiagnosticReport(t, unfinishedPath)
	if invalid.Complete || len(invalid.Errors) == 0 {
		t.Fatalf("unfinished report: %+v", invalid)
	}
	active.End()
}

func TestStatusDiagnosticsIgnoresClientIDAndSecuresArtifact(t *testing.T) {
	diag, path, _ := newStatusDiagnosticFixture(t)
	handler := diag.Middleware(otelhttp.NewHandler(diag.PostAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordStatusDiagnosticStages(r.Context(), diag.provider.Tracer("fixture"))
		w.WriteHeader(http.StatusOK)
	})), "eshu-api", otelhttp.WithTracerProvider(diag.provider)))
	req := httptest.NewRequest(http.MethodGet, statusDiagnosticPath, nil)
	req.Header.Set(statusDiagnosticHeader, "client-forged")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if got := recorder.Header().Get(statusDiagnosticHeader); got != "1" {
		t.Fatalf("server accepted client diagnostic ID: %q", got)
	}
	if err := diag.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("report permissions = %o", got)
	}
}

func TestStatusDiagnosticsInterruptedHandlerIsIncomplete(t *testing.T) {
	diag, path, _ := newStatusDiagnosticFixture(t)
	handler := diag.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("private failure")
	}))
	func() {
		defer func() { _ = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, statusDiagnosticPath, nil))
	}()
	if err := diag.Flush(context.Background()); err == nil {
		t.Fatal("interrupted handler produced complete report")
	}
	report := readStatusDiagnosticReport(t, path)
	if report.Complete || len(report.Requests) != 1 || report.Requests[0].StatusCode != 0 {
		t.Fatalf("interrupted report: %+v", report)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private failure") {
		t.Fatal("panic text leaked")
	}
}

func TestStatusDiagnosticsNoTargetRequestsIsIncomplete(t *testing.T) {
	diag, path, _ := newStatusDiagnosticFixture(t)
	if err := diag.Flush(context.Background()); err == nil {
		t.Fatal("empty diagnostic report accepted")
	}
	report := readStatusDiagnosticReport(t, path)
	if report.Complete || len(report.Errors) != 1 || report.Errors[0] != "no_target_requests" {
		t.Fatalf("empty report: %+v", report)
	}
}

func TestStatusDiagnosticsMissingSuccessfulReadStagesIsIncomplete(t *testing.T) {
	diag, path, provider := newStatusDiagnosticFixture(t)
	handler := diag.Middleware(otelhttp.NewHandler(diag.PostAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})), "eshu-api", otelhttp.WithTracerProvider(provider)))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, statusDiagnosticPath, nil))
	if err := diag.Flush(context.Background()); err == nil {
		t.Fatal("successful read with missing stages accepted")
	}
	report := readStatusDiagnosticReport(t, path)
	if report.Complete || len(report.Errors) == 0 {
		t.Fatalf("missing stages report: %+v", report)
	}
}
