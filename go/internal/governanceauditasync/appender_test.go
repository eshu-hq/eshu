// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceauditasync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeSink is a test Appender. When started is non-nil, every Append call
// signals it (non-blocking, best-effort) before optionally blocking on
// proceed, letting tests deterministically synchronize with the single
// background worker.
type fakeSink struct {
	mu       sync.Mutex
	received [][]governanceaudit.Event
	err      error

	started chan struct{}
	proceed chan struct{}
}

func (s *fakeSink) Append(ctx context.Context, events []governanceaudit.Event) error {
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.proceed != nil {
		select {
		case <-s.proceed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	batch := append([]governanceaudit.Event(nil), events...)
	s.received = append(s.received, batch)
	return s.err
}

func (s *fakeSink) allReceived() []governanceaudit.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []governanceaudit.Event
	for _, batch := range s.received {
		all = append(all, batch...)
	}
	return all
}

func (s *fakeSink) batchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.received)
}

func testEvent(correlationID string) governanceaudit.Event {
	return governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         governanceaudit.ActorClassScopedToken,
		ActorIDHash:        "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd",
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           governanceaudit.DecisionAllowed,
		ReasonCode:         "scoped_read_allowed",
		CorrelationID:      correlationID,
		PolicyRevisionHash: "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba98765432",
		OccurredAt:         time.Now().UTC(),
	}
}

// testMetrics registers the three counters against a manual reader so tests
// can assert exact values without a process-global meter provider.
func testMetrics(t *testing.T) (Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	meter := provider.Meter("governanceauditasync_test")

	emitted, err := meter.Int64Counter("eshu_dp_governance_audit_allowed_emitted_total")
	if err != nil {
		t.Fatalf("register emitted counter: %v", err)
	}
	dropped, err := meter.Int64Counter("eshu_dp_governance_audit_allowed_dropped_total")
	if err != nil {
		t.Fatalf("register dropped counter: %v", err)
	}
	persistFailures, err := meter.Int64Counter("eshu_dp_governance_audit_allowed_persist_failures_total")
	if err != nil {
		t.Fatalf("register persist failures counter: %v", err)
	}
	return Metrics{Emitted: emitted, Dropped: dropped, PersistFailures: persistFailures}, reader
}

func counterValue(t *testing.T, reader *sdkmetric.ManualReader, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q data = %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

func TestAsyncAppender_AppendNeverBlocksEvenWhenFull(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{started: make(chan struct{}, 1), proceed: make(chan struct{})}
	metrics, _ := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(4))
	t.Cleanup(func() {
		close(sink.proceed)
		_ = appender.Close()
	})

	// Seed one event so the worker drains it and blocks inside Append,
	// leaving the buffer empty and deterministically full-able.
	if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent("seed")}); err != nil {
		t.Fatalf("Append() seed error = %v", err)
	}
	select {
	case <-sink.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to enter blocked Append")
	}

	// Fill the buffer, then overflow it well past capacity. Every call must
	// return quickly regardless of buffer state.
	for i := 0; i < 20; i++ {
		start := time.Now()
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("fill-%d", i))}); err != nil {
			t.Fatalf("Append() %d error = %v", i, err)
		}
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("Append() %d took %v, want non-blocking", i, elapsed)
		}
	}
}

func TestAsyncAppender_FullBufferDropsExactCount(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{started: make(chan struct{}, 1), proceed: make(chan struct{})}
	metrics, reader := testMetrics(t)
	const capacity = 8
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(capacity))
	t.Cleanup(func() {
		close(sink.proceed)
		_ = appender.Close()
	})

	// Seed one event so the worker picks it up and blocks inside Append,
	// draining the channel to empty and holding it there.
	if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent("seed")}); err != nil {
		t.Fatalf("Append() seed error = %v", err)
	}
	select {
	case <-sink.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to enter blocked Append")
	}

	// Fill exactly capacity slots.
	for i := 0; i < capacity; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("fill-%d", i))}); err != nil {
			t.Fatalf("Append() fill %d error = %v", i, err)
		}
	}

	// Overflow by exactly 3; each must be dropped.
	const overflow = 3
	for i := 0; i < overflow; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("overflow-%d", i))}); err != nil {
			t.Fatalf("Append() overflow %d error = %v", i, err)
		}
	}

	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_dropped_total"), int64(overflow); got != want {
		t.Fatalf("dropped = %d, want exactly %d", got, want)
	}
	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_emitted_total"), int64(1+capacity); got != want {
		t.Fatalf("emitted = %d, want exactly %d", got, want)
	}
}

