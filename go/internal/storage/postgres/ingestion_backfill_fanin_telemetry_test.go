// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Metric proofs for the deferred backfill's fan-in publication phase.
//
// The fan-in is a concurrent stage with its own scaling behaviour: it opens one
// transaction per (scope, generation) partition after every evidence batch has
// committed, so DeferredBackfillDuration -- which covers the whole pass -- cannot
// separate its cost from the evidence phase. It also carries the per-partition
// ArgoCD probe whose cost this design deliberately accepted, and an accepted cost
// that cannot be observed is one nobody can revisit.
//
// These tests assert the instruments RECORD, not merely that they exist. A
// skipped-partition counter that never increments is a check that cannot fail, so
// each path is driven for real: the success path must move published, and the
// superseded-generation path must move skipped with its reason label.

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fanInMetricHarness wires a manual reader so a test can drive the real fan-in
// and then read what it recorded.
func fanInMetricHarness(t *testing.T) (*sdkmetric.ManualReader, *telemetry.Instruments) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return reader, instruments
}

// collectFanInCounter returns the total recorded for an Int64 counter and, when
// wantReason is non-empty, requires every data point to carry that reason label.
// A metric absent from the collection reports 0, which is what distinguishes
// "recorded zero" from "never wired".
func collectFanInCounter(t *testing.T, reader *sdkmetric.ManualReader, name, wantReason string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != name {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", name, record.Data)
			}
			for _, dp := range sum.DataPoints {
				if wantReason != "" {
					reason, found := dp.Attributes.Value("reason")
					if !found {
						t.Fatalf("%s data point carries no reason attribute, want %q", name, wantReason)
					}
					if reason.AsString() != wantReason {
						t.Fatalf("%s reason = %q, want %q", name, reason.AsString(), wantReason)
					}
				}
				total += dp.Value
			}
		}
	}
	return total
}

// collectFanInHistogramCount returns how many observations a Float64 histogram
// recorded. The fan-in phase runs once per pass, so the expected count is 1.
func collectFanInHistogramCount(t *testing.T, reader *sdkmetric.ManualReader, name string) uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var count uint64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, record := range scopeMetrics.Metrics {
			if record.Name != name {
				continue
			}
			hist, ok := record.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Histogram[float64]", name, record.Data)
			}
			for _, dp := range hist.DataPoints {
				count += dp.Count
			}
		}
	}
	return count
}

// TestDeferredBackfillFanInRecordsPublishedAndDurationMetrics drives the clean
// success path: three distinct partitions all publish. The published counter must
// reach 3, the duration histogram must record exactly one observation for the
// phase, and the skipped counter must stay at 0 -- present-but-zero, not missing,
// which the sibling test below distinguishes by making it move.
func TestDeferredBackfillFanInRecordsPublishedAndDurationMetrics(t *testing.T) {
	t.Parallel()

	activeGen := [][]any{
		{"repo-a", "scope-a", "gen-a"},
		{"repo-b", "scope-b", "gen-b"},
		{"repo-c", "scope-c", "gen-c"},
	}
	db := &concurrencyProbeDB{activeGenRows: activeGen}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 2

	reader, instruments := fanInMetricHarness(t)

	published, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"fingerprint-metrics",
		instruments,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if published != 3 {
		t.Fatalf("writeDeferredBackfillInBatches() published = %d, want 3", published)
	}

	if got := collectFanInCounter(t, reader, "eshu_dp_deferred_backfill_fanin_published_total", ""); got != 3 {
		t.Fatalf("eshu_dp_deferred_backfill_fanin_published_total = %d, want 3 (one per published partition)", got)
	}
	if got := collectFanInCounter(t, reader, "eshu_dp_deferred_backfill_fanin_skipped_total", ""); got != 0 {
		t.Fatalf("eshu_dp_deferred_backfill_fanin_skipped_total = %d, want 0 when every partition publishes", got)
	}
	if got := collectFanInHistogramCount(t, reader, "eshu_dp_deferred_backfill_fanin_duration_seconds"); got != 1 {
		t.Fatalf("eshu_dp_deferred_backfill_fanin_duration_seconds observations = %d, want exactly 1 for the pass's fan-in phase", got)
	}
}

// TestDeferredBackfillFanInRecordsSkippedMetricWhenGenerationAdvanced is the half
// that keeps the skipped counter honest. It drives the superseded-generation path
// and requires the counter to MOVE and to carry the reason label, so the label
// value and the deferred_backfill_fanin_partition_skipped log's reason field are
// pinned to the same constant.
func TestDeferredBackfillFanInRecordsSkippedMetricWhenGenerationAdvanced(t *testing.T) {
	t.Parallel()

	// The fake answers the fan-in's per-scope lookup from the FIRST row matching
	// the scope, so scope-shared resolves to gen-shared and repo-stale's
	// contribution under gen-stale is a superseded partition.
	activeGen := [][]any{
		{"repo-live", "scope-shared", "gen-shared"},
		{"repo-stale", "scope-shared", "gen-stale"},
	}
	db := &concurrencyProbeDB{activeGenRows: activeGen}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	reader, instruments := fanInMetricHarness(t)

	published, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"fingerprint-skip-metrics",
		instruments,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if published != 1 {
		t.Fatalf("writeDeferredBackfillInBatches() published = %d, want 1 (only the live generation)", published)
	}

	skipped := collectFanInCounter(t, reader,
		"eshu_dp_deferred_backfill_fanin_skipped_total", deferredFanInSkipGenerationAdvanced)
	if skipped != 1 {
		t.Fatalf(
			"eshu_dp_deferred_backfill_fanin_skipped_total = %d, want 1; a skip counter that never moves is a check that cannot fail",
			skipped,
		)
	}
	if got := collectFanInCounter(t, reader, "eshu_dp_deferred_backfill_fanin_published_total", ""); got != 1 {
		t.Fatalf("eshu_dp_deferred_backfill_fanin_published_total = %d, want 1", got)
	}
}

// TestDeferredFanInSkipReasonsAreTheLoggedReasons pins the closed label set. The
// reason strings are metric label values AND the log line's reason field; if they
// drift apart, an operator correlating the counter with the log finds nothing.
func TestDeferredFanInSkipReasonsAreTheLoggedReasons(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{name: "under-lock re-read", got: deferredFanInSkipGenerationAdvanced, want: "generation_advanced_since_batch"},
		{name: "fact-load snapshot", got: deferredFanInSkipSnapshotAdvanced, want: "generation_advanced_since_snapshot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("skip reason = %q, want %q (the value the log line and the metric label share)", tc.got, tc.want)
			}
		})
	}
}
