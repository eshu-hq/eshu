// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package concurrentreplay

import (
	"context"
	"fmt"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/collector"
)

// Source wraps a single-threaded collector.Source delegate (for example
// cassette.Source) behind a mutex so it is safe for concurrent Next calls, and
// latches the delegate's first drain or error signal so a recorded tape is
// delivered exactly once across the wrapper's lifetime regardless of how many
// callers race to drain it.
//
// The delegate's own Next may not be safe for concurrent use — cassette.Source
// is not, because its scope cursor is unsynchronized per-instance state — so
// Source holds its lock across the entire delegate call, not just around a
// counter. The delegate call itself is in-memory (no I/O, no network, no
// blocking syscall), so serializing it does not create the contended
// production path the repository's Serialization-Is-Not-A-Fix rule warns
// against: tape handout is inherently sequential (one cursor, one file), and
// the expensive work — committing the generation's facts — happens outside
// this lock, once per caller, after Next returns.
//
// See doc.go for the full rationale, including why the one-shot latch is a
// semantic requirement (defeating the delegate's poll-restart) rather than a
// concurrency-defect workaround.
type Source struct {
	mu       sync.Mutex
	delegate collector.Source
	done     bool
	served   int
}

// NewSource returns a Source that wraps delegate. The delegate is assumed to
// be single-threaded (Next is not safe for concurrent use on its own); Source
// makes it safe for concurrent callers.
func NewSource(delegate collector.Source) *Source {
	return &Source{delegate: delegate}
}

// Next returns the next collected generation from the wrapped delegate,
// exactly once per recorded generation, safe for concurrent use by multiple
// callers.
//
// The delegate is invoked under the wrapper's lock. Once the delegate reports
// ok=false (batch exhausted) or a non-nil error, Source latches into a
// permanently drained state: the error (if any) is returned exactly once, on
// the call that observed it, and every subsequent call — from this or any
// other goroutine — returns the zero CollectedGeneration, ok=false, err=nil
// without invoking the delegate again. This defeats delegate implementations
// that restart their internal cursor after the first ok=false (as
// cassette.Source does for the production single-threaded poll loop), which
// would otherwise re-deliver the same recorded tape to a second wave of
// concurrent callers.
func (s *Source) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done {
		return collector.CollectedGeneration{}, false, nil
	}

	gen, ok, err := s.delegate.Next(ctx)
	if err != nil {
		s.done = true
		return collector.CollectedGeneration{}, false, fmt.Errorf("concurrentreplay: delegate next: %w", err)
	}
	if !ok {
		s.done = true
		return collector.CollectedGeneration{}, false, nil
	}

	s.served++
	return gen, true, nil
}

// Drained reports whether the wrapper has latched into its permanently
// drained state, either because the delegate reported batch exhaustion
// (ok=false) or because the delegate returned an error. Safe for concurrent
// use.
func (s *Source) Drained() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// Served returns the count of generations successfully delivered by Next
// before the wrapper latched into its drained state. Safe for concurrent use.
func (s *Source) Served() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.served
}
