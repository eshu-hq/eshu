// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package concurrentreplay_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/concurrentreplay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// scriptedGenerationSource is a minimal collector.Source that hands out a
// fixed set of n distinct generations, one per Next call, then reports
// permanent exhaustion (ok=false, err=nil) forever after. It is the
// "recorded tape" shape concurrentreplay.Source wraps; the poll-restart
// latch behavior is already proven in source_test.go, so the driver tests
// only need a finite tape whose own cursor advance is mutex-guarded (Source
// serializes calls to it, but guarding the delegate directly keeps this
// fake safe even if called outside a Source in a future test).
type scriptedGenerationSource struct {
	mu   sync.Mutex
	next int
	gens []collector.CollectedGeneration
}

// newScriptedGenerationSource builds a scriptedGenerationSource with n
// distinct generations, each with a unique GenerationID and an empty,
// already-closed fact stream (via collector.FactsFromSlice) so committers
// can safely range over Facts without blocking.
func newScriptedGenerationSource(n int) *scriptedGenerationSource {
	gens := make([]collector.CollectedGeneration, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("driver-gen-%03d", i)
		gens = append(gens, collector.FactsFromSlice(
			scope.IngestionScope{ScopeID: "scope-" + id, SourceSystem: "fake"},
			scope.ScopeGeneration{GenerationID: id},
			nil,
		))
	}
	return &scriptedGenerationSource{gens: gens}
}

// Next returns the next scripted generation, or permanent exhaustion once the
// tape is consumed. Safe for concurrent use on its own (guarded by its own
// mutex), independent of any Source wrapper.
func (s *scriptedGenerationSource) Next(_ context.Context) (collector.CollectedGeneration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next >= len(s.gens) {
		return collector.CollectedGeneration{}, false, nil
	}
	gen := s.gens[s.next]
	s.next++
	return gen, true, nil
}

// recordingCommitter records every committed generation ID under a mutex so
// a test can assert exactly-once delivery after concurrent Driver workers
// have all returned.
type recordingCommitter struct {
	mu        sync.Mutex
	committed []string
}

// CommitScopeGeneration drains factStream (mirroring how a real committer
// consumes the fact channel) and records generation.GenerationID as
// committed.
func (c *recordingCommitter) CommitScopeGeneration(
	_ context.Context,
	_ scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	for range factStream {
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.committed = append(c.committed, generation.GenerationID)
	return nil
}

// snapshot returns a copy of the committed generation IDs recorded so far.
func (c *recordingCommitter) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.committed))
	copy(out, c.committed)
	return out
}

// TestDriverCommitsEveryGenerationExactlyOnce proves that concurrent Driver
// workers draining a shared Source commit every recorded generation exactly
// once: no duplicates, none missing, matching Report.GenerationsCommitted.
// Meaningful under -race: recordingCommitter guards its slice append with its
// own mutex, and concurrentreplay.Source's mutex is what makes the shared,
// otherwise single-threaded scriptedGenerationSource delegate safe for the
// Driver's concurrent workers to drain at once.
func TestDriverCommitsEveryGenerationExactlyOnce(t *testing.T) {
	t.Parallel()

	const generationCount = 40
	delegate := newScriptedGenerationSource(generationCount)
	src := concurrentreplay.NewSource(delegate)
	committer := &recordingCommitter{}

	driver := concurrentreplay.Driver{
		Source:    src,
		Committer: committer,
		Workers:   8,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report, err := driver.Run(ctx)
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if got, want := report.GenerationsCommitted, generationCount; got != want {
		t.Fatalf("Report.GenerationsCommitted = %d, want %d", got, want)
	}
	if got, want := report.Workers, 8; got != want {
		t.Fatalf("Report.Workers = %d, want %d", got, want)
	}

	committed := committer.snapshot()
	if got, want := len(committed), generationCount; got != want {
		t.Fatalf("committed %d generations, want exactly %d (no dup/missing)", got, want)
	}
	seen := make(map[string]int, len(committed))
	for _, id := range committed {
		seen[id]++
	}
	if got, want := len(seen), generationCount; got != want {
		t.Fatalf("committed %d unique generations, want %d", got, want)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("generation %q committed %d times, want exactly once", id, count)
		}
	}
	if !src.Drained() {
		t.Error("Source.Drained() = false after Run, want true")
	}
}

// errorAtCallCommitter commits successfully until the failAt-th call
// (1-indexed), then returns wantErr on that call and every call after. Used
// to prove Driver fails fast without requiring every generation to commit.
type errorAtCallCommitter struct {
	mu      sync.Mutex
	calls   int
	failAt  int
	wantErr error
}

