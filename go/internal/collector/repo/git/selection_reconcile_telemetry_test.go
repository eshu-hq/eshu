// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// reconcileCounterByReason sums an Int64 counter's datapoints by their reason
// attribute.
func reconcileCounterByReason(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]int64 {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scopeMetrics := range metrics.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 1 {
					t.Fatalf("%s attributes = %v, want only the bounded reason", name, point.Attributes.ToSlice())
				}
				reason, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionReason))
				out[reason.AsString()] += point.Value
			}
		}
	}
	return out
}

// TestReconcileSweepTelemetry proves the #7288 operator signals: forced
// reconciles carry a bounded reason label and a git_reconcile_forced log (WARN
// when an in-flight full expired), and in-flight holds count as suppressions.
func TestReconcileSweepTelemetry(t *testing.T) {
	start := time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC)
	world, reposDir := newReconcileWorld(t, start)
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("reconcile-telemetry-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Cycle 0 forces (interval_elapsed); cycles 1-2 hold (reconcile_in_flight);
	// a cycle one Interval after the forced full forces again (in_flight_expired).
	for _, now := range []time.Time{start, start.Add(6 * time.Minute), start.Add(12 * time.Minute), start.Add(24 * time.Hour)} {
		runObservedReconcileCycle(t, world, reposDir, now, "samesha", instruments, logger)
	}

	forced := reconcileCounterByReason(t, reader, "eshu_dp_collector_reconciliation_full_snapshots_total")
	if forced[reconcileReasonIntervalElapsed] != 1 || forced[reconcileReasonInFlightExpired] != 1 || len(forced) != 2 {
		t.Fatalf("forced reconciles by reason = %v, want interval_elapsed=1 in_flight_expired=1", forced)
	}
	suppressed := reconcileCounterByReason(t, reader, "eshu_dp_collector_reconciliation_suppressed_total")
	if suppressed[reconcileReasonInFlight] != 2 || len(suppressed) != 1 {
		t.Fatalf("suppressed by reason = %v, want reconcile_in_flight=2", suppressed)
	}

	var forcedLogs []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if record["msg"] == "git_reconcile_forced" {
			forcedLogs = append(forcedLogs, record)
		}
	}
	if len(forcedLogs) != 2 {
		t.Fatalf("git_reconcile_forced logs = %d, want 2", len(forcedLogs))
	}
	first, second := forcedLogs[0], forcedLogs[1]
	if first["level"] != "INFO" || first["reason"] != reconcileReasonIntervalElapsed ||
		first["scope_id"] == "" || first["last_projected_full_at"] == nil || first["latest_full_status"] != "active" {
		t.Fatalf("first forced log = %v, want INFO interval_elapsed with scope and state", first)
	}
	if second["level"] != "WARN" || second["reason"] != reconcileReasonInFlightExpired || second["latest_full_status"] != "pending" {
		t.Fatalf("second forced log = %v, want WARN in_flight_expired over a pending full", second)
	}
}
