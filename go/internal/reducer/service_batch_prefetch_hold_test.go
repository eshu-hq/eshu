// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// timedStartExecutor blocks every execution on release like
// blockingManyReducerExecutor and additionally records the wall time each
// execution starts, so hold tests can count how many heartbeats landed
// during the dwell before that start.
type timedStartExecutor struct {
	release chan struct{}
	started chan string
	mu      sync.Mutex
	times   map[string]time.Time
	count   int
}

func (e *timedStartExecutor) Execute(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	e.mu.Lock()
	e.count++
	if e.times == nil {
		e.times = make(map[string]time.Time)
	}
	e.times[intent.IntentID] = time.Now()
	e.mu.Unlock()
	select {
	case e.started <- intent.IntentID:
	case <-ctx.Done():
		return reducercontract.Result{}, ctx.Err()
	}
	select {
	case <-e.release:
		return reducercontract.Result{
			IntentID: intent.IntentID,
			Domain:   intent.Domain,
			Status:   ResultStatusSucceeded,
		}, nil
	case <-ctx.Done():
		return reducercontract.Result{}, ctx.Err()
	}
}

func (e *timedStartExecutor) startTime(id string) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.times[id]
}

// TestServiceRunBatchPrefetchHoldRenewsAcrossDwell proves a buffered item's
// hold heartbeat keeps renewing the lease while workers are busy: with a
// 5ms interval and workers blocked 100ms, the two buffered intents each
// collect hold heartbeats before their execution starts, then succeed and
// ack with no lease-lost record.
func TestServiceRunBatchPrefetchHoldRenewsAcrossDwell(t *testing.T) {
	t.Parallel()

	intents := makeTestIntents(4)
	source := &fakeBatchWorkSource{intents: intents}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &timedStartExecutor{release: release, started: make(chan string, 4)}
	heartbeater := newTimedHeartbeater()
	instruments, reader := batchTelemetry(t)

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Heartbeater:        heartbeater,
		HeartbeatInterval:  5 * time.Millisecond,
		Workers:            2,
		BatchClaimSize:     4,
		BatchPrefetchDepth: 2,
		Instruments:        instruments,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- service.runMainLoop(ctx)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-executor.started:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for worker %d to start", i)
		}
	}
	// Dwell the buffered intents across many heartbeat intervals.
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		t.Fatalf("context ended during dwell: %v", ctx.Err())
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}

	// FIFO pop order puts intent-0 and intent-1 on the workers first, so
	// intent-2 and intent-3 are the buffered pair whose holds must renew.
	for _, id := range []string{"intent-2", "intent-3"} {
		start := executor.startTime(id)
		if start.IsZero() {
			t.Fatalf("no execution start recorded for buffered %s", id)
		}
		if got := heartbeater.callsBefore(id, start); got < 2 {
			t.Fatalf("hold heartbeat calls for buffered %s before execution = %d, want at least 2 across the dwell", id, got)
		}
	}
	sink.mu.Lock()
	acked := sink.acked
	sink.mu.Unlock()
	if acked != 4 {
		t.Fatalf("sink acked = %d, want 4 (every held item succeeds after its dwell)", acked)
	}
	counts, _ := batchMetricStatusCounts(t, reader)
	for _, status := range []string{"lease_lost_before_start", "lease_lost_during_execution"} {
		if counts[status] != 0 {
			t.Fatalf("status %q count = %d, want 0 (holds renewed every lease)", status, counts[status])
		}
	}
}

// TestServiceRunBatchPrefetchHoldStopsBeforeWorkerHeartbeat proves the pop
// ordering: the hold stops before the worker heartbeat starts, so the two
// loops for one intent never overlap. The recording heartbeater sleeps 2ms
// per call to widen any overlap window; strict sequencing keeps the per
// intent max in-flight at exactly 1 while every intent still executes once.
func TestServiceRunBatchPrefetchHoldStopsBeforeWorkerHeartbeat(t *testing.T) {
	t.Parallel()

	intents := makeTestIntents(4)
	source := &fakeBatchWorkSource{intents: intents}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &timedStartExecutor{release: release, started: make(chan string, 4)}
	heartbeater := newInflightHeartbeater(2 * time.Millisecond)

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Heartbeater:        heartbeater,
		HeartbeatInterval:  5 * time.Millisecond,
		Workers:            2,
		BatchClaimSize:     4,
		BatchPrefetchDepth: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- service.runMainLoop(ctx)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-executor.started:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for worker %d to start", i)
		}
	}
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		t.Fatalf("context ended during dwell: %v", ctx.Err())
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}

	for _, intent := range intents {
		if got := heartbeater.maxInflight(intent.IntentID); got != 1 {
			t.Fatalf("max in-flight heartbeats for %s = %d, want exactly 1 (hold stops before the worker heartbeat starts)", intent.IntentID, got)
		}
	}
	executor.mu.Lock()
	executions := executor.count
	executor.mu.Unlock()
	if executions != 4 {
		t.Fatalf("executions = %d, want 4", executions)
	}
}