// CommitScopeGeneration drains factStream, then either records a successful
// commit or returns c.wantErr once the call count reaches c.failAt.
func (c *errorAtCallCommitter) CommitScopeGeneration(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	for range factStream {
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls >= c.failAt {
		return c.wantErr
	}
	return nil
}

// TestDriverFailsFastOnCommitError proves that once Committer returns an
// error, Driver.Run returns that error (wrapped, so errors.Is still matches
// it) instead of waiting for every generation in the source to commit. The
// test is guarded by a context timeout so a fail-fast bug that deadlocks
// instead of returning promptly fails loudly rather than hanging the suite.
func TestDriverFailsFastOnCommitError(t *testing.T) {
	t.Parallel()

	const generationCount = 200
	wantErr := errors.New("boom: fake commit failure")
	delegate := newScriptedGenerationSource(generationCount)
	src := concurrentreplay.NewSource(delegate)
	committer := &errorAtCallCommitter{failAt: 5, wantErr: wantErr}

	driver := concurrentreplay.Driver{
		Source:    src,
		Committer: committer,
		Workers:   8,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Do not assert every generation committed: the point of fail-fast is
	// that Run returns promptly once the committer fails, whatever partial
	// progress the other concurrent workers made in the meantime.
	_, err := driver.Run(ctx)
	if err == nil {
		t.Fatal("Run: error = nil, want the committer's failure")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run: error = %v, want it to wrap %v", err, wantErr)
	}
	if ctx.Err() != nil {
		t.Fatalf("Run: context deadline exceeded before Run returned (possible deadlock): %v", ctx.Err())
	}
}

// TestDriverWorkersDefaultsToOne proves Workers: 0 behaves as a valid
// sequential run (a single worker), not a panic or a zero-worker no-op.
func TestDriverWorkersDefaultsToOne(t *testing.T) {
	t.Parallel()

	const generationCount = 6
	delegate := newScriptedGenerationSource(generationCount)
	src := concurrentreplay.NewSource(delegate)
	committer := &recordingCommitter{}

	driver := concurrentreplay.Driver{
		Source:    src,
		Committer: committer,
		Workers:   0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report, err := driver.Run(ctx)
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if got, want := report.Workers, 1; got != want {
		t.Fatalf("Report.Workers = %d, want %d (Workers: 0 must default to 1)", got, want)
	}
	if got, want := report.GenerationsCommitted, generationCount; got != want {
		t.Fatalf("Report.GenerationsCommitted = %d, want %d", got, want)
	}
	if got, want := len(committer.snapshot()), generationCount; got != want {
		t.Fatalf("committed %d generations, want %d", got, want)
	}
}

// TestDriverRunNilSource covers the precondition guard: Run must reject a nil
// Source with a sentinel error and without spawning any worker, so a future
// refactor that drops the check is caught.
func TestDriverRunNilSource(t *testing.T) {
	t.Parallel()

	driver := concurrentreplay.Driver{
		Committer: &recordingCommitter{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report, err := driver.Run(ctx)
	if err == nil {
		t.Fatal("Run with nil Source: got nil error, want 'source is required'")
	}
	if !strings.Contains(err.Error(), "source is required") {
		t.Fatalf("Run with nil Source: error %q, want it to contain 'source is required'", err)
	}
	if report.GenerationsCommitted != 0 {
		t.Fatalf("Run with nil Source: GenerationsCommitted = %d, want 0", report.GenerationsCommitted)
	}
}

// TestDriverRunNilCommitter covers the precondition guard: Run must reject a
// nil Committer with a sentinel error even when Source is valid, so no worker
// drains the tape into a missing committer.
func TestDriverRunNilCommitter(t *testing.T) {
	t.Parallel()

	driver := concurrentreplay.Driver{
		Source: concurrentreplay.NewSource(newScriptedGenerationSource(3)),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report, err := driver.Run(ctx)
	if err == nil {
		t.Fatal("Run with nil Committer: got nil error, want 'committer is required'")
	}
	if !strings.Contains(err.Error(), "committer is required") {
		t.Fatalf("Run with nil Committer: error %q, want it to contain 'committer is required'", err)
	}
	if report.GenerationsCommitted != 0 {
		t.Fatalf("Run with nil Committer: GenerationsCommitted = %d, want 0", report.GenerationsCommitted)
	}
}
