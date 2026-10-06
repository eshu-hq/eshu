// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"sync"
)

// Flight shares one in-flight call per key among concurrent callers, so a
// stale or missing stored row costs one live statement per process instead of
// one per request. The zero value is ready to use. A shared result carries
// the leader's data: it was read in the leader's snapshot, which began before
// the follower's, so the answer can be older than the follower's own
// snapshot by at most the statement's own duration.
type Flight[T any] struct {
	mu    sync.Mutex
	calls map[string]*flightCall[T]
}

type flightCall[T any] struct {
	done    chan struct{}
	value   T
	err     error
	ok      bool // fn returned (did not panic)
	waiting int
}

// Do runs fn once for concurrent callers of the same key. The first caller
// runs fn; callers that arrive while it runs wait and receive its value with
// shared = true. A waiting caller stops when its own ctx ends and returns
// ctx.Err() without running anything, so a cancelled request does not hold its
// transaction until the leader finishes; the leader keeps running for the
// others. When the leader fails or panics, each waiting caller runs fn itself
// and returns its own result, so the leader's error (a cancelled request
// context, for one) never becomes another request's answer. Callers that
// arrive after the call finishes start a new one.
func (f *Flight[T]) Do(ctx context.Context, key string, fn func() (T, error)) (value T, shared bool, err error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = make(map[string]*flightCall[T])
	}
	if call, running := f.calls[key]; running {
		call.waiting++
		f.mu.Unlock()
		select {
		case <-call.done:
		case <-ctx.Done():
			var zero T
			return zero, false, ctx.Err()
		}
		if call.ok && call.err == nil {
			return call.value, true, nil
		}
		value, err = fn()
		return value, false, err
	}
	call := &flightCall[T]{done: make(chan struct{})}
	f.calls[key] = call
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		delete(f.calls, key)
		f.mu.Unlock()
		close(call.done)
	}()
	call.value, call.err = fn()
	call.ok = true
	return call.value, false, call.err
}

// Waiting reports how many callers wait on the in-flight call for key, or -1
// when none is in flight. Tests use it to release a leader only after every
// follower has joined, which keeps a concurrency proof deterministic.
func (f *Flight[T]) Waiting(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	call, running := f.calls[key]
	if !running {
		return -1
	}
	return call.waiting
}
