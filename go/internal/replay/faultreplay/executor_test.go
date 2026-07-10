// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// countingInner is a stub reducer.Executor that records how many times it was
// called and always succeeds, so executor.go's tests can assert exactly when
// (and how many times) the inner executor is actually invoked.
type countingInner struct {
	calls atomic.Int64
}

func (c *countingInner) Execute(_ context.Context, intent reducer.Intent) (reducer.Result, error) {
	c.calls.Add(1)
	return reducer.Result{IntentID: intent.IntentID, Status: reducer.ResultStatusSucceeded}, nil
}

// stubRedeliverer is a test double for the redeliverer interface. armCh, if
// set, is returned by ArmMidHandlerDuplicate; redeliverOnceCalls counts
// RedeliverOnce invocations.
type stubRedeliverer struct {
	armCh              chan struct{}
	armCalls           atomic.Int64
	redeliverOnceCalls atomic.Int64
	lastRedelivered    atomic.Value // string
}

func (s *stubRedeliverer) ArmMidHandlerDuplicate(reducer.Intent) <-chan struct{} {
	s.armCalls.Add(1)
	return s.armCh
}

func (s *stubRedeliverer) RedeliverOnce(intent reducer.Intent) {
	s.redeliverOnceCalls.Add(1)
	s.lastRedelivered.Store(intent.IntentID)
}

func statementOrdinalScript(ordinal int, lane string) faultreplay.Script {
	n := ordinal
	return faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindFailGraphWriteOnceThenSucceed,
			Trigger: faultreplay.Trigger{StatementOrdinal: &n},
			Target:  faultreplay.Target{Lane: lane},
		}},
	}
}

// TestFaultingExecutorFailTerminalNeverCallsInner proves a fail-terminal
// target always fails, on every delivery, without ever reaching inner.
func TestFaultingExecutorFailTerminalNeverCallsInner(t *testing.T) {
	t.Parallel()

	id := "dead-letter-me"
	inner := &countingInner{}
	redeliv := &stubRedeliverer{}
	items := []schedulereplay.WorkItem{{IntentID: id}}
	exec, err := faultreplay.NewFaultingExecutor(inner, faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindFailTerminal,
			Trigger: faultreplay.Trigger{IntentID: &id},
		}},
	}, items, redeliv)
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}

	for i := 0; i < 3; i++ {
		_, err := exec.Execute(context.Background(), reducer.Intent{IntentID: id})
		if err == nil {
			t.Fatalf("attempt %d: expected fail-terminal error, got nil", i)
		}
		if reducer.IsRetryable(err) {
			t.Fatalf("attempt %d: fail-terminal error must not be retryable, got %v", i, err)
		}
	}
	if inner.calls.Load() != 0 {
		t.Fatalf("inner.calls = %d, want 0 (fail-terminal must never delegate)", inner.calls.Load())
	}
}

// TestFaultingExecutorOnceThenSucceedExecutorRetryNeverErrors proves the
// executor-retry lane absorbs its one injected failure entirely inside
// Execute: the caller never observes an error, and inner is still called
// exactly once for the targeted call.
func TestFaultingExecutorOnceThenSucceedExecutorRetryNeverErrors(t *testing.T) {
	t.Parallel()

	inner := &countingInner{}
	redeliv := &stubRedeliverer{}
	items := []schedulereplay.WorkItem{{IntentID: "x"}}
	exec, err := faultreplay.NewFaultingExecutor(inner, statementOrdinalScript(1, faultreplay.LaneExecutorRetry), items, redeliv)
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}

	result, err := exec.Execute(context.Background(), reducer.Intent{IntentID: "x"})
	if err != nil {
		t.Fatalf("Execute: unexpected error %v (executor-retry must never surface to the caller)", err)
	}
	if result.Status != reducer.ResultStatusSucceeded {
		t.Fatalf("result.Status = %v, want succeeded", result.Status)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("inner.calls = %d, want 1", inner.calls.Load())
	}
	if !exec.ExecutorRetryFired() {
		t.Fatal("ExecutorRetryFired() = false, want true (fault must fire, not silently no-op)")
	}
	if redeliv.redeliverOnceCalls.Load() != 0 {
		t.Fatalf("RedeliverOnce called %d times, want 0 (executor-retry lane must not touch the queue)", redeliv.redeliverOnceCalls.Load())
	}

	// The second call (a different statement ordinal) must not re-fire.
	if _, err := exec.Execute(context.Background(), reducer.Intent{IntentID: "x"}); err != nil {
		t.Fatalf("second Execute: unexpected error %v", err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("inner.calls after second Execute = %d, want 2", inner.calls.Load())
	}
}

