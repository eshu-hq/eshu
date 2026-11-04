// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// NVDCursorLagTracker records the latest server-stamped cursor position per
// cursor key (the scope id in production). It is process-local by design,
// following SuccessorHoldTracker: it feeds the cursor-lag gauge and adds no
// Postgres read or write to the commit path. Both operands are server
// timestamps (the committed head and the cursor clamped to it), so a fast
// local clock fabricates no lag and a slow one hides none. A series exists
// only while a position is recorded; an absent series means no claim has
// reported a head yet (the cursor is inactive until the per-window-ownership
// follow-up wires the recording call site).
type NVDCursorLagTracker struct {
	mu        sync.Mutex
	positions map[nvdCursorKey]nvdCursorPosition
}

type nvdCursorKey struct {
	source string
	key    string
}

type nvdCursorPosition struct {
	cursorUnixMicro int64
	headUnixMicro   int64
}

// NewNVDCursorLagTracker returns an empty tracker.
func NewNVDCursorLagTracker() *NVDCursorLagTracker {
	return &NVDCursorLagTracker{positions: make(map[nvdCursorKey]nvdCursorPosition)}
}

// RecordCursorPosition stores the latest server-stamped position for key. A
// nil tracker or a blank key records nothing. Positions keep the max per
// key, mirroring the planner's compare-and-advance: concurrent claims can
// record in either order, and a stale out-of-order record must not move a
// series backward. A negative lag still floors at zero in the gauge.
func (t *NVDCursorLagTracker) RecordCursorPosition(source, key string, cursorUnixMicro, headUnixMicro int64) {
	if t == nil || key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	id := nvdCursorKey{source: source, key: key}
	current := t.positions[id]
	if cursorUnixMicro > current.cursorUnixMicro {
		current.cursorUnixMicro = cursorUnixMicro
	}
	if headUnixMicro > current.headUnixMicro {
		current.headUnixMicro = headUnixMicro
	}
	t.positions[id] = current
}

// LagSecondsBySource returns, per source, the maximum lag in seconds across
// its recorded positions (head minus cursor, floored at zero). A nil tracker
// reports nothing.
func (t *NVDCursorLagTracker) LagSecondsBySource() map[string]float64 {
	lags := make(map[string]float64)
	if t == nil {
		return lags
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, position := range t.positions {
		lag := float64(position.headUnixMicro-position.cursorUnixMicro) / 1e6
		if lag < 0 {
			lag = 0
		}
		if lag > lags[id.source] {
			lags[id.source] = lag
		}
	}
	return lags
}

// registerNVDCursorLag registers the NVD cursor-lag gauge and the time
// anomaly counter (#9877). The gauge callback reads process-local state
// only, so a scrape adds no query. Labels stay source-only: no scope, window,
// CVE, or clock value ever becomes a label.
func registerNVDCursorLag(meter metric.Meter, inst *Instruments) error {
	inst.NVDCursorLags = NewNVDCursorLagTracker()
	var err error
	inst.NVDCursorLagSeconds, err = meter.Float64ObservableGauge(
		"eshu_dp_vulnerability_nvd_cursor_lag_seconds",
		metric.WithDescription("NVD rolling-window lag in seconds per source (stored head minus cursor, both server-stamped; #9877). Rising lag with flat clock skew means the planner or claims are behind. Process-local: max across this process's recorded positions; a series exists only while a position is recorded"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			for source, lag := range inst.NVDCursorLags.LagSecondsBySource() {
				o.Observe(lag, metric.WithAttributes(attribute.String(MetricDimensionSource, source)))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register NVDCursorLagSeconds gauge: %w", err)
	}
	inst.NVDTimeAnomalies, err = meter.Int64Counter(
		"eshu_dp_vulnerability_nvd_time_anomalies_total",
		metric.WithDescription("Total NVD unreliable-time observations per source (#9877): unknown head, clock skew past the 5-minute budget, or record timestamps past head plus budget. The reason rides the span, never a label"),
	)
	if err != nil {
		return fmt.Errorf("register NVDTimeAnomalies counter: %w", err)
	}
	return nil
}
