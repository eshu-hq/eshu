// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReducerMetricsExposeMemorySignals is the #9567 regression: the QA
// reducer's scraped /metrics had no eshu_dp_gomemlimit_bytes and no Go heap
// series, because the reducer never registered the gauge the other runtimes
// register. It scrapes the reducer's Prometheus handler after the reducer's
// own memory-telemetry setup and requires both.
func TestReducerMetricsExposeMemorySignals(t *testing.T) {
	// ConfigureMemoryLimit writes GODEBUG; t.Setenv restores it.
	t.Setenv("GODEBUG", os.Getenv("GODEBUG"))
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	bootstrap, err := telemetry.NewBootstrap("reducer")
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	ctx := context.Background()
	providers, err := telemetry.NewProviders(ctx, bootstrap)
	if err != nil {
		t.Fatalf("NewProviders: %v", err)
	}
	defer func() { _ = providers.Shutdown(ctx) }()

	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	if err := configureMemoryTelemetry(slog.New(slog.DiscardHandler), meter); err != nil {
		t.Fatalf("configureMemoryTelemetry: %v", err)
	}

	rec := httptest.NewRecorder()
	providers.PrometheusHandler.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, name := range []string{"eshu_dp_gomemlimit_bytes", "go_memstats_heap_alloc_bytes", "go_memstats_heap_inuse_bytes"} {
		if !strings.Contains(body, "\n"+name) {
			t.Errorf("reducer /metrics is missing %s", name)
		}
	}
}
