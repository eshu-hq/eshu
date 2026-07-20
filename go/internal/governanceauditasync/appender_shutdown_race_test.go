// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceauditasync

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// countingSink counts every event handed to it. The single worker is the only
// caller, so the atomic is defensive; the test reads it only after Close()
// returns (which happens-after the worker's final flush via done).
type countingSink struct {
	persisted int64
}

func (s *countingSink) Append(_ context.Context, events []governanceaudit.Event) error {
	atomic.AddInt64(&s.persisted, int64(len(events)))
	return nil
}

// TestAsyncAppender_ShutdownRaceAccountingInvariant is the F-9 (#5170) second
// P1 regression: truthful loss accounting across a concurrent shutdown. The
// pre-fix enqueue did a closed-check select and then a SEPARATE non-blocking
// send. If Close closed the intake signal between those two steps, the worker
// could take its shutdown branch and EXIT, yet the producer's send still
// succeeded into the never-closed buffered channel — an event counted
// `emitted` but never persisted and never counted dropped/persist-failed:
// emitted-but-lost, while emitted_total overcounts.
//
// Many producers Append concurrently while another goroutine calls Close near
// the start, so the shutdown overlaps in-flight enqueues. After Close+done,
// every event Append was called with MUST be accounted exactly once:
//
//	emitted == persisted + persist_failures   (no emitted-but-lost)
//	emitted + dropped == total                (exactly one outcome per event)
//
// A small buffer keeps the worker draining to near-empty, maximizing the
// chance its shutdown branch exits while a producer is mid-enqueue — the exact
// window the fix closes. Run with -race -count to sweep interleavings.
func TestAsyncAppender_ShutdownRaceAccountingInvariant(t *testing.T) {
	t.Parallel()

	const producers = 64
	const perProducer = 100
	const total = producers * perProducer

	sink := &countingSink{}
	metrics, reader := testMetrics(t)
	// Deliberately small buffer: full-buffer drops are legitimate (counted
	// dropped, never emitted) and do not perturb the emitted==persisted+
	// persist_failures invariant, while a near-empty buffer maximizes the
	// shutdown-exit-mid-enqueue window.
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(16))

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(producers)
	for p := 0; p < producers; p++ {
		go func(id int) {
			defer wg.Done()
			<-start
			for i := 0; i < perProducer; i++ {
				_ = appender.Append(
					context.Background(),
					[]governanceaudit.Event{testEvent(fmt.Sprintf("p%d-e%d", id, i))},
				)
			}
		}(p)
	}

	// Close overlaps the very start of the producer burst.
	closeErr := make(chan error, 1)
	go func() {
		<-start
		closeErr <- appender.Close()
	}()

	close(start)
	wg.Wait()
	if err := <-closeErr; err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	persisted := atomic.LoadInt64(&sink.persisted)
	emitted := counterValue(t, reader, "eshu_dp_governance_audit_allowed_emitted_total")
	dropped := counterValue(t, reader, "eshu_dp_governance_audit_allowed_dropped_total")
	persistFailures := counterValue(t, reader, "eshu_dp_governance_audit_allowed_persist_failures_total")

	// The discriminating invariant: every event counted `emitted` must have
	// reached the sink or been counted a persist failure. A positive gap is an
	// emitted-but-lost event orphaned in the buffer after the worker exited.
	if emitted != persisted+persistFailures {
		t.Fatalf(
			"emitted (%d) != persisted (%d) + persist_failures (%d): %d event(s) counted emitted but never persisted or failed — emitted-but-lost",
			emitted, persisted, persistFailures, emitted-(persisted+persistFailures),
		)
	}
	// Sanity: every Append'd event took exactly one outcome.
	if emitted+dropped != int64(total) {
		t.Fatalf("emitted (%d) + dropped (%d) = %d, want total %d", emitted, dropped, emitted+dropped, total)
	}
}
