// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceauditasync

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// noopSink is a fast sink for concurrency proofs that only need to observe
// enqueue-side behavior, not persistence timing.
type noopSink struct {
	receivedCount int64
}

func (s *noopSink) Append(_ context.Context, events []governanceaudit.Event) error {
	atomic.AddInt64(&s.receivedCount, int64(len(events)))
	return nil
}

// TestAsyncAppender_ConcurrentEnqueue_NoRaceNoDrops is the F-9 (#5170)
// addendum §4 concurrency proof item 1: 64 goroutines enqueue concurrently
// against a fast sink. Run with -race (see Makefile/CI invocation) to prove
// the channel send is the only shared operation and no other state is
// unsynchronized. With a fast-draining sink and a buffer sized well above the
// total burst, zero drops are expected.
func TestAsyncAppender_ConcurrentEnqueue_NoRaceNoDrops(t *testing.T) {
	t.Parallel()

	const goroutines = 64
	const perGoroutine = 200
	const total = goroutines * perGoroutine

	sink := &noopSink{}
	metrics, reader := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(total*2))

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				event := testEvent(fmt.Sprintf("g%d-e%d", id, i))
				if err := appender.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
					t.Errorf("Append() error = %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_emitted_total"), int64(total); got != want {
		t.Fatalf("emitted = %d, want %d (buffer sized well above burst, so no drops expected)", got, want)
	}
	if got := counterValue(t, reader, "eshu_dp_governance_audit_allowed_dropped_total"); got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
	if got := atomic.LoadInt64(&sink.receivedCount); got != int64(total) {
		t.Fatalf("sink received %d events, want %d", got, total)
	}
}

// TestAsyncAppender_ConcurrentBlockedSink_ExactDropAccounting is the F-9
// (#5170) addendum §4 concurrency proof item 2: with the worker stuck inside
// a blocked sink.Append, concurrent overflow enqueues must never block and
// the dropped counter must exactly equal the overflow count, proving
// backpressure is drop, never block, even under concurrent producers.
func TestAsyncAppender_ConcurrentBlockedSink_ExactDropAccounting(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{started: make(chan struct{}, 1), proceed: make(chan struct{})}
	metrics, reader := testMetrics(t)
	const capacity = 16
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(capacity))
	t.Cleanup(func() {
		close(sink.proceed)
		_ = appender.Close()
	})

	// Seed one event so the worker drains it and blocks inside Append,
	// leaving the buffer empty and holding it there deterministically.
	if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent("seed")}); err != nil {
		t.Fatalf("Append() seed error = %v", err)
	}
	select {
	case <-sink.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to enter blocked Append")
	}

	// Fill exactly capacity slots first (sequentially, deterministic count).
	for i := 0; i < capacity; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("fill-%d", i))}); err != nil {
			t.Fatalf("Append() fill %d error = %v", i, err)
		}
	}

	// Now overflow concurrently from many goroutines: buffer is full and
	// staying full (worker still blocked), so every one of these must drop.
	const overflowGoroutines = 32
	const overflowPerGoroutine = 5
	const overflow = overflowGoroutines * overflowPerGoroutine

	var wg sync.WaitGroup
	wg.Add(overflowGoroutines)
	maxElapsed := make([]time.Duration, overflowGoroutines)
	for g := 0; g < overflowGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			start := time.Now()
			for i := 0; i < overflowPerGoroutine; i++ {
				event := testEvent(fmt.Sprintf("overflow-%d-%d", id, i))
				if err := appender.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
					t.Errorf("Append() error = %v", err)
					return
				}
			}
			maxElapsed[id] = time.Since(start)
		}(g)
	}
	wg.Wait()

	for id, elapsed := range maxElapsed {
		if elapsed > time.Second {
			t.Fatalf("goroutine %d overflow enqueues took %v, want non-blocking (<1s for %d sends)", id, elapsed, overflowPerGoroutine)
		}
	}

	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_dropped_total"), int64(overflow); got != want {
		t.Fatalf("dropped = %d, want exactly %d", got, want)
	}
	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_emitted_total"), int64(1+capacity); got != want {
		t.Fatalf("emitted = %d, want exactly %d", got, want)
	}
}

