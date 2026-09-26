// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// describingQuiescence is a canonical-code quiescence checker that also names
// its blockers, as postgres.ReducerGraphDrain does.
type describingQuiescence struct {
	mu          sync.Mutex
	uncommitted bool
	total       int
	ids         []string
	describeErr error
	describes   int
}

func (d *describingQuiescence) HasUncommittedCanonicalCodeScopes(context.Context) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.uncommitted, nil
}

func (d *describingQuiescence) DescribeUncommittedCanonicalCodeScopes(_ context.Context, limit int) (int, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.describes++
	if limit != blockedScopeSampleLimit {
		return 0, nil, errors.New("unexpected sample limit")
	}
	return d.total, d.ids, d.describeErr
}

func (d *describingQuiescence) set(uncommitted bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.uncommitted = uncommitted
}

func (d *describingQuiescence) describeCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.describes
}

type blockedTelemetryHarness struct {
	reader *sdkmetric.ManualReader
	logs   *bytes.Buffer
	runner *Runner
}

func newBlockedTelemetryHarness(t *testing.T, configure func(*Runner)) blockedTelemetryHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	var logs bytes.Buffer
	store := &fakeCodeCallIntentStore{leaseGranted: true}
	runner := &Runner{
		IntentReader: store,
		LeaseManager: store,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  func(sharedintent.AcceptanceKey) (string, bool) { return "", false },
		Config:       RunnerConfig{BatchLimit: 10},
		Instruments:  instruments,
		Logger:       telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &logs),
	}
	configure(runner)
	return blockedTelemetryHarness{reader: reader, logs: &logs, runner: runner}
}

func (h blockedTelemetryHarness) process(t *testing.T) {
	t.Helper()
	if _, err := h.runner.processOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
}

func (h blockedTelemetryHarness) logEntries(t *testing.T, message string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(h.logs.Bytes()))
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("json.Unmarshal(%q) error = %v", scanner.Text(), err)
		}
		if entry["message"] == message || entry["msg"] == message {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (h blockedTelemetryHarness) metrics(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

func laneAttrs(reason string) attribute.Set {
	return attribute.NewSet(
		attribute.String("domain", "code_calls"),
		attribute.String("reason", reason),
	)
}

func blockedCounterValue(rm metricdata.ResourceMetrics, reason string) int64 {
	want := laneAttrs(reason)
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_shared_projection_lane_blocked_total" {
				continue
			}
			sum, _ := m.Data.(metricdata.Sum[int64])
			for _, point := range sum.DataPoints {
				if point.Attributes.Equals(&want) {
					return point.Value
				}
			}
		}
	}
	return -1
}

func blockingScopesGauge(rm metricdata.ResourceMetrics, reason string) int64 {
	want := laneAttrs(reason)
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_shared_projection_lane_blocking_scopes" {
				continue
			}
			gauge, _ := m.Data.(metricdata.Gauge[int64])
			for _, point := range gauge.DataPoints {
				if point.Attributes.Equals(&want) {
					return point.Value
				}
			}
		}
	}
	return -1
}

// TestCodeCallProjectionRunnerReportsQuiescenceBlock is the #7133
// observability regression: the code_calls lane sat blocked for eight days
// with no log line and no metric. A blocked cycle must count on
// eshu_dp_shared_projection_lane_blocked_total, publish the blocking scope
// count, and log the blocking scope ids once per episode (then at a bounded
// interval), and the release must be logged.
func TestCodeCallProjectionRunnerReportsQuiescenceBlock(t *testing.T) {
	t.Parallel()

	gate := &describingQuiescence{uncommitted: true, total: 2, ids: []string{"aws:x:us-east-1:ecs", "eshu:global"}}
	h := newBlockedTelemetryHarness(t, func(r *Runner) { r.CanonicalQuiescence = gate })

	h.process(t)
	h.process(t)

	rm := h.metrics(t)
	if got := blockedCounterValue(rm, BlockedReasonCanonicalCodeQuiescence); got != 2 {
		t.Fatalf("lane_blocked_total{canonical_code_quiescence} = %d, want 2", got)
	}
	if got := blockingScopesGauge(rm, BlockedReasonCanonicalCodeQuiescence); got != 2 {
		t.Fatalf("lane_blocking_scopes{canonical_code_quiescence} = %d, want 2", got)
	}
	if got := gate.describeCount(); got != 1 {
		t.Fatalf("describe calls = %d, want 1 (rate-limited per episode)", got)
	}
	blocked := h.logEntries(t, "code call projection lane blocked")
	if len(blocked) != 1 {
		t.Fatalf("blocked log lines = %d, want 1", len(blocked))
	}
	entry := blocked[0]
	if got, want := entry["blocked_reason"], BlockedReasonCanonicalCodeQuiescence; got != want {
		t.Fatalf("blocked_reason = %v, want %v", got, want)
	}
	if got, want := entry["blocking_scope_count"], float64(2); got != want {
		t.Fatalf("blocking_scope_count = %v, want %v", got, want)
	}
	if got, want := entry["blocking_scope_ids"], []any{"aws:x:us-east-1:ecs", "eshu:global"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("blocking_scope_ids = %v, want %v", got, want)
	}

	gate.set(false)
	h.process(t)
	h.process(t)
	if got := len(h.logEntries(t, "code call projection lane released")); got != 1 {
		t.Fatalf("released log lines = %d, want 1", got)
	}
	if got := blockingScopesGauge(h.metrics(t), BlockedReasonCanonicalCodeQuiescence); got != 0 {
		t.Fatalf("lane_blocking_scopes after release = %d, want 0", got)
	}
}

