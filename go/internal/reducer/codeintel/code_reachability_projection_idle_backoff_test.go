// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeintel

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRunBacksOffOnConsecutiveEmptyProbeCycles drives the whole Run loop
// with a captured Wait: empty probe-equal cycles must wait base, 2x base,
// then clamp at the configured cap (#10870 mirrors the shared worker's
// consecutiveEmpty shape with the reachability idle cap).
func TestRunBacksOffOnConsecutiveEmptyProbeCycles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	loader := &probeFakeCodeReachabilityInputLoader{probe: now, probeOK: true}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config: CodeReachabilityProjectionRunnerConfig{
			PollInterval:    10 * time.Millisecond,
			MaxPollInterval: 35 * time.Millisecond,
			AuditInterval:   time.Hour,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waits []time.Duration
	// The stub records and returns immediately: Run calls waits
	// sequentially, so blocking for a future wait would deadlock the loop.
	// Canceling on the last expected wait makes the next loop-top check
	// see the canceled context and return.
	runner.Wait = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		if len(waits) == 4 {
			cancel()
		}
		return nil
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 35 * time.Millisecond, 35 * time.Millisecond}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
}

// TestRunResetsBackoffAfterProductiveCycle proves a productive cycle resets
// the idle backoff and re-polls immediately: with one productive cycle
// between empties, the wait sequence restarts at the base interval.
func TestRunResetsBackoffAfterProductiveCycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	input := []CodeReachabilityProjectionInput{{
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		RepositoryID: "repo-1",
		Roots:        []CodeReachabilityRoot{{EntityID: "entity:root"}},
		UpdatedAt:    now,
	}}
	loader := &probeFakeCodeReachabilityInputLoader{
		// Cycle 1: unknown cursor (audit) and productive. Cycle 2: probe
		// advances but the load is empty. Cycle 3: probe-equal skip.
		probeScript: []probeReading{{}, {at: now, ok: true}, {at: now, ok: true}},
		loadScript:  [][]CodeReachabilityProjectionInput{input, nil, nil},
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config: CodeReachabilityProjectionRunnerConfig{
			PollInterval:    10 * time.Millisecond,
			MaxPollInterval: time.Second,
			AuditInterval:   time.Hour,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waits []time.Duration
	// Records and returns immediately (see above): canceling on the last
	// expected wait ends the loop at the next loop-top check.
	runner.Wait = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		if len(waits) == 2 {
			cancel()
		}
		return nil
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(waits) != 2 || waits[0] != 10*time.Millisecond || waits[1] != 20*time.Millisecond {
		t.Fatalf("waits = %v, want [10ms 20ms] (productive reset then doubling)", waits)
	}
}

// collectIdleMetrics gathers the manual-reader metrics for the idle-probe
// instruments test.
func collectIdleMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

// TestIdleProbeInstrumentsRecordCycleKinds proves every cycle records its
// kind and productive cycles record which trigger found the inputs.
func TestIdleProbeInstrumentsRecordCycleKinds(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}

	now := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	loader := &probeFakeCodeReachabilityInputLoader{probe: now, probeOK: true}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   &fakeCodeReachabilityRowWriter{},
		Config:      CodeReachabilityProjectionRunnerConfig{BatchLimit: 10, AuditInterval: time.Hour},
		Instruments: inst,
	}

	if _, err := runner.ProcessNextCycle(context.Background(), now); err != nil {
		t.Fatalf("startup ProcessNextCycle() error = %v", err)
	}
	if _, err := runner.ProcessNextCycle(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("skip ProcessNextCycle() error = %v", err)
	}

	rm := collectIdleMetrics(t, reader)
	found := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_code_reachability_projection_cycles_total" {
				continue
			}
			// Int64Counter aggregates as Sum[int64], not Sum[float64].
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range sum.DataPoints {
					for _, attr := range dp.Attributes.ToSlice() {
						if string(attr.Key) == telemetry.MetricDimensionKind {
							found[attr.Value.AsString()] += dp.Value
						}
					}
				}
			}
		}
	}
	if found[telemetry.CodeReachabilityCycleKindAuditFull] != 1 || found[telemetry.CodeReachabilityCycleKindProbeSkip] != 1 {
		t.Fatalf("cycle kinds = %v, want audit_full=1 probe_skip=1", found)
	}
}