// TestAsyncAppender_ConcurrentShutdown_SingleProducerFIFOWithinBound is the
// F-9 (#5170) addendum §4 concurrency proof item 3 (single-producer FIFO
// variant; the multi-producer case is covered by
// TestAsyncAppender_ConcurrentEnqueue_NoRaceNoDrops, which does not assert
// cross-goroutine order since none is guaranteed). One goroutine enqueues N
// events sequentially, then Close() must return within the shutdown timeout
// and the sink must have received exactly those N events in the order they
// were enqueued.
func TestAsyncAppender_ConcurrentShutdown_SingleProducerFIFOWithinBound(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{}
	metrics, _ := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(DefaultBufferCapacity), WithShutdownTimeout(5*time.Second))

	const n = 500 // <= DefaultBufferCapacity, per the addendum's proof scope
	for i := 0; i < n; i++ {
		event := testEvent(fmt.Sprintf("seq-%04d", i))
		if err := appender.Append(context.Background(), []governanceaudit.Event{event}); err != nil {
			t.Fatalf("Append() %d error = %v", i, err)
		}
	}

	start := time.Now()
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 5*time.Second {
		t.Fatalf("Close() took %v, want < 5s", elapsed)
	}

	got := sink.allReceived()
	if len(got) != n {
		t.Fatalf("sink received %d events, want %d", len(got), n)
	}
	for i, event := range got {
		want := fmt.Sprintf("seq-%04d", i)
		if event.CorrelationID != want {
			t.Fatalf("event[%d].CorrelationID = %q, want %q (FIFO order violated)", i, event.CorrelationID, want)
		}
	}
}

// TestAsyncAppender_CloseBoundedEvenWhenSinkNeverReturns proves Close()'s
// hard external contract: even a sink that ignores context cancellation and
// never returns cannot hang shutdown past the configured timeout.
func TestAsyncAppender_CloseBoundedEvenWhenSinkNeverReturns(t *testing.T) {
	t.Parallel()

	block := make(chan struct{}) // never closed during this test
	sink := &blockingForeverSink{block: block}
	metrics, _ := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(4), WithShutdownTimeout(200*time.Millisecond))

	if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent("stuck")}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	// Give the worker a moment to pick up the event and enter the blocking
	// Append call before we measure Close()'s bound.
	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	err := appender.Close()
	elapsed := time.Since(start)

	if err != ErrShutdownFlushIncomplete {
		t.Fatalf("Close() error = %v, want ErrShutdownFlushIncomplete", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Close() took %v, want bounded near the 200ms shutdown timeout", elapsed)
	}
}

// blockingForeverSink ignores context cancellation entirely, modeling a
// pathologically stuck sink so the Close() timeout proof is genuine (not
// merely relying on context propagation from the flush call).
type blockingForeverSink struct {
	block chan struct{}
}

func (s *blockingForeverSink) Append(context.Context, []governanceaudit.Event) error {
	<-s.block
	return nil
}

// BenchmarkAsyncAppenderEnqueueSerial and BenchmarkAsyncAppenderEnqueueParallel64
// are the F-9 (#5170) addendum §4 concurrency-proof companion measurement:
// enqueue ns/op should stay flat between one producer and 64 concurrent
// producers, since the buffered-channel send is the only shared operation.
// Both use a fast draining sink so the buffer never fills.
func BenchmarkAsyncAppenderEnqueueSerial(b *testing.B) {
	sink := &noopSink{}
	appender := NewAsyncAppender(sink, Metrics{}, WithBufferCapacity(DefaultBufferCapacity))
	defer func() { _ = appender.Close() }()
	event := testBenchmarkEvent()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = appender.Append(context.Background(), []governanceaudit.Event{event})
	}
}

func BenchmarkAsyncAppenderEnqueueParallel64(b *testing.B) {
	sink := &noopSink{}
	appender := NewAsyncAppender(sink, Metrics{}, WithBufferCapacity(DefaultBufferCapacity))
	defer func() { _ = appender.Close() }()
	event := testBenchmarkEvent()

	b.ReportAllocs()
	b.SetParallelism(64)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = appender.Append(context.Background(), []governanceaudit.Event{event})
		}
	})
}

func testBenchmarkEvent() governanceaudit.Event {
	return testEvent("bench-corr")
}