func TestCodeCallProjectionRunnerReportsReducerGraphWorkBlock(t *testing.T) {
	t.Parallel()

	gate := &describingQuiescence{total: 9}
	h := newBlockedTelemetryHarness(t, func(r *Runner) {
		r.ReducerGraphDrain = staticReducerGraphDrain{active: true}
		r.CanonicalQuiescence = gate
	})

	h.process(t)

	if got := blockedCounterValue(h.metrics(t), BlockedReasonReducerGraphWork); got != 1 {
		t.Fatalf("lane_blocked_total{reducer_graph_work_active} = %d, want 1", got)
	}
	if got := gate.describeCount(); got != 0 {
		t.Fatalf("describe calls = %d, want 0 when reducer graph work holds the lane", got)
	}
	blocked := h.logEntries(t, "code call projection lane blocked")
	if len(blocked) != 1 || blocked[0]["blocked_reason"] != BlockedReasonReducerGraphWork {
		t.Fatalf("blocked log = %v, want one line with reason %s", blocked, BlockedReasonReducerGraphWork)
	}
}

// A describe failure must not fail or unblock the cycle: the gate already
// answered, and the sample is operator detail only.
func TestCodeCallProjectionRunnerDescribeErrorKeepsCycleBlocked(t *testing.T) {
	t.Parallel()

	gate := &describingQuiescence{uncommitted: true, describeErr: errors.New("describe boom")}
	h := newBlockedTelemetryHarness(t, func(r *Runner) { r.CanonicalQuiescence = gate })

	result, err := h.runner.processOnce(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1", result.BlockedReadiness)
	}
	blocked := h.logEntries(t, "code call projection lane blocked")
	if len(blocked) != 1 || blocked[0]["error"] == nil {
		t.Fatalf("blocked log = %v, want one line carrying the describe error", blocked)
	}
}

func TestLaneBlockStateReportsOncePerIntervalAndOnRelease(t *testing.T) {
	t.Parallel()

	var state laneBlockState
	start := time.Unix(1_000, 0)
	if report, _, _ := state.observe(start, BlockedReasonCanonicalCodeQuiescence); !report {
		t.Fatal("first blocked observation must report")
	}
	if report, _, _ := state.observe(start.Add(blockedReportInterval/2), BlockedReasonCanonicalCodeQuiescence); report {
		t.Fatal("observation inside the interval must not report")
	}
	report, blockedFor, _ := state.observe(start.Add(blockedReportInterval), BlockedReasonCanonicalCodeQuiescence)
	if !report || blockedFor != blockedReportInterval.Seconds() {
		t.Fatalf("observe at interval = (%v, %v), want (true, %v)", report, blockedFor, blockedReportInterval.Seconds())
	}
	report, _, replaced := state.observe(start.Add(blockedReportInterval+time.Second), BlockedReasonReducerGraphWork)
	if !report || replaced != BlockedReasonCanonicalCodeQuiescence {
		t.Fatalf("reason change = (%v, %q), want (true, %q)", report, replaced, BlockedReasonCanonicalCodeQuiescence)
	}
	released, reason, _ := state.release(start.Add(2 * blockedReportInterval))
	if !released || reason != BlockedReasonReducerGraphWork {
		t.Fatalf("release = (%v, %q), want (true, %q)", released, reason, BlockedReasonReducerGraphWork)
	}
	if released, _, _ := state.release(start.Add(3 * blockedReportInterval)); released {
		t.Fatal("release without a blocked episode must not report")
	}
}
