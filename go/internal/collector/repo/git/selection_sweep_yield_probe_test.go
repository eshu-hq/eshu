// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSweepYieldProbeErrorIsCounted pins the #8512 P2 follow-up: a failed
// trigger-queue probe degrades to the full walk (see
// TestSweepYieldProbeErrorCompletesSweep) AND counts the failure on the
// existing WebhookStoreOperations instrument, so yield-disabled-by-outage is
// alertable instead of visible only by log grep.
func TestSweepYieldProbeErrorIsCounted(t *testing.T) {
	t.Parallel()

	instruments, reader := newCollisionTestInstruments(t)
	probe := &stubQueuedTriggerProbe{err: errors.New("probe unavailable")}
	policy := sweepYieldPolicy{Probe: probe, CheckEvery: 1, Instruments: instruments}

	if policy.shouldYield(context.Background(), nil, 1) {
		t.Fatal("shouldYield() = true on probe error, want false (complete the sweep)")
	}
	if probe.calls != 1 {
		t.Fatalf("probe calls = %d, want 1", probe.calls)
	}
	if got := collectWebhookStoreFailedProbes(t, reader); got != 1 {
		t.Fatalf("webhook store failed-probe operations = %d, want 1", got)
	}
}

// collectWebhookStoreFailedProbes returns the value of the single
// eshu_dp_webhook_store_operations_total stream the sweep-yield probe-error
// path records: provider and status unknown with outcome failed. Any other
// shape fails: the probe error must stay on existing label values so it adds
// no cardinality.
func collectWebhookStoreFailedProbes(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("reader.Collect: %v", err)
	}
	wantAttrs := attribute.NewSet(
		attribute.String("provider", "unknown"),
		attribute.String("outcome", "failed"),
		attribute.String("status", "unknown"),
	)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_webhook_store_operations_total" {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q: unexpected data type %T", m.Name, m.Data)
			}
			if len(data.DataPoints) != 1 {
				t.Fatalf("failed-probe datapoints = %d, want 1", len(data.DataPoints))
			}
			point := data.DataPoints[0]
			if !point.Attributes.Equals(&wantAttrs) {
				t.Fatalf("failed-probe attributes = %v, want %v",
					point.Attributes.ToSlice(), wantAttrs.ToSlice())
			}
			return point.Value
		}
	}
	t.Fatal("eshu_dp_webhook_store_operations_total not recorded")
	return 0
}