func TestAsyncAppender_ShutdownFlushDrainsInFIFOOrder(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	metrics, _ := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(64))

	const n = 10
	for i := 0; i < n; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("event-%02d", i))}); err != nil {
			t.Fatalf("Append() %d error = %v", i, err)
		}
	}

	start := time.Now()
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Close() took %v, want < 5s", elapsed)
	}

	got := sink.allReceived()
	if len(got) != n {
		t.Fatalf("sink received %d events, want %d", len(got), n)
	}
	for i, event := range got {
		want := fmt.Sprintf("event-%02d", i)
		if event.CorrelationID != want {
			t.Fatalf("event[%d].CorrelationID = %q, want %q (FIFO order violated): %#v", i, event.CorrelationID, want, got)
		}
	}
}

func TestAsyncAppender_EnqueueAfterCloseDropsCleanlyNoPanic(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	metrics, reader := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(4))

	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Must not panic on a "closed channel" and must count as dropped.
	if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent("after-close")}); err != nil {
		t.Fatalf("Append() after Close() error = %v, want nil (best-effort)", err)
	}

	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_dropped_total"), int64(1); got != want {
		t.Fatalf("dropped = %d, want %d", got, want)
	}
	if sink.batchCount() != 0 {
		t.Fatalf("sink received %d batches after close, want 0", sink.batchCount())
	}
}

func TestAsyncAppender_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	metrics, _ := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(4))

	if err := appender.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil (idempotent)", err)
	}
}

// TestAsyncAppender_PersistFailureIncrementsMetricForEveryFailedEvent proves
// a sink that rejects every event counts one persist failure per event.
// (Fault-isolation of a single poison event among well-formed siblings — the
// F-9 (#5170) P1 defense — is covered by
// TestAsyncAppender_MalformedEventDoesNotDropBatchSiblings in
// appender_fault_isolation_test.go; this test guards the total-outage
// counting the per-event fallback must not under-count.)
func TestAsyncAppender_PersistFailureIncrementsMetricForEveryFailedEvent(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{err: errors.New("insert failed")}
	metrics, reader := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(64))

	const n = 5
	for i := 0; i < n; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("event-%d", i))}); err != nil {
			t.Fatalf("Append() %d error = %v", i, err)
		}
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_persist_failures_total"), int64(n); got != want {
		t.Fatalf("persist failures = %d, want %d", got, want)
	}
	// Emitted still counts (accepted into buffer); persist failure is a
	// separate, additional signal, not a replacement for emitted.
	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_emitted_total"), int64(n); got != want {
		t.Fatalf("emitted = %d, want %d", got, want)
	}
}

func TestAsyncAppender_BatchesUpToMaxPerFlush(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	metrics, _ := testMetrics(t)
	const batchMax = 3
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(64), WithBatchMax(batchMax))

	// Enqueue well beyond one batch's worth before the worker can drain, then
	// close and let the final flush(es) happen. No batch may exceed batchMax.
	const n = 10
	for i := 0; i < n; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("event-%d", i))}); err != nil {
			t.Fatalf("Append() %d error = %v", i, err)
		}
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	for i, batch := range sink.received {
		if len(batch) > batchMax {
			t.Fatalf("batch[%d] size = %d, want <= %d", i, len(batch), batchMax)
		}
	}
	var total int
	for _, batch := range sink.received {
		total += len(batch)
	}
	if total != n {
		t.Fatalf("total events flushed = %d, want %d", total, n)
	}
}

func TestAsyncAppender_DefaultBufferCapacityIs1024(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	metrics, _ := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics)
	t.Cleanup(func() { _ = appender.Close() })

	if got, want := cap(appender.buf), DefaultBufferCapacity; got != want {
		t.Fatalf("default buffer capacity = %d, want %d", got, want)
	}
	if want := 1024; DefaultBufferCapacity != want {
		t.Fatalf("DefaultBufferCapacity = %d, want %d (F-9 addendum contract)", DefaultBufferCapacity, want)
	}
}

func TestAsyncAppender_NilMetricsFieldsAreSafe(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	appender := NewAsyncAppender(sink, Metrics{}, WithBufferCapacity(4))

	if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent("no-metrics")}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestAsyncAppender_EmptyEventsSliceIsNoOp(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	metrics, reader := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(4))

	if err := appender.Append(context.Background(), nil); err != nil {
		t.Fatalf("Append(nil) error = %v", err)
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := counterValue(t, reader, "eshu_dp_governance_audit_allowed_emitted_total"); got != 0 {
		t.Fatalf("emitted = %d, want 0 for an empty batch", got)
	}
}
