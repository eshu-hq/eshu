// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The tests in this file reproduce issue #10866: a reducer handler that runs
// past its configured per-domain bound holds its worker forever — nothing
// enforces Service.HandlerTimeout/HandlerTimeoutOverrides yet. The trigger
// is the bound expiring while the handler is still running (no shutdown): the
// worker must be freed and the intent must fail through the existing
// WorkSink.Fail triage with the counting timeout class, so the item retries
// under the normal attempt budget and dead-letters when the budget is spent.
//
// Executor variants reused from service_handler_abandon_test.go:
//   - ignoringReducerExecutor ignores its context and never returns: the
//     stuck-handler shape. On unpatched code the timeout run hangs: Fail is
//     never called without shutdown. This variant must fail before the fix
//     and pass after.
//   - ctxDoneReducerExecutor blocks until its context ends and returns
//     ctx.Err(): the cooperative-handler shape. The derived bound cancels the
//     execution context, so it returns context.DeadlineExceeded from our own
//     timer and must be relabeled to the timeout class (not dropped, not
//     double-failed).
//
// Both the per-item path (executeWithTelemetry) and the batch path
// (executeAndReport) are covered.

// wantTimeoutFailureClass is the classified failure class the timeout path
// must report. It is spelled out here so the test defines the contract
// before the implementation names it.
const wantTimeoutFailureClass = "reducer_handler_timeout"

// deadlineExceededReducerExecutor returns context.DeadlineExceeded
// immediately without blocking: the downstream-timeout shape. A generous
// bound means our own timer never fires, so the error must stay a plain
// handler failure and must NOT be relabeled to the timeout class.
type deadlineExceededReducerExecutor struct{}

func (e *deadlineExceededReducerExecutor) Execute(context.Context, reducercontract.Intent) (reducercontract.Result, error) {
	return reducercontract.Result{}, context.DeadlineExceeded
}

// fastReducerExecutor returns success immediately: the under-bound shape.
type fastReducerExecutor struct{}

func (e *fastReducerExecutor) Execute(context.Context, reducercontract.Intent) (reducercontract.Result, error) {
	return reducercontract.Result{}, nil
}

func callExecuteHandlerWithWatchdog(t *testing.T, s Service, execCtx, shutdownCtx context.Context, intent reducercontract.Intent) (reducercontract.Result, error, bool) {
	t.Helper()
	type outcome struct {
		result    reducercontract.Result
		err       error
		abandoned bool
	}
	done := make(chan outcome, 1)
	go func() {
		result, err, abandoned := s.executeHandler(execCtx, shutdownCtx, intent)
		done <- outcome{result: result, err: err, abandoned: abandoned}
	}()
	select {
	case out := <-done:
		return out.result, out.err, out.abandoned
	case <-time.After(10 * time.Second):
		t.Fatal("executeHandler did not return within 10s of the bound expiring: a handler that ignores its bound holds its worker forever (RED on unpatched code)")
		return reducercontract.Result{}, nil, false
	}
}

func assertTimeoutFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want the handler-timeout error")
	}
	if !IsRetryable(err) {
		t.Fatalf("IsRetryable(%v) = false, want true: a timed-out handler must retry under the attempt budget, never dead-letter on the spot", err)
	}
	if got := reducerExecutionFailureClass(err); got != wantTimeoutFailureClass {
		t.Fatalf("failure class = %q, want %q", got, wantTimeoutFailureClass)
	}
}

// TestExecuteHandlerTimesOutIgnoringHandler is the #10866 unit regression:
// the handler ignores its context and never returns while no shutdown
// arrives. The bound expiring must free the worker with the timeout class.
func TestExecuteHandlerTimesOutIgnoringHandler(t *testing.T) {
	t.Parallel()

	service := Service{
		Executor:       &ignoringReducerExecutor{},
		HandlerTimeout: 50 * time.Millisecond,
	}
	_, err, abandoned := callExecuteHandlerWithWatchdog(t, service, context.Background(), context.Background(), abandonTestIntent("intent-timeout-unit"))
	if !abandoned {
		t.Fatal("abandoned = false, want true: the bound expired with the handler still running")
	}
	assertTimeoutFailure(t, err)
}