// TestServiceRunBatchPrefetchHoldFailureEvictsWithoutExecute proves a hold
// that dies during the dwell evicts the item: the executor never sees the
// buffered intents, nothing is acked or failed for them, the hold-failure
// signal fires, and the slot frees so later work still flows. Ticks for
// intent-2 and intent-3 fail while intent-0 and intent-1 tick cleanly; the
// 100ms dwell against a 1ms interval makes the failure certain.
func TestServiceRunBatchPrefetchHoldFailureEvictsWithoutExecute(t *testing.T) {
	t.Parallel()

	intents := makeTestIntents(4)
	source := &fakeBatchWorkSource{intents: intents}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &timedStartExecutor{release: release, started: make(chan string, 4)}
	heartbeater := newTimedHeartbeater("intent-2", "intent-3")
	instruments, reader := batchTelemetry(t)

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Heartbeater:        heartbeater,
		HeartbeatInterval:  time.Millisecond,
		Workers:            2,
		BatchClaimSize:     4,
		BatchPrefetchDepth: 2,
		Instruments:        instruments,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() {
		errc <- service.runMainLoop(ctx)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-executor.started:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for worker %d to start", i)
		}
	}
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		t.Fatalf("context ended during dwell: %v", ctx.Err())
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}

	for _, id := range []string{"intent-2", "intent-3"} {
		if start := executor.startTime(id); !start.IsZero() {
			t.Fatalf("evicted %s executed at %v, want no execution after its hold died", id, start)
		}
	}
	sink.mu.Lock()
	acked, failed := sink.acked, sink.failed
	sink.mu.Unlock()
	if acked != 2 {
		t.Fatalf("sink acked = %d, want 2 (only the healthy pair)", acked)
	}
	if failed != 0 {
		t.Fatalf("sink failed = %d, want 0 (eviction never routes through Fail)", failed)
	}
	if got := prefetchHoldFailedCount(t, reader); got != 2 {
		t.Fatalf("hold-failure observations = %d, want 2 (one per evicted buffered item)", got)
	}
	counts, _ := batchMetricStatusCounts(t, reader)
	if counts["ack_outcome_unknown"] != 2 {
		t.Fatalf("ack_outcome_unknown count = %d, want 2 (one per evicted item, so claimed work never vanishes)", counts["ack_outcome_unknown"])
	}
	if got := prefetchDwellCount(t, reader); got != 2 {
		t.Fatalf("buffer dwell observations = %d, want 2 (only executed items record a dwell; evicted items record none)", got)
	}
}

// TestServiceRunBatchPrefetchHoldStartFailureEvictsAtInsert proves a hold
// that fails its synchronous pre-heartbeat never enters the buffer: nothing
// executes, nothing acks or fails, both items record the hold-failure signal
// and the unknown outcome, and the run drains cleanly.
func TestServiceRunBatchPrefetchHoldStartFailureEvictsAtInsert(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(2)}
	sink := &fakeBatchWorkSink{}
	instruments, reader := batchTelemetry(t)

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           &countingExecutor{},
		WorkSink:           sink,
		Heartbeater:        failPreheartbeatsHeartbeater{},
		HeartbeatInterval:  time.Hour,
		Workers:            2,
		BatchClaimSize:     2,
		BatchPrefetchDepth: 2,
		Instruments:        instruments,
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.runMainLoop(context.Background()); err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	sink.mu.Lock()
	acked, failed := sink.acked, sink.failed
	sink.mu.Unlock()
	if acked != 0 || failed != 0 {
		t.Fatalf("sink acked/failed = %d/%d, want 0/0 (pre-heartbeat failure evicts before execution)", acked, failed)
	}
	if got := prefetchHoldFailedCount(t, reader); got != 2 {
		t.Fatalf("hold-failure observations = %d, want 2 (one per evicted claim)", got)
	}
	counts, _ := batchMetricStatusCounts(t, reader)
	if counts["ack_outcome_unknown"] != 2 {
		t.Fatalf("ack_outcome_unknown count = %d, want 2 (evicted claims stay observable)", counts["ack_outcome_unknown"])
	}
	if got := prefetchDwellCount(t, reader); got != 0 {
		t.Fatalf("buffer dwell observations = %d, want 0 (nothing reached execution)", got)
	}
}

// TestServiceRunBatchPrefetchExpiryFenceNoDoubleExecute proves the worker
// pre-heartbeat fence in the prefetch path: after a clean hold stop, a dead
// worker heartbeat records lease_lost_before_start without calling Execute
// and without acking, mirroring the per-item path.
func TestServiceRunBatchPrefetchExpiryFenceNoDoubleExecute(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(2)}
	sink := &fakeBatchWorkSink{}
	executor := &countingExecutor{}
	instruments, reader := batchTelemetry(t)

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Heartbeater:        &failSecondCallHeartbeater{},
		HeartbeatInterval:  time.Hour,
		Workers:            2,
		BatchClaimSize:     2,
		BatchPrefetchDepth: 2,
		Instruments:        instruments,
	}

	// A timeout context ends the run after the assertions settle: cancelling
	// on the first empty poll would race the buffered items still waiting
	// for their pop.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.runMainLoop(ctx); err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	if got := executor.count.Load(); got != 0 {
		t.Fatalf("executor count = %d, want 0 (no handler work runs under a claim that lost its lease before starting)", got)
	}
	sink.mu.Lock()
	acked, failed := sink.acked, sink.failed
	sink.mu.Unlock()
	if acked != 0 || failed != 0 {
		t.Fatalf("sink acked/failed = %d/%d, want 0/0 (nothing to ack, nothing to dead-letter)", acked, failed)
	}
	counts, _ := batchMetricStatusCounts(t, reader)
	if counts["lease_lost_before_start"] != 2 {
		t.Fatalf("lease_lost_before_start count = %d, want 2", counts["lease_lost_before_start"])
	}
}

func prefetchHoldFailedCount(t *testing.T, reader *metric.ManualReader) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_batch_buffer_hold_heartbeat_failed_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("hold-failure data = %T, want counter sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				total += point.Value
			}
		}
	}
	return total
}

func prefetchDwellCount(t *testing.T, reader *metric.ManualReader) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_batch_buffer_dwell_seconds" {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("dwell data = %T, want histogram", m.Data)
			}
			for _, point := range histogram.DataPoints {
				total += int64(point.Count)
			}
		}
	}
	return total
}
