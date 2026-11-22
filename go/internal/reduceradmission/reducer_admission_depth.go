// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"go.opentelemetry.io/otel/metric"
)

// reducerAdmissionBackoffCap is the ceiling of the read interval while the
// gate defers (#11233). The interval starts at the poll interval and doubles
// per deferred read up to this cap, or up to half the max wait when that is
// smaller, so a waiter still gets fresh reads inside its max wait. A poll
// interval above the cap disables the growth.
const reducerAdmissionBackoffCap = 10 * time.Second

// admissionDepthHolder is the process-wide queue-depth read of the reducer
// admission gate (#11233). Every admission decision takes each depth signal
// from here: a held value no older than the current window, or the result of
// the one read in flight. Concurrent callers share one read in flight; the
// one overlap is a read whose last waiter left: it is cancelled and
// forgotten, and its statement can still run on the server (bounded by the
// Postgres cancel latency) while the next caller starts a fresh one.
//
// The window is the max age A (one poll interval) while the gate admits. While
// it defers, the window is the backoff, which grows per deferred read from the
// poll interval to the cap and resets on the first decision whose global
// gates pass (an admit, or a per-source source_behind deferral). Deferring on an
// older value only delays a producer. Admitting uses a value at most A old in
// the common case, because the window is A whenever the last decision
// admitted. Two corners hold a below-mark value while the window is still a
// backoff: a read that completes as its last waiter leaves, and a deferred
// call from a stale verdict that advances the backoff just after an admit
// reset it. There an admit may use a value up to the current backoff old, at
// most the backoff cap.
//
// Lock order: holder.mu and each read's mu are never held together, and no
// lock is held across a database read.
type admissionDepthHolder struct {
	now          func() time.Time
	maxAge       time.Duration
	pollInterval time.Duration
	backoffCap   time.Duration
	instruments  *telemetry.Instruments

	// queueDepth holds the reducer total of QueueDepths (the high-water
	// signal); graphWriteTimeout holds ReducerGraphWriteTimeoutDepth (the
	// graph-write-pressure signal). Both share one read sequence.
	queueDepth        *admissionSharedRead[int64]
	graphWriteTimeout *admissionSharedRead[int64]

	mu sync.Mutex
	// backoff is the current read interval while deferring; zero means the
	// gate is admitting and the window is maxAge.
	backoff time.Duration
	// advancedSeq is the newest read sequence that advanced the backoff, so
	// K waiters deferring on one read advance it once, not K times.
	advancedSeq uint64
}

// newAdmissionDepthHolder builds the shared holder over reader. The max age
// is the configured poll interval. now is the gate clock (time.Now in
// production).
func newAdmissionDepthHolder(
	reader reducerAdmissionDepthReader,
	config reducerAdmissionConfig,
	now func() time.Time,
	instruments *telemetry.Instruments,
) *admissionDepthHolder {
	if now == nil {
		now = time.Now
	}
	seq := &atomic.Uint64{}
	h := &admissionDepthHolder{
		now:          now,
		maxAge:       config.PollInterval,
		pollInterval: config.PollInterval,
		backoffCap:   admissionBackoffCap(config),
		instruments:  instruments,
		queueDepth: &admissionSharedRead[int64]{
			seq: seq,
			read: func(ctx context.Context) (int64, error) {
				depths, err := reader.QueueDepths(ctx)
				if err != nil {
					return 0, fmt.Errorf("read queue depths: %w", err)
				}
				var total int64
				for _, count := range depths["reducer"] {
					total += count
				}
				return total, nil
			},
		},
		graphWriteTimeout: &admissionSharedRead[int64]{
			seq:  seq,
			read: reader.ReducerGraphWriteTimeoutDepth,
		},
	}
	h.recordBackoff(context.Background(), 0)
	return h
}

// admissionBackoffCap returns the backoff ceiling for config: the default cap,
// lowered to half the max wait when that is smaller, and never below the poll
// interval.
func admissionBackoffCap(config reducerAdmissionConfig) time.Duration {
	ceiling := reducerAdmissionBackoffCap
	if config.MaxWait > 0 && ceiling > config.MaxWait/2 {
		ceiling = config.MaxWait / 2
	}
	if ceiling < config.PollInterval {
		ceiling = config.PollInterval
	}
	return ceiling
}

// window returns how old a held value may be for the next decision.
func (h *admissionDepthHolder) window() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.backoff > 0 {
		return h.backoff
	}
	return h.maxAge
}

// read takes one signal through the shared read and records its outcome.
func (h *admissionDepthHolder) read(ctx context.Context, signal string, shared *admissionSharedRead[int64]) (admissionHeldValue[int64], error) {
	value, outcome, err := shared.get(ctx, h.now(), h.window())
	if h.instruments != nil && h.instruments.ReducerAdmissionDepthReads != nil {
		h.instruments.ReducerAdmissionDepthReads.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrOperation(signal), telemetry.AttrOutcome(outcome)))
	}
	return value, err
}

// recordAge records the age of the oldest depth a decision used.
func (h *admissionDepthHolder) recordAge(ctx context.Context, readAt time.Time) {
	if h.instruments == nil || h.instruments.ReducerAdmissionDepthAge == nil {
		return
	}
	age := h.now().Sub(readAt)
	if age < 0 {
		age = 0
	}
	h.instruments.ReducerAdmissionDepthAge.Record(ctx, age.Seconds())
}