// TestExecuteHandlerCooperativeTimeoutCarriesTimeoutClass pins the
// cooperative path: the derived bound cancels the execution context, so a
// handler that observes it returns context.DeadlineExceeded from OUR timer.
// Whichever side of the exact-boundary race wins — the handler's return or
// the deadline branch — the outcome must carry the timeout class (not a bare
// context error), and the worker must be free (the call returned). The race
// itself is deliberately nondeterministic: the select prefers an
// already-buffered outcome, so scheduling decides which release shape runs.
func TestExecuteHandlerCooperativeTimeoutCarriesTimeoutClass(t *testing.T) {
	t.Parallel()

	service := Service{
		Executor:       &ctxDoneReducerExecutor{},
		HandlerTimeout: 50 * time.Millisecond,
	}
	_, err, _ := callExecuteHandlerWithWatchdog(t, service, context.Background(), context.Background(), abandonTestIntent("intent-timeout-cooperative"))
	assertTimeoutFailure(t, err)
}

// slowCooperativeReducerExecutor observes cancellation like a real paged
// handler but cannot return before the bound releases it: it sleeps past the
// bound after its context ends, forcing the deadline branch deterministically.
// This proves a cooperating handler takes the abandon release (no double
// Fail, no drop) when it cannot return in time.
type slowCooperativeReducerExecutor struct {
	pastBound time.Duration
}

func (e *slowCooperativeReducerExecutor) Execute(ctx context.Context, _ reducercontract.Intent) (reducercontract.Result, error) {
	<-ctx.Done()
	time.Sleep(e.pastBound)
	return reducercontract.Result{}, ctx.Err()
}

// TestExecuteHandlerSlowCooperativeTakesAbandonRelease pins the deterministic
// abandon side of the cooperative race: the handler observes the bound but
// returns too late, so the deadline branch frees the worker with the timeout
// class and the late return is dropped.
func TestExecuteHandlerSlowCooperativeTakesAbandonRelease(t *testing.T) {
	t.Parallel()

	service := Service{
		Executor:       &slowCooperativeReducerExecutor{pastBound: 200 * time.Millisecond},
		HandlerTimeout: 50 * time.Millisecond,
	}
	_, err, abandoned := callExecuteHandlerWithWatchdog(t, service, context.Background(), context.Background(), abandonTestIntent("intent-timeout-slow-cooperative"))
	if !abandoned {
		t.Fatal("abandoned = false, want true: the handler returned after the bound released it")
	}
	assertTimeoutFailure(t, err)
}

// TestExecuteHandlerDoesNotRelabelDownstreamDeadline pins the relabel guard:
// a handler that fails fast with context.DeadlineExceeded while the bound is
// generous (our own timer never fired) reports a plain handler failure, never
// the timeout class. Relabeling it would falsely trigger timeout backpressure.
func TestExecuteHandlerDoesNotRelabelDownstreamDeadline(t *testing.T) {
	t.Parallel()

	service := Service{
		Executor:       &deadlineExceededReducerExecutor{},
		HandlerTimeout: time.Hour,
	}
	_, err, abandoned := callExecuteHandlerWithWatchdog(t, service, context.Background(), context.Background(), abandonTestIntent("intent-timeout-downstream"))
	if abandoned {
		t.Fatal("abandoned = true, want false: the handler returned before the bound")
	}
	if err == nil {
		t.Fatal("error = nil, want the handler's DeadlineExceeded")
	}
	if got := reducerExecutionFailureClass(err); got == wantTimeoutFailureClass {
		t.Fatalf("failure class = %q, want anything but the timeout class: our own timer never fired", got)
	}
}

// TestExecuteHandlerSuccessUnderBound pins the green path: a handler that
// returns before the bound succeeds with no timeout class and no
// abandonment.
func TestExecuteHandlerSuccessUnderBound(t *testing.T) {
	t.Parallel()

	service := Service{
		Executor:       &fastReducerExecutor{},
		HandlerTimeout: time.Hour,
	}
	_, err, abandoned := callExecuteHandlerWithWatchdog(t, service, context.Background(), context.Background(), abandonTestIntent("intent-timeout-under-bound"))
	if abandoned {
		t.Fatal("abandoned = true, want false: the handler returned before the bound")
	}
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}

