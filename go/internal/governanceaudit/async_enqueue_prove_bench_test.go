// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceaudit

import (
	"testing"
	"time"
)

// asyncEnqueueProveBufferCap mirrors the F-9 (#5170) addendum's proposed
// AsyncAppender buffer capacity (§3): chan capacity 1024.
const asyncEnqueueProveBufferCap = 1024

// asyncEnqueueProveProbe is a throwaway shim for the F-9 (#5170)
// prove-theory-first Bench A. It is NOT the production AsyncAppender — it
// exists only to measure the cost of the non-blocking buffered-channel send
// the real design's Append() would perform: an Event struct copy plus one
// channel send, with drop-on-full backpressure instead of blocking. No
// worker goroutine drains the channel during the timed loop, matching the
// "enqueue cost only" scope of Bench A (the addendum's real decision driver
// is Bench A vs Bench B, not a full async pipeline).
type asyncEnqueueProveProbe struct {
	buf     chan Event
	dropped int
}

func newAsyncEnqueueProveProbe(capacity int) *asyncEnqueueProveProbe {
	return &asyncEnqueueProveProbe{buf: make(chan Event, capacity)}
}

// enqueue is the non-blocking select-default-drop send under proof (addendum
// §3 "Enqueue (Append)"): buffer full -> drop and count, never block.
func (p *asyncEnqueueProveProbe) enqueue(event Event) {
	select {
	case p.buf <- event:
	default:
		p.dropped++
	}
}

// BenchmarkAsyncEnqueueProve is the F-9 (#5170) prove-theory-first Bench A
// proxy: the cost of a non-blocking buffered-channel send standing in for
// AsyncAppender.Append's enqueue path (addendum §3, §4). No Postgres
// involved — draining/persist cost is out of scope for this measurement.
func BenchmarkAsyncEnqueueProve(b *testing.B) {
	probe := newAsyncEnqueueProveProbe(asyncEnqueueProveBufferCap)
	// Drain concurrently so the send side is exercised against a live
	// receiver rather than a channel nobody reads (which would only measure
	// the cheaper cap-check-and-drop branch once full). A single Go
	// goroutine cannot keep up with a tight b.N enqueue loop on a modern
	// core, so the buffer fills and most sends take the drop branch — that
	// is fine and expected: Bench A's decision numbers are ns/op and
	// allocs/op for the enqueue call itself (the addendum's disqualifiers,
	// §4), not a zero-drop guarantee. The drop rate under real traffic is a
	// separate, later concurrency proof (addendum §4 item 2), not this
	// benchmark.
	stop := make(chan struct{})
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case <-probe.buf:
			case <-stop:
				for {
					select {
					case <-probe.buf:
					default:
						return
					}
				}
			}
		}
	}()

	event := asyncEnqueueProveBenchmarkEvent()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		probe.enqueue(event)
	}
	b.StopTimer()

	close(stop)
	<-drainDone
	b.ReportMetric(float64(probe.dropped)/float64(b.N), "drop_ratio")
}

// asyncEnqueueProveBenchmarkEvent mirrors the allowed read_authorization
// event shape from the addendum's §5 field table, matching the event Bench B
// appends, so both benchmarks measure the same payload shape.
func asyncEnqueueProveBenchmarkEvent() Event {
	return Event{
		Type:               EventTypeReadAuthorization,
		ActorClass:         ActorClassScopedToken,
		ActorIDHash:        "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd",
		ScopeClass:         ScopeClassAdmin,
		Decision:           DecisionAllowed,
		ReasonCode:         "scoped_read_allowed",
		CorrelationID:      "bench-corr-async",
		PolicyRevisionHash: "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba98765432",
		OccurredAt:         time.Now().UTC(),
	}
}
