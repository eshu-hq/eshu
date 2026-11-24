// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestGoEvidenceLockSignalsRecordWithClosedLabels proves the #11327 signals: the
// recheck-abort counter carries only the closed reason label, the fence's timeout
// counter and wait histogram carry none, and every method is safe on a nil receiver
// and on an Instruments that did not register the instrument.
func TestGoEvidenceLockSignalsRecordWithClosedLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst := &Instruments{}
	if err := registerSupplyChainImpactSupportsInstruments(meter, inst); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	inst.RecordSupplyChainImpactGoEvidenceRecheckAbort(ctx, "generation_changed")
	inst.RecordSupplyChainImpactGoEvidenceRecheckAbort(ctx, "generation_changed")
	inst.RecordSupplyChainImpactGoEvidenceRecheckAbort(ctx, "lock_timeout")
	inst.RecordGoFenceAdvisoryLockWait(ctx, 3*time.Millisecond, false)
	inst.RecordGoFenceAdvisoryLockWait(ctx, 2*time.Second, true)

	var nilInst *Instruments
	nilInst.RecordSupplyChainImpactGoEvidenceRecheckAbort(ctx, "lock_timeout")
	nilInst.RecordGoFenceAdvisoryLockWait(ctx, time.Second, true)
	(&Instruments{}).RecordSupplyChainImpactGoEvidenceRecheckAbort(ctx, "lock_timeout")
	(&Instruments{}).RecordGoFenceAdvisoryLockWait(ctx, time.Second, true)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	aborts := map[string]int64{}
	var timeouts, waits int64
	var attributes int
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total":
				for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
					reason, _ := point.Attributes.Value("reason")
					aborts[reason.AsString()] += point.Value
					if point.Attributes.Len() != 1 {
						t.Fatalf("abort counter has %d attributes, want only reason", point.Attributes.Len())
					}
				}
			case "eshu_dp_go_fence_advisory_lock_timeouts_total":
				for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
					timeouts += point.Value
					attributes += point.Attributes.Len()
				}
			case "eshu_dp_go_fence_advisory_lock_wait_seconds":
				for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					waits += int64(point.Count)
					attributes += point.Attributes.Len()
				}
			}
		}
	}
	if aborts["generation_changed"] != 2 || aborts["lock_timeout"] != 1 {
		t.Fatalf("aborts = %v, want generation_changed 2 and lock_timeout 1", aborts)
	}
	if timeouts != 1 || waits != 2 || attributes != 0 {
		t.Fatalf("timeouts = %d waits = %d attributes = %d, want 1, 2 and no labels", timeouts, waits, attributes)
	}
}
