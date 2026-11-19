// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// timedHeartbeatCall is one Heartbeater.Heartbeat invocation with its wall
// time, so tests can prove hold heartbeats landed during a buffer dwell
// (before the worker started executing the same intent).
type timedHeartbeatCall struct {
	intentID string
	at       time.Time
}

// timedHeartbeater records every Heartbeat call per intent. failIntentIDs
// names intents whose calls after the first (the synchronous pre-heartbeat)
// fail: the pre-heartbeat succeeds so the hold starts and the item enters
// the buffer, then periodic ticks die during the dwell. Intents not named
// always succeed.
type timedHeartbeater struct {
	mu            sync.Mutex
	calls         map[string][]timedHeartbeatCall
	failIntentIDs map[string]bool
}

func newTimedHeartbeater(failIntentIDs ...string) *timedHeartbeater {
	fail := make(map[string]bool, len(failIntentIDs))
	for _, id := range failIntentIDs {
		fail[id] = true
	}
	return &timedHeartbeater{calls: make(map[string][]timedHeartbeatCall), failIntentIDs: fail}
}

func (h *timedHeartbeater) Heartbeat(_ context.Context, intent reducercontract.Intent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	prior := len(h.calls[intent.IntentID])
	h.calls[intent.IntentID] = append(h.calls[intent.IntentID], timedHeartbeatCall{intentID: intent.IntentID, at: time.Now()})
	if prior > 0 && h.failIntentIDs[intent.IntentID] {
		return context.DeadlineExceeded
	}
	return nil
}

func (h *timedHeartbeater) callsBefore(id string, before time.Time) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.calls[id] {
		if c.at.Before(before) {
			n++
		}
	}
	return n
}

// inflightHeartbeater tracks the maximum concurrent in-flight Heartbeat calls
// per intent. A hold loop that overlaps a worker heartbeat for the same
// intent shows max > 1; strict stop-hold-before-worker-heartbeat sequencing
// keeps it at exactly 1.
type inflightHeartbeater struct {
	mu       sync.Mutex
	inflight map[string]int
	maxSeen  map[string]int
	sleep    time.Duration
}

func newInflightHeartbeater(sleep time.Duration) *inflightHeartbeater {
	return &inflightHeartbeater{inflight: make(map[string]int), maxSeen: make(map[string]int), sleep: sleep}
}

func (h *inflightHeartbeater) Heartbeat(ctx context.Context, intent reducercontract.Intent) error {
	h.mu.Lock()
	h.inflight[intent.IntentID]++
	if h.inflight[intent.IntentID] > h.maxSeen[intent.IntentID] {
		h.maxSeen[intent.IntentID] = h.inflight[intent.IntentID]
	}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.inflight[intent.IntentID]--
		h.mu.Unlock()
	}()
	select {
	case <-time.After(h.sleep):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *inflightHeartbeater) maxInflight(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.maxSeen[id]
}

// failPreheartbeatsHeartbeater fails every Heartbeat call, so every hold
// start fails its synchronous pre-heartbeat and the item is evicted at
// insert time without ever entering the buffer.
type failPreheartbeatsHeartbeater struct{}

func (failPreheartbeatsHeartbeater) Heartbeat(context.Context, reducercontract.Intent) error {
	return context.DeadlineExceeded
}

// failSecondCallHeartbeater succeeds the first call per intent (the
// synchronous pre-heartbeat, whether hold or worker) and fails every later
// call. With an hour-scale interval no periodic tick fires, so the second
// call is always the worker pre-heartbeat after a clean hold stop.
type failSecondCallHeartbeater struct {
	mu    sync.Mutex
	calls map[string]int
}

func (h *failSecondCallHeartbeater) Heartbeat(_ context.Context, intent reducercontract.Intent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.calls == nil {
		h.calls = make(map[string]int)
	}
	h.calls[intent.IntentID]++
	if h.calls[intent.IntentID] > 1 {
		return context.DeadlineExceeded
	}
	return nil
}

// gatedStartReducerExecutor records execution-start order and parks each
// execution on a per-intent gate the test opens, so the test can release
// workers one at a time and prove pop order is FIFO.
type gatedStartReducerExecutor struct {
	mu     sync.Mutex
	starts []string
	gates  map[string]chan struct{}
}

