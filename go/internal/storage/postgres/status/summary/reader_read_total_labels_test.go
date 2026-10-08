// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReaderCountsEachModelReadUnderItsOwnModelKey reads through the production
// StatusStore with both models enabled and checks eshu_dp_status_summary_read_total
// by label, not by sum: one point for model_key=active_work_summary (served from
// the model) and one for model_key=terraform_state (no row, so a live
// fallback). A read recorded under the wrong model_key keeps the sum at two and
// fails here.
func TestReaderCountsEachModelReadUnderItsOwnModelKey(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	storedAt := sourceTestNow.Add(-10 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	store := readerStore(q, true)
	store.Instruments = instruments
	readSnapshot(t, store)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	type labels struct{ model, source, reason string }
	points := map[labels]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "eshu_dp_status_summary_read_total" {
				continue
			}
			for _, point := range data.DataPoints {
				model, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionModelKey))
				source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
				reason, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionReason))
				points[labels{model.AsString(), source.AsString(), reason.AsString()}] += point.Value
			}
		}
	}
	want := map[labels]int64{
		{"active_work_summary", "model", "fresh"}:       1,
		{"terraform_state", "live_fallback", "missing"}: 1,
	}
	if len(points) != len(want) {
		t.Fatalf("eshu_dp_status_summary_read_total points = %v, want exactly %v", points, want)
	}
	for key, n := range want {
		if points[key] != n {
			t.Fatalf("eshu_dp_status_summary_read_total points = %v, want %v with one read per model_key", points, want)
		}
	}
}
