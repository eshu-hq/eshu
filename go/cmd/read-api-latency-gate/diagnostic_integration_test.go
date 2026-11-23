// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/cmd/api/diagnostic"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TestDiagnosticHTTPIntegration joins the actual SDK collector and gate workers.
// This transport fixture does not prove database behavior or runtime overhead.
func TestDiagnosticHTTPIntegration(t *testing.T) {
	provider := sdktrace.NewTracerProvider()
	defer func() { _ = provider.Shutdown(context.Background()) }()
	path := filepath.Join(t.TempDir(), "server.json")
	collector, err := diagnostic.New(func(string) string { return path }, provider)
	if err != nil {
		t.Fatal(err)
	}
	tracer := provider.Tracer("integration-fixture")
	var calls atomic.Int32
	inner := collector.PostAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, checkpoint := tracer.Start(r.Context(), "postgres.reader_access", trace.WithAttributes(
			attribute.String("role", "writer"), attribute.String("stage", "writer_checkpoint")))
		checkpoint.End()
		ctx, snapshot := tracer.Start(r.Context(), "postgres.status_snapshot")
		_, reader := tracer.Start(db.WithQuerySummary(ctx, "SELECT private_fixture"), "postgres.reader_access", trace.WithAttributes(
			attribute.String("role", "reader"), attribute.String("stage", "reader_borrow")))
		reader.End()
		time.Sleep(time.Millisecond)
		snapshot.End()
		w.WriteHeader(http.StatusOK)
	}))
	server := httptest.NewServer(collector.Middleware(otelhttp.NewHandler(inner, "eshu-api", otelhttp.WithTracerProvider(provider))))
	defer server.Close()
	client := newDiagnosticClient(20)
	op := Operation{Method: http.MethodGet, Path: "/api/v0/status/ingesters/repository"}
	results, err := SweepConcurrentOperations(SweepOptions{BaseURL: server.URL, Timeout: time.Second, Diagnostics: client}, map[string]Operation{op.Path: op}, 4, 20)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 20 || len(results) != 1 || results[0].Succeeded != 20 || results[0].PeakInFlight < 2 {
		t.Fatalf("unexpected transport results: calls=%d results=%+v", calls.Load(), results)
	}
	if err := collector.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Complete bool `json:"complete"`
		Requests []struct {
			ID     string `json:"id"`
			Status int    `json:"status_code"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	records, err := client.snapshot()
	if err != nil || !report.Complete || len(report.Requests) != 20 || len(records) != 20 {
		t.Fatalf("incomplete joined evidence: %v", err)
	}
	ids := make(map[string]int, 20)
	for _, request := range report.Requests {
		ids[request.ID] = request.Status
	}
	for index, record := range records {
		if record.Index != index || ids[record.ServerID] != record.Status || record.ElapsedMS != float64(results[0].Samples[index])/float64(time.Millisecond) {
			t.Fatalf("dispatch index %d lost response or measurement correlation", index)
		}
	}
}