func newGatedStartReducerExecutor(ids ...string) *gatedStartReducerExecutor {
	gates := make(map[string]chan struct{}, len(ids))
	for _, id := range ids {
		gates[id] = make(chan struct{})
	}
	return &gatedStartReducerExecutor{gates: gates}
}

func (e *gatedStartReducerExecutor) Execute(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	e.mu.Lock()
	e.starts = append(e.starts, intent.IntentID)
	gate := e.gates[intent.IntentID]
	e.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return reducercontract.Result{}, ctx.Err()
		}
	}
	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   ResultStatusSucceeded,
	}, nil
}

func (e *gatedStartReducerExecutor) open(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if gate, ok := e.gates[id]; ok {
		close(gate)
		delete(e.gates, id)
	}
}

func (e *gatedStartReducerExecutor) startOrder() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.starts...)
}

// TestServiceRunBatchConcurrentPrefetchBoundedByBufferDepth replaces
// TestServiceRunBatchConcurrentDoesNotPreclaimBeyondReadyWorkers. The old
// test pinned lease-safety-via-rendezvous (claim only what idle workers can
// take right now); the new contract is lease-safety-via-hold-heartbeat: the
// claimer may hand out up to free-buffer-slots items because every
// buffered item holds its lease with a heartbeat until a worker pops it
// (#10867). Workers=2 with depth 2 and a blocking executor must show 4
// claimed while both workers are busy (2 executing + 2 buffered), never the
// rendezvous 2 and never 5.
func TestServiceRunBatchConcurrentPrefetchBoundedByBufferDepth(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(5)}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &blockingManyReducerExecutor{
		release: release,
		started: make(chan string, 5),
	}

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Workers:            2,
		BatchClaimSize:     8,
		BatchPrefetchDepth: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

	// Both workers are blocked in execution, so the claimer must have
	// refilled the drained buffer by now: poll for the quiescent count
	// instead of asserting mid-race, since pops only notify after they
	// happen.
	pollDeadline := time.Now().Add(5 * time.Second)
	for source.claimedCount() != 4 {
		if time.Now().After(pollDeadline) {
			t.Fatalf("claimed count while both workers are busy = %d, want 4 (2 executing + 2 hold-heartbeated in the buffer)", source.claimedCount())
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context ended waiting for the prefetch refill: %v", ctx.Err())
		}
	}
	for _, limit := range source.claimLimits() {
		if limit > 8 {
			t.Fatalf("ClaimBatch limit = %d, want at most batchClaimSize 8", limit)
		}
	}
	// The first claim always fills the empty buffer to the depth, with no
	// idle-worker race: free slots alone bound the claim.
	if got := source.claimLimits(); len(got) == 0 || got[0] != 2 {
		t.Fatalf("first ClaimBatch limit = %v, want exactly 2 (the empty buffer's free slots)", got)
	}

	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	if got := executor.count.Load(); got != 5 {
		t.Fatalf("executor count = %d, want 5", got)
	}
}

// TestServiceRunBatchConcurrentPrefetchZeroDegenerates proves depth 0 is
// exactly today's idle-only claiming: with both workers busy only the 2
// executing items are claimed and no ClaimBatch limit exceeds the 2 idle
// workers that existed at claim time.
func TestServiceRunBatchConcurrentPrefetchZeroDegenerates(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(5)}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &blockingManyReducerExecutor{
		release: release,
		started: make(chan string, 5),
	}

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Workers:            2,
		BatchClaimSize:     8,
		BatchPrefetchDepth: 0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

	if got, want := source.claimedCount(), 2; got != want {
		t.Fatalf("claimed count while both workers are busy = %d, want %d (depth 0 claims idle workers only)", got, want)
	}
	for _, limit := range source.claimLimits() {
		if limit > 2 {
			t.Fatalf("ClaimBatch limit = %d, want at most ready worker count 2", limit)
		}
	}

	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	if got := executor.count.Load(); got != 5 {
		t.Fatalf("executor count = %d, want 5", got)
	}
}

