// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// hangingPinner answers its first `ok` calls and then blocks every later call
// until the call's context ends, the way an HTTP client behaves against a
// stalled claim plane. entered receives one value per blocked call.
type hangingPinner struct {
	mu      sync.Mutex
	ok      int
	calls   []pinCall
	entered chan struct{}
}

func newHangingPinner(ok int) *hangingPinner {
	return &hangingPinner{ok: ok, entered: make(chan struct{}, 16)}
}

func (h *hangingPinner) PinClaimForCommit(
	ctx context.Context,
	claimID string,
	fencingToken int64,
	scopeID, sourceRunID, generationID string,
	commitDeadline time.Time,
) (string, error) {
	h.mu.Lock()
	h.calls = append(h.calls, pinCall{claimID, fencingToken, scopeID, sourceRunID, generationID, commitDeadline})
	answer := len(h.calls) <= h.ok
	h.mu.Unlock()
	if answer {
		return "commit-" + claimID, nil
	}
	h.entered <- struct{}{}
	<-ctx.Done()
	return "", ctx.Err()
}

func (h *hangingPinner) snapshot() []pinCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]pinCall(nil), h.calls...)
}

// within fails the test when fn does not return inside limit.
func within(t *testing.T, limit time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("%s did not return within %s", what, limit)
	}
}

// TestPinTimesOutAHungAttemptAndRetriesIdentically is the D7 transport-timeout
// row on a hang: each attempt is bounded, the three attempts carry identical
// arguments, and the result is the retryable pin_unavailable refusal.
func TestPinTimesOutAHungAttemptAndRetriesIdentically(t *testing.T) {
	pinner := newHangingPinner(0)
	deadline := time.Unix(400, 0)
	var err error
	within(t, 5*time.Second, "Pin against a hung plane", func() {
		_, err = Pin(context.Background(), pinner, testCode, testRequest(), deadline,
			Retry{Attempts: 3, AttemptTimeout: 20 * time.Millisecond})
	})
	var rejection *Rejection
	if !errors.As(err, &rejection) || rejection.Reason != ReasonPinUnavailable || rejection.TerminalFailure() {
		t.Fatalf("Pin() error = %v, want retryable pin_unavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Pin() error = %v, want the attempt deadline as the cause", err)
	}
	calls := pinner.snapshot()
	if len(calls) != 3 {
		t.Fatalf("pin calls = %d, want 3", len(calls))
	}
	for i, call := range calls {
		if call != calls[0] {
			t.Fatalf("call %d = %+v, want identical to %+v", i, call, calls[0])
		}
	}
}

// TestKeeperStopCancelsAHungRepin is the #9376 review P2-1 probe: a re-pin
// blocked on a stalled plane must not hold Stop (and so the already-durable
// commit) hostage. The cancelled attempt is not reported as a re-pin error and
// does not cancel the commit context.
func TestKeeperStopCancelsAHungRepin(t *testing.T) {
	pinner := newHangingPinner(0)
	ticks := make(chan time.Time)
	log := &repinLog{}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	request := testRequest()
	request.Lease = time.Minute
	keeper := StartKeeper(ctx, cancel, KeeperConfig{
		Pinner: pinner, Code: testCode, Request: request, FirstDeadline: time.Unix(1060, 0),
		Now: func() time.Time { return time.Unix(1020, 0) }, Ticks: ticks, OnRepin: log.record,
	})
	ticks <- time.Time{}
	<-pinner.entered
	within(t, 2*time.Second, "Keeper.Stop with a hung re-pin", keeper.Stop)
	if got := log.snapshot(); len(got) != 0 {
		t.Fatalf("repin outcomes = %v, want none for an attempt Stop cancelled", got)
	}
	if ctx.Err() != nil || keeper.Stale() != nil {
		t.Fatalf("Stop cancelled the commit: ctx=%v stale=%v", ctx.Err(), keeper.Stale())
	}
}

// TestKeeperBoundsEachRepinAttempt: a hung re-pin times out as a re-pin
// error, the loop keeps running, and the deadline check stays the judge.
func TestKeeperBoundsEachRepinAttempt(t *testing.T) {
	pinner := newHangingPinner(0)
	ticks := make(chan time.Time)
	log := &repinLog{}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	request := testRequest()
	request.Lease = time.Minute
	keeper := StartKeeper(ctx, cancel, KeeperConfig{
		Pinner: pinner, Code: testCode, Request: request, FirstDeadline: time.Unix(1060, 0),
		Now: func() time.Time { return time.Unix(1020, 0) }, Ticks: ticks, OnRepin: log.record,
		AttemptTimeout: 20 * time.Millisecond,
	})
	tickAndWait(t, ticks, log, 1)
	keeper.Stop()
	if got := log.snapshot(); len(got) != 1 || got[0] != RepinError {
		t.Fatalf("repin outcomes = %v, want [%s]", got, RepinError)
	}
	if ctx.Err() != nil || !keeper.LatestDeadline().Equal(time.Unix(1060, 0)) {
		t.Fatalf("timed-out re-pin changed the commit: ctx=%v latest=%v", ctx.Err(), keeper.LatestDeadline())
	}
}
