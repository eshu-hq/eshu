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
		{"active_work_summary", "model", "fresh"}:            1,
		{"coordinator_snapshot", "live_fallback", "missing"}: 1,
		{"terraform_state", "live_fallback", "missing"}:      1,
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

// TestScrapeWithTheReaderOffAttributesOnlyModelReads pins the #10872
// attribution contract: a StoredActiveWorkOnly scrape with the reader off
// records its database time under the model read labels
// (active_work_summary_model, coordinator_model) and the scrape counter, and
// never under the live labels (active_work_summary, coordinator). Before the
// fix the same scrape recorded its time under the live labels; after it, the
// before/after per-reader split is measurable on the same instruments.
func TestScrapeWithTheReaderOffAttributesOnlyModelReads(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	storedAt := sourceTestNow.Add(-10 * time.Second)
	q := &sourceQueryer{installed: true, row: storedRow(storedAt, summary.SchemaVersion, pgstatus.ActiveWorkSummarySourceSHA256(), 4, encodedEntries(t, goodEntries()...))}
	// A stored coordinator row too, so both models serve stored and nothing
	// runs live: without it the flag-off scrape would correctly fall back
	// to the live coordinator statement, which is a different attribution
	// than this test pins.
	q.coordRow = coordStoredRow(t, storedAt, pgstatus.CoordinatorSummarySourceSHA256(), liveCoordinator(t))
	store := readerStore(q, false)
	store.Instruments = instruments
	scrapeSnapshot(t, store)
	if q.liveRuns.Load() != 0 {
		t.Fatalf("live statement ran %d times on a flag-off scrape, want 0", q.liveRuns.Load())
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	durations := map[string]uint64{}
	type scrapeKey struct{ model, source, reason string }
	scrapes := map[scrapeKey]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				if m.Name != "eshu_dp_status_snapshot_read_duration_seconds" {
					continue
				}
				for _, point := range data.DataPoints {
					read, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionRead))
					durations[read.AsString()] += point.Count
				}
			case metricdata.Sum[int64]:
				if m.Name != "eshu_dp_status_summary_scrape_total" {
					continue
				}
				for _, point := range data.DataPoints {
					model, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionModelKey))
					source, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSource))
					reason, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionReason))
					scrapes[scrapeKey{model.AsString(), source.AsString(), reason.AsString()}] += point.Value
				}
			}
		}
	}
	for _, live := range []string{"active_work_summary", "coordinator"} {
		if durations[live] != 0 {
			t.Fatalf("read %q recorded %d duration samples on a flag-off scrape, want none: the live statement must not run", live, durations[live])
		}
	}
	for _, model := range []string{"active_work_summary_model", "coordinator_model"} {
		if durations[model] == 0 {
			t.Fatalf("read %q recorded no duration sample on a flag-off scrape, want the stored-row reads", model)
		}
	}
	want := map[scrapeKey]int64{
		{"active_work_summary", "model", "fresh"}:  1,
		{"coordinator_snapshot", "model", "fresh"}: 1,
	}
	if len(scrapes) != len(want) {
		t.Fatalf("eshu_dp_status_summary_scrape_total points = %v, want exactly %v", scrapes, want)
	}
	for key, n := range want {
		if scrapes[key] != n {
			t.Fatalf("eshu_dp_status_summary_scrape_total points = %v, want %v", scrapes, want)
		}
	}
}
