// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// statsJournal is a fakeJournal whose Stats returns fixed ledger stats.
type statsJournal struct {
	fakeJournal
	stats         store.LedgerStats
	ledgerOrphans store.LedgerOrphans
	probes        int
}

func (j *statsJournal) Stats(context.Context) (store.LedgerStats, error) { return j.stats, nil }

func (j *statsJournal) Orphans(context.Context) (store.LedgerOrphans, error) {
	j.probes++
	return j.ledgerOrphans, nil
}

// TestRecordGaugesReportsLinkDeltaTableSize proves the link-delta table's
// size reaches operators (#7127 ruling 7.5: the table is bounded by
// generation retention, and the gauge is how that bound is watched), beside
// the state table's.
func TestRecordGaugesReportsLinkDeltaTableSize(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("links-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	r := &Runner{
		Journal:     &statsJournal{stats: store.LedgerStats{StateBytes: 11, StateRows: 12, DeltaBytes: 4096, DeltaRows: 37}},
		Instruments: instruments,
	}
	r.recordGauges(context.Background())

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok && len(gauge.DataPoints) == 1 {
				got[m.Name] = gauge.DataPoints[0].Value
			}
		}
	}
	for name, want := range map[string]int64{
		"eshu_dp_changed_since_state_bytes":  11,
		"eshu_dp_changed_since_state_rows":   12,
		"eshu_dp_changed_since_deltas_bytes": 4096,
		"eshu_dp_changed_since_deltas_rows":  37,
	} {
		if got[name] != want {
			t.Errorf("%s = %d, want %d (collected %v)", name, got[name], want, got)
		}
	}
}

// TestRecordGaugesReportsLedgerOrphans proves the orphan probe (arbiter
// ruling arb-7127-3d, C5) reaches operators as one gauge labelled by kind, and
// that the probe, which scans the link, activation and bucket-count tables,
// runs at most once per orphanProbeInterval however often cycles run.
func TestRecordGaugesReportsLedgerOrphans(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("links-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	journal := &statsJournal{ledgerOrphans: store.LedgerOrphans{Links: 3, Activations: 2, HeadlessBucketGroups: 1}}
	r := &Runner{Journal: journal, Instruments: instruments}
	r.recordGauges(context.Background())
	r.recordGauges(context.Background())
	if journal.probes != 1 {
		t.Fatalf("orphan probe ran %d times in two back-to-back cycles, want 1", journal.probes)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_changed_since_ledger_orphans" {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("%s data = %T, want an int64 gauge", m.Name, m.Data)
			}
			for _, point := range gauge.DataPoints {
				kind, _ := point.Attributes.Value(telemetry.MetricDimensionKind)
				got[kind.AsString()] = point.Value
			}
		}
	}
	want := map[string]int64{"link": 3, "activation": 2, "bucket_group": 1}
	for kind, n := range want {
		if got[kind] != n {
			t.Fatalf("ledger orphans = %v, want %v", got, want)
		}
	}
}
