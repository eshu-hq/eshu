// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The tests in this file reproduce issue #8211: a reducer handler that never
// returns holds its work item's lease via successful heartbeats, and the
// service never releases the item. The trigger is shutdown (the parent
// context ends) while the handler is still running: the worker must stop the
// lease heartbeat first and then fail the intent through the existing
// WorkSink.Fail triage (retry vs dead_letter via failIntent), so the item is
// retried instead of staying claimed. A late return from the abandoned handler
// must never produce a second terminal transition: the lease fence rejects
// it (#7142).
//
// Executor variants:
//   - ignoringReducerExecutor ignores its context entirely and never returns.
//     On unpatched code the run hangs: Fail is never called while heartbeats
//     keep succeeding. This variant must fail before the fix and pass after.
//   - ctxDoneReducerExecutor blocks until its context ends and returns
//     ctx.Err(). It guards the cooperative path: it passes before and after
//     the fix, proving the new select does not double-fail or drop the Fail
//     of a handler that observes shutdown itself.
//
// Both the per-item path (executeWithTelemetry) and the batch path
// (executeAndReport) are covered.

// wantAbandonedFailureClass is the classified failure class the abandonment
// path must report. It is spelled out here so the test defines the contract
// before the implementation names it.
const wantAbandonedFailureClass = "reducer_handler_abandoned_at_shutdown"

// sharedAbandonState observes heartbeat/fail interleaving across the
// heartbeater and the sink. Any heartbeat that lands after Fail started means
// the worker failed the intent while its own heartbeat loop was still
// renewing the lease (stop-after-Fail), which can re-extend a released claim.
type sharedAbandonState struct {
	mu            sync.Mutex
	heartbeats    int
	failStarted   bool
	postFailBeats int
}

func (s *sharedAbandonState) recordBeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeats++
	if s.failStarted {
		s.postFailBeats++
	}
}

func (s *sharedAbandonState) beats() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeats
}

func (s *sharedAbandonState) markFailStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failStarted = true
}

func (s *sharedAbandonState) postFail() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.postFailBeats
}

type abandonObservingHeartbeater struct {
	state *sharedAbandonState
}

func (h *abandonObservingHeartbeater) Heartbeat(context.Context, Intent) error {
	h.state.recordBeat()
	return nil
}

// abandonSingleSink records Fail calls for the per-item path. Fail sleeps so
// that a stop-after-Fail ordering lets at least one periodic tick land after
// Fail started, making the ordering assertion sensitive to the mutation.
type abandonSingleSink struct {
	mu         sync.Mutex
	ackCalls   int
	failCalls  int
	failedWith error
	state      *sharedAbandonState
}

func (s *abandonSingleSink) Ack(context.Context, Intent, Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls++
	return nil
}

func (s *abandonSingleSink) Fail(_ context.Context, _ Intent, err error) error {
	s.mu.Lock()
	s.failCalls++
	s.failedWith = err
	s.mu.Unlock()
	s.state.markFailStarted()
	time.Sleep(20 * time.Millisecond)
	return nil
}

func (s *abandonSingleSink) counts() (acks, fails int, failedWith error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ackCalls, s.failCalls, s.failedWith
}

// abandonBatchSink reuses the batch fakes for Ack/AckBatch and overrides Fail
// with the same sleep-and-flag behavior as abandonSingleSink.
type abandonBatchSink struct {
	fakeBatchWorkSink
	mu         sync.Mutex
	failedWith error
	state      *sharedAbandonState
}

func (s *abandonBatchSink) Fail(_ context.Context, intent Intent, err error) error {
	_ = s.fakeBatchWorkSink.Fail(context.Background(), intent, err)
	s.mu.Lock()
	s.failedWith = err
	s.mu.Unlock()
	s.state.markFailStarted()
	time.Sleep(20 * time.Millisecond)
	return nil
}

func (s *abandonBatchSink) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failedWith
}

// ignoringReducerExecutor ignores its context entirely and never returns: the
// stuck-handler shape from #8211.
type ignoringReducerExecutor struct {
	calls atomic.Int64
}

func (e *ignoringReducerExecutor) Execute(context.Context, Intent) (Result, error) {
	e.calls.Add(1)
	select {}
}

// ctxDoneReducerExecutor blocks until its context ends, then returns ctx.Err():
// the cooperative-handler shape. It proves the fix does not double-fail or
// drop the Fail when the handler observes shutdown itself.
type ctxDoneReducerExecutor struct {
	calls atomic.Int64
}

func (e *ctxDoneReducerExecutor) Execute(ctx context.Context, _ Intent) (Result, error) {
	e.calls.Add(1)
	<-ctx.Done()
	return Result{}, ctx.Err()
}