// deferred advances the backoff once per new read that deferred and returns
// how long the waiter should sleep: until the value it deferred on is
// strictly past the window, so the next decision reads again. The window is
// inclusive (a value exactly window old is still held), so the wake lands one
// nanosecond past it; waking on the boundary would take the held value, defer
// again and lose one poll interval per step, which breaks the admit bound.
// A value already past that instant (a read slower than the window) sleeps
// one poll interval, so a slow read never turns into back-to-back reads and
// the sleep is never zero.
func (h *admissionDepthHolder) deferred(ctx context.Context, seq uint64, readAt time.Time) time.Duration {
	h.mu.Lock()
	changed := false
	if seq > h.advancedSeq {
		h.advancedSeq = seq
		if h.backoff == 0 {
			h.backoff = h.pollInterval
		} else {
			h.backoff = min(2*h.backoff, h.backoffCap)
		}
		changed = true
	}
	window := h.backoff
	if window == 0 {
		window = h.maxAge
	}
	h.mu.Unlock()
	if changed {
		h.recordBackoff(ctx, window)
	}
	sleep := readAt.Add(window + time.Nanosecond).Sub(h.now())
	if sleep <= 0 {
		sleep = h.pollInterval
	}
	return sleep
}

// admitted resets the backoff on the first decision after a deferral whose
// global gates pass: an admit, or a source_behind deferral on the per-source
// leg, which says the global depth no longer defers.
func (h *admissionDepthHolder) admitted(ctx context.Context) {
	h.mu.Lock()
	changed := h.backoff != 0
	h.backoff = 0
	h.mu.Unlock()
	if changed {
		h.recordBackoff(ctx, 0)
	}
}

func (h *admissionDepthHolder) recordBackoff(ctx context.Context, backoff time.Duration) {
	if h.instruments == nil || h.instruments.ReducerAdmissionReadBackoff == nil {
		return
	}
	h.instruments.ReducerAdmissionReadBackoff.Record(ctx, backoff.Seconds())
}

// admissionHeldValue is one successful read: the value, the instant the read
// started (a Postgres statement sees the snapshot taken at its start, so age
// counts from there), and its process-wide read sequence (zero means none).
type admissionHeldValue[T any] struct {
	value  T
	readAt time.Time
	seq    uint64
}

// admissionSharedRead is a single-flight, bounded-age read of one gate
// signal. A held value is served while it is inside the caller's window;
// otherwise the first caller starts one read and later callers join it. The
// read runs on its own goroutine with a context that keeps the starter's
// values but not its cancellation, so one caller leaving never fails the
// others. When the last waiter leaves, the read is cancelled and forgotten so
// the next caller starts fresh. A failed read is returned to its waiters and
// never held.
type admissionSharedRead[T any] struct {
	read func(context.Context) (T, error)
	seq  *atomic.Uint64

	mu   sync.Mutex
	held admissionHeldValue[T]
	call *admissionReadCall[T]
}

type admissionReadCall[T any] struct {
	done    chan struct{}
	result  admissionHeldValue[T]
	err     error
	waiters int
	cancel  context.CancelFunc
}

// get returns a value no older than maxAge at now, or the result of the read
// in flight, and the closed outcome of this call.
func (s *admissionSharedRead[T]) get(ctx context.Context, now time.Time, maxAge time.Duration) (admissionHeldValue[T], string, error) {
	s.mu.Lock()
	if s.held.seq != 0 && now.Sub(s.held.readAt) <= maxAge {
		held := s.held
		s.mu.Unlock()
		return held, telemetry.ReducerAdmissionDepthReadHeld, nil
	}
	outcome := telemetry.ReducerAdmissionDepthReadShared
	call := s.call
	if call == nil {
		outcome = telemetry.ReducerAdmissionDepthReadRead
		call = s.startLocked(ctx, now)
	}
	call.waiters++
	s.mu.Unlock()

	select {
	case <-call.done:
		if call.err != nil {
			return admissionHeldValue[T]{}, telemetry.ReducerAdmissionDepthReadError, call.err
		}
		return call.result, outcome, nil
	case <-ctx.Done():
		s.leave(call)
		return admissionHeldValue[T]{}, telemetry.ReducerAdmissionDepthReadCancelled, fmt.Errorf("wait for shared depth read: %w", ctx.Err())
	}
}

// startLocked starts one read. The caller holds s.mu.
func (s *admissionSharedRead[T]) startLocked(ctx context.Context, startedAt time.Time) *admissionReadCall[T] {
	readCtx, cancel := context.WithCancel(context.WithoutCancel(ctx)) // #nosec G118 -- cancel is stored on the call; run() defers it and leave() calls it when the last waiter goes
	call := &admissionReadCall[T]{done: make(chan struct{}), cancel: cancel}
	s.call = call
	go s.run(readCtx, call, startedAt)
	return call
}

func (s *admissionSharedRead[T]) run(ctx context.Context, call *admissionReadCall[T], startedAt time.Time) {
	defer call.cancel()
	value, err := s.read(ctx)
	s.mu.Lock()
	call.err = err
	if err == nil {
		call.result = admissionHeldValue[T]{value: value, readAt: startedAt, seq: s.seq.Add(1)}
		// A read that a newer one overtook (its waiters left, a fresh read
		// started) must not replace the newer value.
		if s.held.seq == 0 || !startedAt.Before(s.held.readAt) {
			s.held = call.result
		}
	}
	if s.call == call {
		s.call = nil
	}
	s.mu.Unlock()
	close(call.done)
}

// leave drops one waiter; the last waiter out cancels and forgets the read.
func (s *admissionSharedRead[T]) leave(call *admissionReadCall[T]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call.waiters--
	if call.waiters == 0 && s.call == call {
		s.call = nil
		call.cancel()
	}
}

// waiting reports how many callers wait on the read in flight.
func (s *admissionSharedRead[T]) waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.call == nil {
		return 0
	}
	return s.call.waiters
}
