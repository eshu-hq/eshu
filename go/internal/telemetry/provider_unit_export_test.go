// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDeferredBackfillPartitionLoadFactCountExportedNameHasNoUnitSuffix locks
// in the exact Prometheus series name for
// DeferredBackfillPartitionLoadFactCount, registered with
// metric.WithUnit("{fact}") in instruments.go.
//
// Existing coverage for this histogram (see
// internal/storage/postgres/ingestion_backfill_task_telemetry_test.go) reads
// the metric back through an sdkmetric.NewManualReader, which observes the
// pre-export OTel SDK name and cannot see anything the Prometheus exporter's
// naming translator does to it. This test instead goes through the real
// production path -- NewProviders, which builds the same otelprom exporter
// createMeterProvider wires in provider.go -- and scrapes the actual
// /metrics HTTP output, the same surface an operator's Prometheus server
// sees.
//
// Why this matters: go.opentelemetry.io/otel/exporters/prometheus appends a
// suffix for units it recognizes (see unitSuffixes in exporter.go: "s" ->
// "_seconds", "By" -> "_bytes", etc.). "{fact}" is a UCUM curly-brace
// annotation unit and is not a key in that table, so the lookup misses and
// the exporter leaves the name unchanged -- it does not special-case
// annotation syntax, it simply has no entry for this unit. If a future
// otelprom bump adds "{fact}" (or annotation units generally) to that table,
// this test fails loudly instead of silently renaming the operator-facing
// series.
func TestDeferredBackfillPartitionLoadFactCountExportedNameHasNoUnitSuffix(t *testing.T) {
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	b, err := NewBootstrap("test-service")
	require.NoError(t, err)

	ctx := context.Background()
	providers, err := NewProviders(ctx, b)
	require.NoError(t, err)
	defer func() {
		_ = providers.Shutdown(ctx)
	}()

	meter := providers.MeterProvider.Meter(DefaultSignalName)
	inst, err := NewInstruments(meter)
	require.NoError(t, err)

	inst.DeferredBackfillPartitionLoadFactCount.Record(ctx, 231)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	providers.PrometheusHandler.ServeHTTP(rec, req)
	body := rec.Body.String()

	wantName := "eshu_dp_deferred_backfill_partition_load_fact_count"
	countLines := metricLinesContaining(body, wantName+"_count")
	require.NotEmpty(t, countLines, "exported series %q not found on /metrics; full body:\n%s", wantName, body)

	// The unit must not have grown a suffix (e.g. "..._fact_count" would mean
	// a future otelprom version started recognizing "{fact}").
	suffixedLines := metricLinesContaining(body, wantName+"_fact")
	require.Empty(
		t,
		suffixedLines,
		"exported series gained an unexpected unit suffix from \"{fact}\"; full body:\n%s",
		body,
	)
}