// TestServiceRunBatchPrefetchCappedByBatchClaimSize proves a depth larger
// than the claim size never raises a ClaimBatch limit past the claim size:
// depth 64 with BatchClaimSize 3 still claims at most 3 per call.
func TestServiceRunBatchPrefetchCappedByBatchClaimSize(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(10)}
	sink := &fakeBatchWorkSink{}
	release := make(chan struct{})
	executor := &blockingManyReducerExecutor{
		release: release,
		started: make(chan string, 10),
	}

	service := Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         source,
		Executor:           executor,
		WorkSink:           sink,
		Workers:            2,
		BatchClaimSize:     3,
		BatchPrefetchDepth: 64,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

	// Workers block in execution after popping the first claim, so the
	// claimer must refill to 5 (3 + 2) and then park on the full buffer:
	// poll for that quiescent count.
	pollDeadline := time.Now().Add(5 * time.Second)
	for source.claimedCount() != 5 {
		if time.Now().After(pollDeadline) {
			t.Fatalf("claimed count while both workers are busy = %d, want 5 (first claim 3 plus a 2-item refill)", source.claimedCount())
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("context ended waiting for the prefetch refill: %v", ctx.Err())
		}
	}

	close(release)
	if err := <-errc; err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	if got := executor.count.Load(); got != 10 {
		t.Fatalf("executor count = %d, want 10", got)
	}
	limits := source.claimLimits()
	if len(limits) == 0 {
		t.Fatal("fake source saw no ClaimBatch calls")
	}
	for _, limit := range limits {
		if limit > 3 {
			t.Fatalf("ClaimBatch limit = %d, want at most batchClaimSize 3 despite depth 64", limit)
		}
	}
	// The free buffer slots saturate the bound, so the first claim uses the
	// whole claim size.
	if limits[0] != 3 {
		t.Fatalf("first ClaimBatch limit = %d, want 3 (free buffer slots, capped by claim size)", limits[0])
	}
}

// TestServiceWorkersOneIgnoresPrefetch proves Workers=1 never enters the
// batch path: no ClaimBatch call happens and no buffer is created even with
// a nonzero prefetch depth.
func TestServiceWorkersOneIgnoresPrefetch(t *testing.T) {
	t.Parallel()

	source := &fakeBatchWorkSource{intents: makeTestIntents(3)}
	sink := &fakeBatchWorkSink{}
	service := Service{
		PollInterval:       time.Millisecond,
		WorkSource:         source,
		Executor:           &countingExecutor{},
		WorkSink:           sink,
		Workers:            1,
		BatchClaimSize:     8,
		BatchPrefetchDepth: 4,
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.runMainLoop(context.Background()); err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}
	// The sequential path uses single Claim, which shares the fake's claimed
	// counter; the batch assertion is on ClaimBatch calls, which must be
	// zero.
	if got := source.claimLimits(); len(got) != 0 {
		t.Fatalf("ClaimBatch calls = %d, want 0 (Workers=1 stays on the sequential path)", len(got))
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.acked != 3 {
		t.Fatalf("sink acked = %d, want 3 sequential acks", sink.acked)
	}
}

// TestBatchPrefetchDepthHelper pins the depth helper beside batchClaimSize:
// non-positive disables (the zero value preserves today's rendezvous), and a
// positive depth is capped by the claim size.
func TestBatchPrefetchDepthHelper(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		depth     int
		claimSize int
		workers   int
		want      int
	}{
		{name: "zero disables", depth: 0, claimSize: 8, workers: 2, want: 0},
		{name: "negative disables", depth: -1, claimSize: 8, workers: 2, want: 0},
		{name: "explicit depth", depth: 2, claimSize: 8, workers: 2, want: 2},
		{name: "capped by claim size", depth: 64, claimSize: 3, workers: 2, want: 3},
		{name: "capped when claim size below workers", depth: 8, claimSize: 2, workers: 4, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := Service{Workers: tc.workers, BatchClaimSize: tc.claimSize, BatchPrefetchDepth: tc.depth}
			if got := s.batchPrefetchDepth(); got != tc.want {
				t.Fatalf("batchPrefetchDepth() = %d, want %d", got, tc.want)
			}
		})
	}
}
