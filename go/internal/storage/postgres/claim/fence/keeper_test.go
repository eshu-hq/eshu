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

type repinLog struct {
	mu       sync.Mutex
	outcomes []string
}

func (r *repinLog) record(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcomes = append(r.outcomes, outcome)
}

func (r *repinLog) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.outcomes...)
}

// tickAndWait sends one tick and waits until the keeper has handled it.
func tickAndWait(t *testing.T, ticks chan time.Time, log *repinLog, want int) {
	t.Helper()
	ticks <- time.Time{}
	deadline := time.Now().Add(5 * time.Second)
	for len(log.snapshot()) < want {
		if time.Now().After(deadline) {
			t.Fatalf("keeper handled %d ticks, want %d", len(log.snapshot()), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestKeeperExtendsDeadlineOnEachRepin(t *testing.T) {
	clock := time.Unix(1000, 0)
	var clockMu sync.Mutex
	now := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return clock }
	pinner := &fakePinner{}
	ticks := make(chan time.Time)
	log := &repinLog{}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	request := testRequest()
	request.Lease = time.Minute
	keeper := StartKeeper(ctx, cancel, KeeperConfig{
		Pinner: pinner, Code: testCode, Request: request, FirstDeadline: clock.Add(time.Minute),
		Now: now, Ticks: ticks, OnRepin: log.record,
	})
	clockMu.Lock()
	clock = clock.Add(20 * time.Second)
	clockMu.Unlock()
	tickAndWait(t, ticks, log, 1)
	keeper.Stop()
	if got, want := keeper.LatestDeadline(), time.Unix(1080, 0); !got.Equal(want) {
		t.Fatalf("LatestDeadline() = %v, want %v", got, want)
	}
	if got := log.snapshot(); len(got) != 1 || got[0] != RepinOK {
		t.Fatalf("repin outcomes = %v, want [%s]", got, RepinOK)
	}
	if keeper.Stale() != nil || ctx.Err() != nil {
		t.Fatalf("healthy keeper reported stale=%v ctx=%v", keeper.Stale(), ctx.Err())
	}
}

func TestKeeperStaleRepinCancelsCommitContext(t *testing.T) {
	pinner := &fakePinner{results: []error{codedError{"stale_fence"}}}
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
	tickAndWait(t, ticks, log, 1)
	<-ctx.Done()
	keeper.Stop()
	var rejection *Rejection
	if !errors.As(context.Cause(ctx), &rejection) || rejection.Reason != ReasonStaleFence {
		t.Fatalf("context cause = %v, want stale_fence rejection", context.Cause(ctx))
	}
	if !errors.As(keeper.Stale(), &rejection) || !rejection.TerminalFailure() {
		t.Fatalf("Stale() = %v, want terminal stale rejection", keeper.Stale())
	}
	if got := keeper.LatestDeadline(); !got.Equal(time.Unix(1060, 0)) {
		t.Fatalf("LatestDeadline() = %v, want the first deadline unchanged", got)
	}
	if got := log.snapshot(); len(got) != 1 || got[0] != RepinStale {
		t.Fatalf("repin outcomes = %v, want [%s]", got, RepinStale)
	}
}

func TestKeeperTransientRepinFailureKeepsGoing(t *testing.T) {
	pinner := &fakePinner{results: []error{errors.New("timeout"), nil}}
	ticks := make(chan time.Time)
	log := &repinLog{}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	request := testRequest()
	request.Lease = time.Minute
	keeper := StartKeeper(ctx, cancel, KeeperConfig{
		Pinner: pinner, Code: testCode, Request: request, FirstDeadline: time.Unix(1060, 0),
		Now: func() time.Time { return time.Unix(1030, 0) }, Ticks: ticks, OnRepin: log.record,
	})
	tickAndWait(t, ticks, log, 1)
	tickAndWait(t, ticks, log, 2)
	keeper.Stop()
	if got := log.snapshot(); len(got) != 2 || got[0] != RepinError || got[1] != RepinOK {
		t.Fatalf("repin outcomes = %v, want [%s %s]", got, RepinError, RepinOK)
	}
	if ctx.Err() != nil || keeper.Stale() != nil {
		t.Fatalf("transient failure cancelled the commit: ctx=%v stale=%v", ctx.Err(), keeper.Stale())
	}
	if got := keeper.LatestDeadline(); !got.Equal(time.Unix(1090, 0)) {
		t.Fatalf("LatestDeadline() = %v, want 1090", got)
	}
}

func TestKeeperNeverShortensTheDeadline(t *testing.T) {
	pinner := &fakePinner{}
	ticks := make(chan time.Time)
	log := &repinLog{}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	request := testRequest()
	request.Lease = time.Second
	keeper := StartKeeper(ctx, cancel, KeeperConfig{
		Pinner: pinner, Code: testCode, Request: request, FirstDeadline: time.Unix(5000, 0),
		Now: func() time.Time { return time.Unix(1000, 0) }, Ticks: ticks, OnRepin: log.record,
	})
	tickAndWait(t, ticks, log, 1)
	keeper.Stop()
	if got := keeper.LatestDeadline(); !got.Equal(time.Unix(5000, 0)) {
		t.Fatalf("LatestDeadline() = %v, want 5000 (GREATEST)", got)
	}
}

func TestKeeperStopIsIdempotentAndEndsTheLoop(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	request := testRequest()
	request.Lease = time.Minute
	keeper := StartKeeper(ctx, cancel, KeeperConfig{
		Pinner: &fakePinner{}, Code: testCode, Request: request, FirstDeadline: time.Unix(1, 0),
		Now: time.Now, Ticks: make(chan time.Time),
	})
	keeper.Stop()
	keeper.Stop()
}