// TestExecuteHandlerZeroBoundPreservesShutdownAbandon pins the bound=0
// floor: with no bound configured the behavior is byte-identical to today —
// only shutdown releases a stuck handler, with the shutdown class.
func TestExecuteHandlerZeroBoundPreservesShutdownAbandon(t *testing.T) {
	t.Parallel()

	service := Service{Executor: &ignoringReducerExecutor{}}
	shutdown, cancel := context.WithCancel(context.Background())
	cancel()
	_, err, abandoned := callExecuteHandlerWithWatchdog(t, service, context.Background(), shutdown, abandonTestIntent("intent-timeout-zero-bound"))
	if !abandoned {
		t.Fatal("abandoned = false, want true: shutdown was already done")
	}
	// The shutdown class itself is assigned by the abandonment tail;
	// executeHandler reports the bare shutdown cause. Bound=0 keeping
	// today's behavior means the cause is shutdown, never the timeout class.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the shutdown cause: bound=0 must keep today's shutdown release", err)
	}
	if _, isTimeout := err.(*handlerTimeoutError); isTimeout {
		t.Fatalf("error = %v, want no timeout class: no bound is configured", err)
	}
}

// TestExecuteHandlerShutdownWinsOverTimeout pins the race rule: when shutdown
// is already done, the shutdown class wins even with a bound configured.
func TestExecuteHandlerShutdownWinsOverTimeout(t *testing.T) {
	t.Parallel()

	service := Service{
		Executor:       &ignoringReducerExecutor{},
		HandlerTimeout: 50 * time.Millisecond,
	}
	shutdown, cancel := context.WithCancel(context.Background())
	cancel()
	_, err, abandoned := callExecuteHandlerWithWatchdog(t, service, context.Background(), shutdown, abandonTestIntent("intent-timeout-shutdown-race"))
	if !abandoned {
		t.Fatal("abandoned = false, want true: shutdown was already done")
	}
	// The shutdown class itself is assigned by the abandonment tail (which
	// wraps the cause in handlerAbandonedError); executeHandler reports the
	// bare shutdown cause. Shutdown winning means the cause is the shutdown
	// context, never the bound expiry.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the shutdown cause: shutdown races the bound, shutdown wins", err)
	}
	if _, isTimeout := err.(*handlerTimeoutError); isTimeout {
		t.Fatalf("error = %v, want no timeout class: shutdown races the bound, shutdown wins", err)
	}
}

// TestHandlerTimeoutOverrideZeroExemptsDomain pins the per-domain escape
// hatch: a direct Service override of 0 exempts that domain from the default
// bound while the default still applies elsewhere.
func TestHandlerTimeoutOverrideZeroExemptsDomain(t *testing.T) {
	t.Parallel()

	service := Service{
		HandlerTimeout: 5 * time.Minute,
		HandlerTimeoutOverrides: map[reducercontract.Domain]time.Duration{
			DomainSemanticEntityMaterialization: 0,
		},
	}
	if got := service.HandlerTimeoutFor(DomainSemanticEntityMaterialization); got != 0 {
		t.Fatalf("HandlerTimeoutFor(exempt domain) = %v, want 0: an explicit 0 override disables the default bound", got)
	}
	if got := service.HandlerTimeoutFor(DomainWorkloadIdentity); got != 5*time.Minute {
		t.Fatalf("HandlerTimeoutFor(other domain) = %v, want 5m default", got)
	}
}

// TestTimeoutFailureClassIsCountingRetryable pins the repeat-timeout policy
// (criterion 4): the timeout class opts into bounded retry (never an
// on-the-spot dead-letter), is not a readiness wait, and self-classifies so
// failIntent preserves it on the retry row.
func TestTimeoutFailureClassIsCountingRetryable(t *testing.T) {
	t.Parallel()

	timeoutErr := &handlerTimeoutError{
		intentID: "intent-timeout-policy",
		domain:   DomainSemanticEntityMaterialization,
		elapsed:  time.Minute,
		bound:    50 * time.Millisecond,
		cause:    context.DeadlineExceeded,
	}
	if !IsRetryable(timeoutErr) {
		t.Fatalf("IsRetryable(%v) = false, want true", timeoutErr)
	}
	if got := reducerExecutionFailureClass(timeoutErr); got != wantTimeoutFailureClass {
		t.Fatalf("failure class = %q, want %q", got, wantTimeoutFailureClass)
	}
	if reducercontract.IsExpectedWait(timeoutErr) {
		t.Fatalf("IsExpectedWait(%v) = true, want false: a timeout is a defect to retry, never a deferred readiness wait", timeoutErr)
	}
	if !errors.Is(timeoutErr, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(%v, DeadlineExceeded) = false, want true: the timeout wraps the bound expiry", timeoutErr)
	}
}

// timeoutTestInstruments builds meter-backed instruments plus a reader so
// timeout tests can assert the operator signals by metric name.
func timeoutTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("reducer-timeout"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return instruments, reader
}

func collectTimeoutMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return rm
}