// TestFaultingExecutorOnceThenSucceedQueueRetryFailsThenRedelivers proves the
// queue-retry lane returns a plain (non-retryable) error on the targeted call,
// arms exactly one redelivery via RedeliverOnce, and does not call inner for
// that failed call.
func TestFaultingExecutorOnceThenSucceedQueueRetryFailsThenRedelivers(t *testing.T) {
	t.Parallel()

	inner := &countingInner{}
	redeliv := &stubRedeliverer{}
	items := []schedulereplay.WorkItem{{IntentID: "y"}}
	exec, err := faultreplay.NewFaultingExecutor(inner, statementOrdinalScript(1, faultreplay.LaneQueueRetry), items, redeliv)
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}

	_, err = exec.Execute(context.Background(), reducer.Intent{IntentID: "y"})
	if err == nil {
		t.Fatal("expected a queue-retry error on the targeted call, got nil")
	}
	if reducer.IsRetryable(err) {
		t.Fatalf("queue-retry error must not be RetryableError-marked, got %v", err)
	}
	if inner.calls.Load() != 0 {
		t.Fatalf("inner.calls = %d, want 0 (the failed call must not delegate)", inner.calls.Load())
	}
	if redeliv.redeliverOnceCalls.Load() != 1 {
		t.Fatalf("RedeliverOnce called %d times, want 1", redeliv.redeliverOnceCalls.Load())
	}
	if got := redeliv.lastRedelivered.Load(); got != "y" {
		t.Fatalf("RedeliverOnce got intent %v, want %q", got, "y")
	}

	// The redelivered (second) attempt must succeed and delegate to inner.
	result, err := exec.Execute(context.Background(), reducer.Intent{IntentID: "y"})
	if err != nil {
		t.Fatalf("redelivered Execute: unexpected error %v", err)
	}
	if result.Status != reducer.ResultStatusSucceeded {
		t.Fatalf("redelivered result.Status = %v, want succeeded", result.Status)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("inner.calls after redelivery = %d, want 1", inner.calls.Load())
	}
}

// TestFaultingExecutorMidHandlerBlocksUntilReleased proves Execute for the
// targeted intent blocks on the redeliverer's rendezvous channel before
// delegating to inner, and proceeds once the channel closes.
func TestFaultingExecutorMidHandlerBlocksUntilReleased(t *testing.T) {
	t.Parallel()

	inner := &countingInner{}
	armCh := make(chan struct{})
	redeliv := &stubRedeliverer{armCh: armCh}
	id := "parked"
	items := []schedulereplay.WorkItem{{IntentID: id}}
	exec, err := faultreplay.NewFaultingExecutor(inner, faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindExpireLeaseMidHandler,
			Trigger: faultreplay.Trigger{IntentID: &id},
		}},
	}, items, redeliv)
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := exec.Execute(context.Background(), reducer.Intent{IntentID: id}); err != nil {
			t.Errorf("Execute: unexpected error %v", err)
		}
	}()

	select {
	case <-done:
		t.Fatal("Execute returned before the rendezvous was released")
	case <-time.After(50 * time.Millisecond):
	}
	if inner.calls.Load() != 0 {
		t.Fatalf("inner.calls while parked = %d, want 0", inner.calls.Load())
	}
	if redeliv.armCalls.Load() != 1 {
		t.Fatalf("ArmMidHandlerDuplicate called %d times, want 1", redeliv.armCalls.Load())
	}

	close(armCh)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Execute did not return after the rendezvous released")
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("inner.calls after release = %d, want 1", inner.calls.Load())
	}
}

// TestFaultingExecutorMidHandlerRespectsContextCancellation proves the
// rendezvous wait is not an unconditional block: a canceled context unparks
// Execute with an error instead of hanging forever.
func TestFaultingExecutorMidHandlerRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	inner := &countingInner{}
	redeliv := &stubRedeliverer{armCh: make(chan struct{})} // never closed
	id := "stuck"
	items := []schedulereplay.WorkItem{{IntentID: id}}
	exec, err := faultreplay.NewFaultingExecutor(inner, faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindExpireLeaseMidHandler,
			Trigger: faultreplay.Trigger{IntentID: &id},
		}},
	}, items, redeliv)
	if err != nil {
		t.Fatalf("NewFaultingExecutor: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = exec.Execute(ctx, reducer.Intent{IntentID: id})
	if err == nil {
		t.Fatal("expected a context-cancellation error, got nil")
	}
	if inner.calls.Load() != 0 {
		t.Fatalf("inner.calls = %d, want 0", inner.calls.Load())
	}
}

// TestFaultingExecutorRejectsMultipleOnceThenSucceedFaults proves
// construction fails loudly for a script naming more than one
// fail-graph-write-once-then-succeed fault.
func TestFaultingExecutorRejectsMultipleOnceThenSucceedFaults(t *testing.T) {
	t.Parallel()

	items := []schedulereplay.WorkItem{{IntentID: "x"}}
	one, two := 1, 2
	_, err := faultreplay.NewFaultingExecutor(&countingInner{}, faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{
			{Kind: faultreplay.KindFailGraphWriteOnceThenSucceed, Trigger: faultreplay.Trigger{StatementOrdinal: &one}, Target: faultreplay.Target{Lane: faultreplay.LaneExecutorRetry}},
			{Kind: faultreplay.KindFailGraphWriteOnceThenSucceed, Trigger: faultreplay.Trigger{StatementOrdinal: &two}, Target: faultreplay.Target{Lane: faultreplay.LaneQueueRetry}},
		},
	}, items, &stubRedeliverer{})
	if err == nil {
		t.Fatal("expected an error for more than one fail-graph-write-once-then-succeed fault, got nil")
	}
}

// TestFaultingExecutorRejectsOutOfRangeIntentOrdinal proves an
// expire-lease-mid-handler fault whose intent_ordinal exceeds the scripted
// delivery order fails at construction rather than silently never firing.
func TestFaultingExecutorRejectsOutOfRangeIntentOrdinal(t *testing.T) {
	t.Parallel()

	items := []schedulereplay.WorkItem{{IntentID: "only-one"}}
	ordinal := 5
	_, err := faultreplay.NewFaultingExecutor(&countingInner{}, faultreplay.Script{
		Version: faultreplay.CurrentVersion,
		Faults: []faultreplay.FaultOp{{
			Kind:    faultreplay.KindExpireLeaseMidHandler,
			Trigger: faultreplay.Trigger{IntentOrdinal: &ordinal},
		}},
	}, items, &stubRedeliverer{})
	if err == nil {
		t.Fatal("expected an out-of-range error, got nil")
	}
}