func abandonTestIntent(id string) Intent {
	return Intent{
		IntentID:     id,
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Cause:        "projector emitted semantic entity work",
		EntityKeys:   []string{"repo:eshu"},
		EnqueuedAt:   time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	}
}

func waitForBeats(t *testing.T, state *sharedAbandonState, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for state.beats() < want {
		if time.Now().After(deadline) {
			t.Fatalf("heartbeats = %d, want at least %d within 5s", state.beats(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertAbandonedFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("failedWith = nil, want the shutdown-abandonment error")
	}
	if !IsRetryable(err) {
		t.Fatalf("IsRetryable(%v) = false, want true: an abandoned handler must retry, never dead-letter", err)
	}
	if got := reducerExecutionFailureClass(err); got != wantAbandonedFailureClass {
		t.Fatalf("failure class = %q, want %q", got, wantAbandonedFailureClass)
	}
	if !strings.Contains(err.Error(), "shutdown") {
		t.Fatalf("abandonment error = %q, want it to name shutdown", err.Error())
	}
}

// TestServiceRunAbandonsStuckHandlerAtShutdown is the #8211 regression test
// for the per-item path: the handler ignores its context and never returns
// while heartbeats keep succeeding. Shutdown must stop the heartbeat first
// and fail the intent exactly once; the run must end.
func TestServiceRunAbandonsStuckHandlerAtShutdown(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-stuck-shutdown")
	state := &sharedAbandonState{}
	sink := &abandonSingleSink{state: state}
	executor := &ignoringReducerExecutor{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()

	// The stuck handler holds its lease via successful heartbeats and nothing
	// is terminalized while it does.
	waitForBeats(t, state, 3)
	if acks, fails, _ := sink.counts(); fails != 0 || acks != 0 {
		t.Fatalf("while stuck: ack calls = %d, fail calls = %d, want 0 and 0", acks, fails)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return within 10s of shutdown: a handler that never returns holds its work item forever (RED on unpatched code)")
	}

	acks, fails, failedWith := sink.counts()
	if fails != 1 {
		t.Fatalf("fail calls = %d, want 1: shutdown must release the stuck item through Fail", fails)
	}
	if acks != 0 {
		t.Fatalf("ack calls = %d, want 0: an abandoned handler must never be acked", acks)
	}
	assertAbandonedFailure(t, failedWith)
	if got := state.postFail(); got != 0 {
		t.Fatalf("heartbeats after Fail started = %d, want 0: the heartbeat must stop before Fail", got)
	}
}

// TestServiceRunKeepsCooperativeShutdownFailOnce guards the per-item path for
// a handler that observes shutdown itself: exactly one Fail, no post-Fail
// heartbeat, and the run ends. It passes before and after the fix.
func TestServiceRunKeepsCooperativeShutdownFailOnce(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-cooperative-shutdown")
	state := &sharedAbandonState{}
	sink := &abandonSingleSink{state: state}
	executor := &ctxDoneReducerExecutor{}

	service := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()

	waitForBeats(t, state, 3)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return within 10s of shutdown")
	}

	acks, fails, _ := sink.counts()
	if fails != 1 {
		t.Fatalf("fail calls = %d, want exactly 1", fails)
	}
	if acks != 0 {
		t.Fatalf("ack calls = %d, want 0", acks)
	}
	if got := state.postFail(); got != 0 {
		t.Fatalf("heartbeats after Fail started = %d, want 0", got)
	}
}

// TestServiceRunBatchAbandonsStuckHandlerAtShutdown is the #8211 regression
// test for the batch path (executeAndReport): same stuck-handler shape, same
// stop-heartbeat-before-Fail contract.
func TestServiceRunBatchAbandonsStuckHandlerAtShutdown(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-stuck-batch-shutdown")
	state := &sharedAbandonState{}
	sink := &abandonBatchSink{state: state}
	executor := &ignoringReducerExecutor{}

	svc := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &fakeBatchWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
		Workers:           2,
		BatchClaimSize:    2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.runMainLoop(ctx) }()

	waitForBeats(t, state, 3)
	sink.fakeBatchWorkSink.mu.Lock()
	failedBefore := sink.failed
	sink.fakeBatchWorkSink.mu.Unlock()
	if failedBefore != 0 {
		t.Fatalf("while stuck: fail calls = %d, want 0", failedBefore)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runMainLoop() error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runMainLoop() did not return within 10s of shutdown: a handler that never returns holds its work item forever (RED on unpatched code)")
	}

	sink.fakeBatchWorkSink.mu.Lock()
	failed, acked := sink.failed, sink.acked
	sink.fakeBatchWorkSink.mu.Unlock()
	if failed != 1 {
		t.Fatalf("fail calls = %d, want 1: shutdown must release the stuck item through Fail", failed)
	}
	if acked != 0 {
		t.Fatalf("ack calls = %d, want 0: an abandoned handler must never be acked", acked)
	}
	assertAbandonedFailure(t, sink.failure())
	if got := state.postFail(); got != 0 {
		t.Fatalf("heartbeats after Fail started = %d, want 0: the heartbeat must stop before Fail", got)
	}
}

// blockingStopFunc reports whether it returned before Fail started. It
// releases its own wait on entry, so the green path is fast; the ordering
// verdict comes from the observation at Fail time, not from how long stop
// blocks. With the wrong order, Fail runs before stop is ever entered and the
// test goes red. The backup timer only bounds a run where stop is never
// called at all.
type blockingStopFunc struct {
	entered  atomic.Bool
	returned atomic.Bool
	release  chan struct{}
	once     sync.Once
}

func (f *blockingStopFunc) releaseNow() {
	f.once.Do(func() { close(f.release) })
}

func (f *blockingStopFunc) stop() error {
	f.entered.Store(true)
	f.releaseNow()
	<-f.release
	f.returned.Store(true)
	return nil
}

// TestFailAbandonedHandlerStopsHeartbeatBeforeFail pins the
// stop-heartbeat-before-Fail ordering of the per-item abandonment tail. The
// stop function blocks until released, so a Fail-before-stop mutation reaches
// Fail while stop is still blocked and the test goes red; the release timer
// only bounds the run, never the verdict.
func TestFailAbandonedHandlerStopsHeartbeatBeforeFail(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-abandon-order")
	stop := &blockingStopFunc{release: make(chan struct{})}
	sink := &abandonSingleSink{state: &sharedAbandonState{}}

	time.AfterFunc(10*time.Second, stop.releaseNow)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	execStart := time.Now().Add(-time.Second)
	start := execStart.Add(-time.Second)

	// Wrap the sink to observe whether stop returned before Fail started.
	observing := &orderObservingSink{inner: sink, stop: stop}
	service := Service{Executor: &ignoringReducerExecutor{}, WorkSink: observing}

	if err := service.failAbandonedHandler(ctx, intent, context.Canceled, start, execStart, 0, 0, stop.stop); err != nil {
		t.Fatalf("failAbandonedHandler() error = %v, want nil", err)
	}
	if !observing.stopReturnedBeforeFail.Load() {
		t.Fatal("WorkSink.Fail ran before the heartbeat stop returned: the heartbeat must stop before Fail")
	}
	if observing.fails.Load() != 1 {
		t.Fatalf("fail calls = %d, want 1", observing.fails.Load())
	}
}

type orderObservingSink struct {
	inner                  *abandonSingleSink
	stop                   *blockingStopFunc
	fails                  atomic.Int64
	stopReturnedBeforeFail atomic.Bool
}

func (s *orderObservingSink) Ack(ctx context.Context, intent Intent, result Result) error {
	return s.inner.Ack(ctx, intent, result)
}

func (s *orderObservingSink) Fail(ctx context.Context, intent Intent, err error) error {
	s.fails.Add(1)
	s.stopReturnedBeforeFail.Store(s.stop.returned.Load())
	return s.inner.Fail(ctx, intent, err)
}

// TestFailAbandonedBatchHandlerStopsHeartbeatBeforeFail pins the same ordering
// for the batch abandonment tail.
func TestFailAbandonedBatchHandlerStopsHeartbeatBeforeFail(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-abandon-batch-order")
	stop := &blockingStopFunc{release: make(chan struct{})}
	sink := &abandonSingleSink{state: &sharedAbandonState{}}
	service := Service{Executor: &ignoringReducerExecutor{}, WorkSink: sink}

	time.AfterFunc(10*time.Second, stop.releaseNow)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	execStart := time.Now().Add(-time.Second)
	start := execStart.Add(-time.Second)

	observing := &orderObservingSink{inner: sink, stop: stop}
	service.WorkSink = observing

	_, needsAck, err := service.failAbandonedBatchHandler(ctx, intent, context.Canceled, start, execStart, 0, 0, stop.stop)
	if err != nil {
		t.Fatalf("failAbandonedBatchHandler() error = %v, want nil", err)
	}
	if needsAck {
		t.Fatal("needsAck = true, want false: Fail already terminalized the row")
	}
	if !observing.stopReturnedBeforeFail.Load() {
		t.Fatal("WorkSink.Fail ran before the heartbeat stop returned: the heartbeat must stop before Fail")
	}
	if observing.fails.Load() != 1 {
		t.Fatalf("fail calls = %d, want 1", observing.fails.Load())
	}
}

// TestReducerHandlerAbandonmentMetricsEmitted proves the new operator signals
// exist and carry the domain label: the handler-duration histogram records a
// completed execution, and the abandonment counter records an abandonment.
func TestReducerHandlerAbandonmentMetricsEmitted(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("reducer-abandon"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	service := Service{Instruments: instruments}
	intent := abandonTestIntent("intent-abandon-metrics")

	service.recordReducerHandlerDuration(context.Background(), intent, 1.5)
	service.recordReducerHandlerAbandoned(context.Background(), intent)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	want := map[string]string{"domain": string(intent.Domain)}
	if got := histogramCount(rm, "eshu_dp_reducer_handler_duration_seconds", want); got != 1 {
		t.Fatalf("handler duration histogram points = %d, want 1 with domain %q", got, intent.Domain)
	}
	if got := counterSum(rm, "eshu_dp_reducer_handler_abandoned_total", want); got != 1 {
		t.Fatalf("handler abandoned counter = %d, want 1 with domain %q", got, intent.Domain)
	}
}

// TestHandlerTimeoutForResolvesOverrideThenDefault pins the per-domain
// override slot: override wins, otherwise the default, otherwise zero (no
// bound configured and nothing enforced).
func TestHandlerTimeoutForResolvesOverrideThenDefault(t *testing.T) {
	t.Parallel()

	service := Service{
		HandlerTimeout: 5 * time.Minute,
		HandlerTimeoutOverrides: map[Domain]time.Duration{
			DomainSemanticEntityMaterialization: time.Minute,
		},
	}
	if got := service.HandlerTimeoutFor(DomainSemanticEntityMaterialization); got != time.Minute {
		t.Fatalf("HandlerTimeoutFor(override domain) = %v, want 1m", got)
	}
	if got := service.HandlerTimeoutFor(DomainWorkloadIdentity); got != 5*time.Minute {
		t.Fatalf("HandlerTimeoutFor(other domain) = %v, want 5m default", got)
	}
	if got := (Service{}).HandlerTimeoutFor(DomainWorkloadIdentity); got != 0 {
		t.Fatalf("HandlerTimeoutFor(unset) = %v, want 0 (no bound)", got)
	}
}

func histogramCount(rm metricdata.ResourceMetrics, name string, want map[string]string) uint64 {
	var total uint64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if m.Name != name || !ok {
				continue
			}
			for _, point := range histogram.DataPoints {
				if attributesMatch(point.Attributes, want) {
					total += point.Count
				}
			}
		}
	}
	return total
}

func counterSum(rm metricdata.ResourceMetrics, name string, want map[string]string) int64 {
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != name || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if attributesMatch(point.Attributes, want) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func attributesMatch(attrs attribute.Set, want map[string]string) bool {
	if attrs.Len() != len(want) {
		return false
	}
	for key, value := range want {
		if got, ok := attrs.Value(attribute.Key(key)); !ok || got.AsString() != value {
			return false
		}
	}
	return true
}

// TestServiceRunBatchKeepsCooperativeShutdownFailOnce guards the batch path
// for a handler that observes shutdown itself.
func TestServiceRunBatchKeepsCooperativeShutdownFailOnce(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-cooperative-batch-shutdown")
	state := &sharedAbandonState{}
	sink := &abandonBatchSink{state: state}
	executor := &ctxDoneReducerExecutor{}

	svc := Service{
		PollInterval:      10 * time.Millisecond,
		WorkSource:        &fakeBatchWorkSource{intents: []Intent{intent}},
		Executor:          executor,
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Wait:              func(context.Context, time.Duration) error { return context.Canceled },
		Workers:           2,
		BatchClaimSize:    2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.runMainLoop(ctx) }()

	waitForBeats(t, state, 3)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runMainLoop() error = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runMainLoop() did not return within 10s of shutdown")
	}

	sink.fakeBatchWorkSink.mu.Lock()
	failed, acked := sink.failed, sink.acked
	sink.fakeBatchWorkSink.mu.Unlock()
	if failed != 1 {
		t.Fatalf("fail calls = %d, want exactly 1", failed)
	}
	if acked != 0 {
		t.Fatalf("ack calls = %d, want 0", acked)
	}
	if got := state.postFail(); got != 0 {
		t.Fatalf("heartbeats after Fail started = %d, want 0", got)
	}
}
