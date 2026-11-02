// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"errors"
	"sync"
	"time"
)

// fakeHeadroom is a HeadroomSource whose readings the test controls: it
// breaches the connection ceiling once breach reports true, and fails
// while fail reports true.
type fakeHeadroom struct {
	mu     sync.Mutex
	breach func() bool
	fail   func() bool
	calls  int
}

func (h *fakeHeadroom) Name() string { return "fake-headroom" }

func (h *fakeHeadroom) Sample(context.Context) (HeadroomSample, error) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	if h.fail != nil && h.fail() {
		return HeadroomSample{}, errors.New("fake: exporter unreachable")
	}
	s := HeadroomSample{Connections: 5, LockWaiters: 0, MaxLockWaitMS: 0, ReplicationLagSec: 0}
	if h.breach != nil && h.breach() {
		s.Connections = 10_000
	}
	return s, nil
}

func (l *fakeLeg) isSeeded() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seeded
}

// startServerReaper stands in for the deployed claim-plane reaper on a leg
// where the harness may not call the reap route.
func (l *fakeLeg) startServerReaper(interval, requeue time.Duration) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_, _ = l.ReapExpiredClaims(context.Background(), time.Now().UTC(), 1000, requeue)
			}
		}
	}()
	return func() { close(done) }
}
