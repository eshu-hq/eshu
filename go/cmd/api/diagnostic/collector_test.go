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
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestStatusDiagnosticsDisabled(t *testing.T) {
	provider := sdktrace.NewTracerProvider()
	got, err := New(func(string) string { return "" }, provider)
	if err != nil || got != nil {
		t.Fatalf("disabled diagnostics = %v, %v; want nil, nil", got, err)
	}
}

func TestStatusDiagnosticsExactRouteAndFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	provider := sdktrace.NewTracerProvider()
	diag, err := New(func(string) string { return path }, provider)
	if err != nil {
		t.Fatal(err)
	}
	handler := diag.Middleware(otelhttp.NewHandler(diag.PostAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordStatusDiagnosticStages(r.Context(), provider.Tracer("fixture"))
		w.WriteHeader(http.StatusOK)
	})), "eshu-api", otelhttp.WithTracerProvider(provider)))
	for _, target := range []string{"/api/v0/status/ingesters/repository", "/api/v0/status/ingesters/other"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		if target == "/api/v0/status/ingesters/repository" && recorder.Header().Get("X-Eshu-Diagnostic-ID") == "" {
			t.Fatal("target response missing diagnostic ID")
		}
		if target != "/api/v0/status/ingesters/repository" && recorder.Header().Get("X-Eshu-Diagnostic-ID") != "" {
			t.Fatal("non-target response has diagnostic ID")
		}
	}
	if err := diag.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var report struct {
		SchemaVersion int  `json:"schema_version"`
		Complete      bool `json:"complete"`
		Requests      []struct {
			ID string `json:"id"`
		} `json:"requests"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 1 || !report.Complete || len(report.Requests) != 1 || report.Requests[0].ID != "1" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

// recordStatusDiagnosticStages models the production checkpoint and snapshot
// span lineage without a database or any payload-bearing span attributes.
func recordStatusDiagnosticStages(ctx context.Context, tracer trace.Tracer) {
	_, checkpoint := tracer.Start(ctx, "postgres.reader_access", trace.WithAttributes(
		attribute.String("role", "writer"), attribute.String("stage", "writer_checkpoint"), attribute.String("outcome", "ok")))
	checkpoint.End()
	snapshotCtx, snapshot := tracer.Start(ctx, "postgres.status_snapshot", trace.WithAttributes(
		attribute.String("phase", "read"), attribute.String("outcome", "ok")))
	_, read := tracer.Start(snapshotCtx, "postgres.reader_access", trace.WithAttributes(
		attribute.String("role", "reader"), attribute.String("stage", "reader_borrow"), attribute.String("outcome", "ok")))
	read.End()
	snapshot.End()
}

// TestClosedStatusQuerySummaryKeepsEveryStoredSummaryReadLabel pins the
// stored-summary row read labels the status store emits (the db.query.summary of
// each model's keyed row read). A label missing here is blanked in the retained
// diagnostic evidence, so a new model must be added with its label (#8828).
func TestClosedStatusQuerySummaryKeepsEveryStoredSummaryReadLabel(t *testing.T) {
	for _, label := range []string{"active_work_summary_model", "terraform_state_model", "coordinator_model", "coordinator"} {
		if got := closedStatusQuerySummary(label); got != label {
			t.Errorf("closedStatusQuerySummary(%q) = %q, want it kept", label, got)
		}
	}
	for _, label := range []string{"", "coordinator_model_typo", "SELECT 1"} {
		if got := closedStatusQuerySummary(label); got != "" {
			t.Errorf("closedStatusQuerySummary(%q) = %q, want it blanked", label, got)
		}
	}
}
