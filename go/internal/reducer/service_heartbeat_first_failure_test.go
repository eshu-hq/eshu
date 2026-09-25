// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// slowFailingHeartbeater succeeds on the pre-heartbeat, fails the first
// periodic tick with a real error only after the ticker has already queued
// the next tick, and answers every later call like a driver handed a
// cancelled context.
type slowFailingHeartbeater struct {
	mu        sync.Mutex
	calls     int
	realErr   error
	slowFor   time.Duration
	tickFails chan struct{} // closed as the first periodic tick returns its error
}

func (h *slowFailingHeartbeater) Heartbeat(ctx context.Context, _ Intent) error {
	h.mu.Lock()
	h.calls++
	call := h.calls
	h.mu.Unlock()
	switch call {
	case 1:
		return nil // immediate pre-heartbeat
	case 2:
		// Outlast the heartbeat interval so the ticker channel already holds a
		// ready tick when the goroutine returns to its select.
		time.Sleep(h.slowFor)
		defer close(h.tickFails)
		return h.realErr
	default:
		return context.Canceled
	}
}

// TestStartHeartbeatKeepsRealFailureWhenStopCancelsFollowUpTick pins the #7109
// review finding F1: a tick that already failed for a real reason cancels the
// heartbeat context, and a follow-up tick that the ticker had queued then runs
// on that cancelled context. Neither the stop guard nor a later canceled
// error may erase or replace the recorded real failure.
//
// After the real failure the goroutine's select sees both ctx.Done and a ready
// ticker.C and Go picks between them at random, so each iteration reaches the
// follow-up tick about half the time. The loop makes the buggy interleaving a
// near certainty (1 - 2^-64) instead of a coin flip.
func TestStartHeartbeatKeepsRealFailureWhenStopCancelsFollowUpTick(t *testing.T) {
	t.Parallel()

	errLeaseLost := errors.New("reducer claim rejected")
	for i := 0; i < 64; i++ {
		hb := &slowFailingHeartbeater{
			realErr:   errLeaseLost,
			slowFor:   15 * time.Millisecond,
			tickFails: make(chan struct{}),
		}
		service := Service{Heartbeater: hb, HeartbeatInterval: 10 * time.Millisecond}

		_, stop := service.startHeartbeat(context.Background(), stopRaceIntent(), 0)
		select {
		case <-hb.tickFails:
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: the failing tick never ran", i)
		}

		if err := stop(); !errors.Is(err, errLeaseLost) {
			t.Fatalf("iteration %d: stop() error = %v, want the recorded real failure %v", i, err, errLeaseLost)
		}
	}
}
