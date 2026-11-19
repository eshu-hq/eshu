// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestExecuteWithTelemetryTimesOutIgnoringHandler is the #10866 per-item
// integration regression: the full executeWithTelemetry path (heartbeat,
// execution, Fail triage, operator signals) with a stuck handler and an
// expired bound, and no shutdown. The worker must be freed, Fail must carry
// the timeout class exactly once, nothing must be acked, the heartbeat must
// stop before Fail, the abandonment counter must stay silent, and no handler
// duration may be recorded (the handler never returned).
func TestExecuteWithTelemetryTimesOutIgnoringHandler(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-timeout-single")
	state := &sharedAbandonState{}
	sink := &abandonSingleSink{state: state}
	instruments, reader := timeoutTestInstruments(t)

	service := Service{
		Executor:          &ignoringReducerExecutor{},
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Instruments:       instruments,
		HandlerTimeout:    50 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() { done <- service.executeWithTelemetry(context.Background(), intent, 0) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeWithTelemetry() error = %v, want nil: the timeout tail releases the item through Fail", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("executeWithTelemetry() did not return within 10s of the bound expiring (RED on unpatched code)")
	}

	acks, fails, failedWith := sink.counts()
	if fails != 1 {
		t.Fatalf("fail calls = %d, want 1: the expired bound must release the stuck item through Fail", fails)
	}
	if acks != 0 {
		t.Fatalf("ack calls = %d, want 0: a timed-out handler must never be acked", acks)
	}
	assertTimeoutFailure(t, failedWith)
	if got := state.postFail(); got != 0 {
		t.Fatalf("heartbeats after Fail started = %d, want 0: the heartbeat must stop before Fail", got)
	}

	rm := collectTimeoutMetrics(t, reader)
	want := map[string]string{"domain": string(intent.Domain)}
	if got := counterSum(rm, "eshu_dp_reducer_handler_timed_out_total", want); got != 1 {
		t.Fatalf("handler timed-out counter = %d, want 1 with domain %q", got, intent.Domain)
	}
	if got := counterSum(rm, "eshu_dp_reducer_handler_abandoned_total", want); got != 0 {
		t.Fatalf("handler abandoned counter = %d, want 0: a timeout is not a shutdown abandonment", got)
	}
	if got := histogramCount(rm, "eshu_dp_reducer_handler_duration_seconds", want); got != 0 {
		t.Fatalf("handler duration histogram points = %d, want 0: the handler never returned", got)
	}
}

// TestExecuteWithTelemetrySlowCooperativeTimeoutAbandons pins the
// cooperative per-item path when the handler cannot return in time: it
// observes the bound (so no shutdown is involved) but sleeps past it, so the
// deadline branch frees the worker deterministically. Fail carries the
// counting timeout class exactly once, nothing is acked, the heartbeat stops
// before Fail, the timeout counter fires, and no duration is recorded (the
// return lands after the release and is dropped).
func TestExecuteWithTelemetrySlowCooperativeTimeoutAbandons(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-timeout-single-cooperative")
	state := &sharedAbandonState{}
	sink := &abandonSingleSink{state: state}
	instruments, reader := timeoutTestInstruments(t)

	service := Service{
		Executor:          &slowCooperativeReducerExecutor{pastBound: 200 * time.Millisecond},
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Instruments:       instruments,
		HandlerTimeout:    50 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() { done <- service.executeWithTelemetry(context.Background(), intent, 0) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeWithTelemetry() error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("executeWithTelemetry() did not return within 10s of the bound expiring (RED on unpatched code)")
	}

	acks, fails, failedWith := sink.counts()
	if fails != 1 {
		t.Fatalf("fail calls = %d, want 1", fails)
	}
	if acks != 0 {
		t.Fatalf("ack calls = %d, want 0", acks)
	}
	assertTimeoutFailure(t, failedWith)

	rm := collectTimeoutMetrics(t, reader)
	want := map[string]string{"domain": string(intent.Domain)}
	if got := counterSum(rm, "eshu_dp_reducer_handler_timed_out_total", want); got != 1 {
		t.Fatalf("handler timed-out counter = %d, want 1 with domain %q", got, intent.Domain)
	}
	if got := histogramCount(rm, "eshu_dp_reducer_handler_duration_seconds", want); got != 0 {
		t.Fatalf("handler duration histogram points = %d, want 0: the late return lands after the release", got)
	}
}

// TestExecuteAndReportTimesOutIgnoringHandler is the #10866 batch-path
// integration regression: same stuck-handler contract through
// executeAndReport — no ack owed, Fail carries the timeout class, heartbeat
// stops before Fail, timeout counter fires, abandonment counter stays
// silent.
func TestExecuteAndReportTimesOutIgnoringHandler(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-timeout-batch")
	state := &sharedAbandonState{}
	sink := &abandonBatchSink{state: state}
	instruments, reader := timeoutTestInstruments(t)

	service := Service{
		Executor:          &ignoringReducerExecutor{},
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Instruments:       instruments,
		HandlerTimeout:    50 * time.Millisecond,
	}

	type outcome struct {
		item     batchAckItem
		needsAck bool
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		item, needsAck, err := service.executeAndReport(context.Background(), intent, 0)
		done <- outcome{item: item, needsAck: needsAck, err: err}
	}()
	var out outcome
	select {
	case out = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("executeAndReport() did not return within 10s of the bound expiring (RED on unpatched code)")
	}
	if out.err != nil {
		t.Fatalf("executeAndReport() error = %v, want nil: the timeout tail releases the item through Fail", out.err)
	}
	if out.needsAck {
		t.Fatal("needsAck = true, want false: Fail already terminalized the row")
	}

	sink.fakeBatchWorkSink.mu.Lock()
	failed, acked := sink.failed, sink.acked
	sink.fakeBatchWorkSink.mu.Unlock()
	if failed != 1 {
		t.Fatalf("fail calls = %d, want 1", failed)
	}
	if acked != 0 {
		t.Fatalf("ack calls = %d, want 0: a timed-out handler must never be acked", acked)
	}
	assertTimeoutFailure(t, sink.failure())
	if got := state.postFail(); got != 0 {
		t.Fatalf("heartbeats after Fail started = %d, want 0: the heartbeat must stop before Fail", got)
	}

	rm := collectTimeoutMetrics(t, reader)
	want := map[string]string{"domain": string(intent.Domain)}
	if got := counterSum(rm, "eshu_dp_reducer_handler_timed_out_total", want); got != 1 {
		t.Fatalf("handler timed-out counter = %d, want 1 with domain %q", got, intent.Domain)
	}
	if got := counterSum(rm, "eshu_dp_reducer_handler_abandoned_total", want); got != 0 {
		t.Fatalf("handler abandoned counter = %d, want 0: a timeout is not a shutdown abandonment", got)
	}
}

// TestExecuteAndReportSlowCooperativeTimeoutAbandons pins the cooperative
// batch path when the handler cannot return in time: same deterministic
// deadline-branch release through executeAndReport — no ack owed, counting
// Fail, heartbeat stopped before Fail, timeout counter fires, no duration.
func TestExecuteAndReportSlowCooperativeTimeoutAbandons(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-timeout-batch-cooperative")
	state := &sharedAbandonState{}
	sink := &abandonBatchSink{state: state}
	instruments, reader := timeoutTestInstruments(t)

	service := Service{
		Executor:          &slowCooperativeReducerExecutor{pastBound: 200 * time.Millisecond},
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Instruments:       instruments,
		HandlerTimeout:    50 * time.Millisecond,
	}

	type outcome struct {
		needsAck bool
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		_, needsAck, err := service.executeAndReport(context.Background(), intent, 0)
		done <- outcome{needsAck: needsAck, err: err}
	}()
	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("executeAndReport() error = %v, want nil", out.err)
		}
		if out.needsAck {
			t.Fatal("needsAck = true, want false: Fail already terminalized the row")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("executeAndReport() did not return within 10s of the bound expiring (RED on unpatched code)")
	}
	assertTimeoutFailure(t, sink.failure())

	rm := collectTimeoutMetrics(t, reader)
	want := map[string]string{"domain": string(intent.Domain)}
	if got := counterSum(rm, "eshu_dp_reducer_handler_timed_out_total", want); got != 1 {
		t.Fatalf("handler timed-out counter = %d, want 1 with domain %q", got, intent.Domain)
	}
	if got := histogramCount(rm, "eshu_dp_reducer_handler_duration_seconds", want); got != 0 {
		t.Fatalf("handler duration histogram points = %d, want 0: the late return lands after the release", got)
	}
}

// TestOwnDeadlineExpired pins the relabel guard deterministically: only a
// DeadlineExceeded observed after our own timer fired counts as our bound.
// A downstream timeout that beats the bound leaves the timer running and
// keeps its own class; any other error never relabels even after our timer.
func TestOwnDeadlineExpired(t *testing.T) {
	t.Parallel()

	deadlineOutcome := handlerOutcome{err: context.DeadlineExceeded}
	plainOutcome := handlerOutcome{err: errors.New("boom")}

	fired := time.NewTimer(0)
	select {
	case <-fired.C:
	case <-time.After(10 * time.Second):
		t.Fatal("test timer did not fire")
	}
	if !ownDeadlineExpired(deadlineOutcome, fired) {
		t.Fatal("ownDeadlineExpired(deadline, fired timer) = false, want true: our bound caused it")
	}
	if ownDeadlineExpired(plainOutcome, fired) {
		t.Fatal("ownDeadlineExpired(plain error, fired timer) = true, want false: only DeadlineExceeded relabels")
	}

	pending := time.NewTimer(time.Hour)
	defer pending.Stop()
	if ownDeadlineExpired(deadlineOutcome, pending) {
		t.Fatal("ownDeadlineExpired(deadline, pending timer) = true, want false: a downstream timeout that beats our bound keeps its own class")
	}
}

// TestReducerHandlerTimedOutMetricsEmitted proves the new operator signal
// exists and carries the domain label.
func TestReducerHandlerTimedOutMetricsEmitted(t *testing.T) {
	t.Parallel()

	instruments, reader := timeoutTestInstruments(t)
	service := Service{Instruments: instruments}
	intent := abandonTestIntent("intent-timeout-metrics")

	service.recordReducerHandlerTimedOut(context.Background(), intent)

	rm := collectTimeoutMetrics(t, reader)
	want := map[string]string{"domain": string(intent.Domain)}
	if got := counterSum(rm, "eshu_dp_reducer_handler_timed_out_total", want); got != 1 {
		t.Fatalf("handler timed-out counter = %d, want 1 with domain %q", got, intent.Domain)
	}
}
