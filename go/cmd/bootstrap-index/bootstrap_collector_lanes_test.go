// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// laneRecordingCommitter records committed scope IDs thread-safely and tracks
// the high-water mark of concurrent in-flight commits, so lane tests can
// assert both exactly-once commit coverage and real commit parallelism.
type laneRecordingCommitter struct {
	fakeCommitter

	mu        sync.Mutex
	scopes    []string
	inFlight  atomic.Int64
	highWater atomic.Int64
	// commitDelay widens the overlap window so lanes>1 reliably overlap.
	commitDelay time.Duration
	// failScope, when non-empty, fails the commit for that scope id.
	failScope string
}

func (c *laneRecordingCommitter) CommitScopeGeneration(
	_ context.Context,
	scopeValue scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ <-chan facts.Envelope,
) error {
	cur := c.inFlight.Add(1)
	for {
		hw := c.highWater.Load()
		if cur <= hw || c.highWater.CompareAndSwap(hw, cur) {
			break
		}
	}
	if c.commitDelay > 0 {
		time.Sleep(c.commitDelay)
	}
	defer c.inFlight.Add(-1)
	if c.failScope != "" && scopeValue.ScopeID == c.failScope {
		return errors.New("boom: injected commit failure")
	}
	c.mu.Lock()
	c.scopes = append(c.scopes, scopeValue.ScopeID)
	c.mu.Unlock()
	return nil
}

func laneTestGenerations(n int) []collector.CollectedGeneration {
	gens := make([]collector.CollectedGeneration, 0, n)
	for i := 0; i < n; i++ {
		gens = append(gens, collector.CollectedGeneration{
			Scope: scope.IngestionScope{
				ScopeID:       fmt.Sprintf("scope-%03d", i),
				SourceSystem:  "git",
				ScopeKind:     scope.KindRepository,
				CollectorKind: scope.CollectorGit,
				PartitionKey:  fmt.Sprintf("repo-%03d", i),
			},
			Generation: scope.ScopeGeneration{
				GenerationID: fmt.Sprintf("gen-%03d", i),
				ScopeID:      fmt.Sprintf("scope-%03d", i),
			},
		})
	}
	return gens
}

// TestDrainCollectorLanesCommitsAllScopesExactlyOnce is the #5130 regression
// test: with N commit lanes every collected generation commits exactly once,
// totals are exact, and commits genuinely overlap (the accepted 896-repo run
// measured upsert_facts at max_concurrency=1 — 921.1s strictly serialized;
// see #5122).
func TestDrainCollectorLanesCommitsAllScopesExactlyOnce(t *testing.T) {
	t.Parallel()

	const scopes = 40
	source := &fakeSource{generations: laneTestGenerations(scopes)}
	committer := &laneRecordingCommitter{commitDelay: 5 * time.Millisecond}

	err := drainCollector(context.Background(), source, committer, nil, nil, nil, 4)
	if err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}

	seen := map[string]int{}
	committer.mu.Lock()
	for _, id := range committer.scopes {
		seen[id]++
	}
	committer.mu.Unlock()
	if len(seen) != scopes {
		t.Fatalf("committed %d distinct scopes, want %d", len(seen), scopes)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("scope %s committed %d times, want exactly once", id, n)
		}
	}
	if hw := committer.highWater.Load(); hw < 2 {
		t.Fatalf("commit high-water concurrency = %d with 4 lanes, want >= 2 (commits must overlap)", hw)
	}
}

// TestDrainCollectorSingleLanePreservesSourceOrder pins that lanes=1 keeps
// the serial contract: commits happen in exactly the source's Next order.
func TestDrainCollectorSingleLanePreservesSourceOrder(t *testing.T) {
	t.Parallel()

	const scopes = 12
	source := &fakeSource{generations: laneTestGenerations(scopes)}
	committer := &laneRecordingCommitter{}

	if err := drainCollector(context.Background(), source, committer, nil, nil, nil, 1); err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}
	committer.mu.Lock()
	defer committer.mu.Unlock()
	if len(committer.scopes) != scopes {
		t.Fatalf("committed %d scopes, want %d", len(committer.scopes), scopes)
	}
	for i, id := range committer.scopes {
		want := fmt.Sprintf("scope-%03d", i)
		if id != want {
			t.Fatalf("commit order[%d] = %s, want %s (single lane must preserve source order)", i, id, want)
		}
	}
	if hw := committer.highWater.Load(); hw != 1 {
		t.Fatalf("commit high-water concurrency = %d with 1 lane, want 1", hw)
	}
}

// TestDrainCollectorLanesCommitFailureIsFatal pins the bootstrap error
// contract under lanes: a commit failure fails the drain (bootstrap has no
// retry/dead-letter path) and in-flight work stops without hanging.
func TestDrainCollectorLanesCommitFailureIsFatal(t *testing.T) {
	t.Parallel()

	source := &fakeSource{generations: laneTestGenerations(30)}
	committer := &laneRecordingCommitter{
		commitDelay: time.Millisecond,
		failScope:   "scope-011",
	}

	err := drainCollector(context.Background(), source, committer, nil, nil, nil, 4)
	if err == nil {
		t.Fatal("drainCollector() error = nil, want injected commit failure")
	}
	if !strings.Contains(err.Error(), "injected commit failure") {
		t.Fatalf("drainCollector() error = %v, want injected commit failure", err)
	}
}

// TestCommitLaneCountFromEnv pins the ESHU_BOOTSTRAP_COMMIT_LANES contract:
// default 4 (the measured throughput plateau from the #5122 lane shim — NOT
// CPU count), explicit override honored up to the maxCommitLanes clamp
// (every lane holds an open transaction, so a runaway value would exhaust
// the Postgres connection pool), invalid/zero/negative fall back to the
// default.
func TestCommitLaneCountFromEnv(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want int
	}{
		{"", 4},
		{"1", 1},
		{"8", 8},
		{"64", 64},
		{"10000", 64},
		{"0", 4},
		{"-3", 4},
		{"nope", 4},
	}
	for _, tc := range cases {
		getenv := func(key string) string {
			if key == "ESHU_BOOTSTRAP_COMMIT_LANES" {
				return tc.raw
			}
			return ""
		}
		if got := commitLaneCount(getenv); got != tc.want {
			t.Fatalf("commitLaneCount(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

// cancelAwareSource returns ctx.Err() from Next once the context is
// canceled, mirroring GitSource's stream select.
type cancelAwareSource struct {
	fakeSource
}

func (s *cancelAwareSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if err := ctx.Err(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return s.fakeSource.Next(ctx)
}

// TestDrainCollectorParentCancelIsAnError pins the review finding on #5130:
// a PARENT-driven cancellation (deadline/signal wiring) must surface as a
// collector error, never as a silent partial-collection success. Only the
// self-induced cancel after a lane commit failure may be swallowed (its
// commit error is joined instead).
func TestDrainCollectorParentCancelIsAnError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := &cancelAwareSource{fakeSource{generations: laneTestGenerations(3)}}

	err := drainCollector(ctx, source, &laneRecordingCommitter{}, nil, nil, nil, 4)
	if err == nil {
		t.Fatal("drainCollector() error = nil on parent cancellation, want context error (partial collection must not report success)")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("drainCollector() error = %v, want context.Canceled", err)
	}
}
