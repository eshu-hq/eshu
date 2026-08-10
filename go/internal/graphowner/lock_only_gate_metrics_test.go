// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newTestInstrumentsPair creates a fresh telemetry.Instruments wired to a
// ManualReader so Collect returns every recorded value, mirroring the
// equivalent helper in internal/collector's claimed-service metrics tests.
func newTestInstrumentsPair(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("lock-only-gate-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// lockOnlyCounterValue extracts an Int64 counter data point's value for the
// given attribute set, mirroring internal/telemetry's int64CounterValue.
func lockOnlyCounterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, wantAttrs map[string]string) (int64, bool) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Sum[int64]", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				if attrsMatch(point.Attributes.ToSlice(), wantAttrs) {
					return point.Value, true
				}
			}
		}
	}
	return 0, false
}

// lockOnlyHistogramCount extracts a Float64 histogram data point's count for
// the given attribute set, mirroring internal/collector's
// claimedHistogramOutcome pattern.
func lockOnlyHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, wantAttrs map[string]string) (uint64, bool) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Histogram[float64]", name, m.Data)
			}
			for _, point := range hist.DataPoints {
				if attrsMatch(point.Attributes.ToSlice(), wantAttrs) {
					return point.Count, true
				}
			}
		}
	}
	return 0, false
}

func attrsMatch(attrs []attribute.KeyValue, wantAttrs map[string]string) bool {
	for wantKey, wantValue := range wantAttrs {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == wantKey && attr.Value.AsString() == wantValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestLockOnlyGateWriteChunkRecordsLockedRowsAndWaitMetrics proves writeChunk
// records both #5101 lock-only observability signals — the locked-rows volume
// counter and the lock-wait distribution histogram — with the bounded family
// label, alongside the pre-existing slow-wait log this change does not
// remove.
func TestLockOnlyGateWriteChunkRecordsLockedRowsAndWaitMetrics(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstrumentsPair(t)
	beginner := &fakeChunkBeginner{}
	store := &fakeLockOnlyStore{}
	gate := &LockOnlyGate{db: beginner, store: store, Instruments: instruments}

	underlying := func(_ context.Context, _ []map[string]any, _, _, _ string) error {
		return nil
	}
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}, {"uid": "c"}}
	if err := w.WriteRDSPostureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes error = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	wantAttrs := map[string]string{"family": familyRDSPosture}
	gotRows, ok := lockOnlyCounterValue(t, rm, "eshu_dp_lock_only_gate_locked_rows_total", wantAttrs)
	if !ok {
		t.Fatal("eshu_dp_lock_only_gate_locked_rows_total: no data point with family=rds_posture found")
	}
	if gotRows != int64(len(rows)) {
		t.Fatalf("eshu_dp_lock_only_gate_locked_rows_total = %d, want %d", gotRows, len(rows))
	}

	gotCount, ok := lockOnlyHistogramCount(t, rm, "eshu_dp_lock_only_gate_lock_wait_seconds", wantAttrs)
	if !ok {
		t.Fatal("eshu_dp_lock_only_gate_lock_wait_seconds: no data point with family=rds_posture found")
	}
	if gotCount != 1 {
		t.Fatalf("eshu_dp_lock_only_gate_lock_wait_seconds count = %d, want 1 (one chunk, one lock acquisition)", gotCount)
	}
}

// TestLockOnlyGateWriteChunkRolledBackChunkCountsNoRowsButRecordsWait pins the
// PLACEMENT of the two recordings, which is the part of this change that four
// separate documents assert and no test previously guarded.
//
// The rows counter sits after tx.Commit() succeeds, so a chunk that rolled back
// contributes nothing — counting rows the gate did not durably write would make
// the counter lie in exactly the situation an operator is investigating. The
// wait histogram sits on the acquisition path instead, so a chunk that acquired
// its lock and then failed downstream STILL contributes its wait sample; those
// are the contended writes whose latency matters most, and recording after the
// commit would silently drop them.
//
// Existence and labels were already pinned. This pins the ordering: hoisting
// the counter above the write, or moving the histogram after the commit, both
// left the whole package green before this test existed.
func TestLockOnlyGateWriteChunkRolledBackChunkCountsNoRowsButRecordsWait(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstrumentsPair(t)
	beginner := &fakeChunkBeginner{}
	gate := &LockOnlyGate{db: beginner, store: &fakeLockOnlyStore{}, Instruments: instruments}

	wantErr := errors.New("graph write failed")
	underlying := func(_ context.Context, _ []map[string]any, _, _, _ string) error {
		return wantErr
	}
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	err := w.WriteRDSPostureNodes(context.Background(), []map[string]any{{"uid": "a"}}, "scope-1", "gen-1", "reducer/rds-posture")
	if !errors.Is(err, wantErr) {
		t.Fatalf("WriteRDSPostureNodes error = %v, want %v", err, wantErr)
	}
	if len(beginner.txs) != 1 || beginner.txs[0].committed || !beginner.txs[0].rolledBack {
		t.Fatalf("tx state = %+v, want rolled back and NOT committed", beginner.txs)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	wantAttrs := map[string]string{"family": familyRDSPosture}

	if got, ok := lockOnlyCounterValue(t, rm, "eshu_dp_lock_only_gate_locked_rows_total", wantAttrs); ok {
		t.Fatalf(
			"eshu_dp_lock_only_gate_locked_rows_total recorded %d for a rolled-back chunk, want no data point: "+
				"the counter must sit after tx.Commit() so it never counts rows that were not durably written",
			got,
		)
	}

	gotCount, ok := lockOnlyHistogramCount(t, rm, "eshu_dp_lock_only_gate_lock_wait_seconds", wantAttrs)
	if !ok {
		t.Fatal(
			"eshu_dp_lock_only_gate_lock_wait_seconds: no data point for a chunk that acquired its lock and then " +
				"failed downstream -- the histogram must sit on the acquisition path, not after the commit",
		)
	}
	if gotCount != 1 {
		t.Fatalf("eshu_dp_lock_only_gate_lock_wait_seconds count = %d, want 1 (the lock was acquired once)", gotCount)
	}
}

// TestLockOnlyGateWriteChunkNilInstrumentsSkipsMetrics proves a LockOnlyGate
// wired without telemetry (nil Instruments) is a silent no-op for both new
// instruments, matching Gate's nil-instruments convention.
func TestLockOnlyGateWriteChunkNilInstrumentsSkipsMetrics(t *testing.T) {
	t.Parallel()

	beginner := &fakeChunkBeginner{}
	store := &fakeLockOnlyStore{}
	gate := &LockOnlyGate{db: beginner, store: store}

	underlying := func(_ context.Context, _ []map[string]any, _, _, _ string) error {
		return nil
	}
	w := NewRDSPostureLockedWriter(gate, underlying, nil)
	rows := []map[string]any{{"uid": "a"}}
	if err := w.WriteRDSPostureNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/rds-posture"); err != nil {
		t.Fatalf("WriteRDSPostureNodes error = %v (nil Instruments must not panic or fail the write)", err)
	}
}
