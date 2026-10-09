// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"sync"
	"testing"
	"time"
)

// TestPartitionBackoffGraceThenDoubling proves the #7724 2A schedule: K=2
// full-cadence unproductive visits, then min(poll*2^(n-K), T_max).
func TestPartitionBackoffGraceThenDoubling(t *testing.T) {
	t.Parallel()

	const poll = 500 * time.Millisecond
	tracker := newPartitionBackoffTracker(poll, 30*time.Second)
	now := time.Now().UTC()

	if tracker.shouldSkip("sql_relationships", 3, now) {
		t.Fatal("fresh partition should not skip")
	}
	// Grace: visits 1-2 carry no delay.
	if delay := tracker.reportUnproductive("sql_relationships", 3, now); delay != 0 {
		t.Fatalf("visit 1 delay = %v, want 0 (grace)", delay)
	}
	if tracker.shouldSkip("sql_relationships", 3, now) {
		t.Fatal("partition should not skip during grace (visit 1)")
	}
	if delay := tracker.reportUnproductive("sql_relationships", 3, now); delay != 0 {
		t.Fatalf("visit 2 delay = %v, want 0 (grace)", delay)
	}
	if tracker.shouldSkip("sql_relationships", 3, now) {
		t.Fatal("partition should not skip during grace (visit 2)")
	}
	// Doubling: visit n=3 -> poll*2, n=4 -> poll*4, n=5 -> poll*8.
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	for i, wantDelay := range want {
		delay := tracker.reportUnproductive("sql_relationships", 3, now)
		if delay != wantDelay {
			t.Fatalf("visit %d delay = %v, want %v", i+3, delay, wantDelay)
		}
		if !tracker.shouldSkip("sql_relationships", 3, now) {
			t.Fatalf("visit %d should skip at the same instant", i+3)
		}
		if tracker.shouldSkip("sql_relationships", 3, now.Add(wantDelay)) {
			t.Fatalf("visit %d should be eligible once its delay elapses", i+3)
		}
	}
}

// TestPartitionBackoffCapsAtMax proves the delay never exceeds T_max and
// the tracker counts partitions pinned there.
func TestPartitionBackoffCapsAtMax(t *testing.T) {
	t.Parallel()

	const poll = 500 * time.Millisecond
	const maxBackoff = 3 * time.Second
	tracker := newPartitionBackoffTracker(poll, maxBackoff)
	now := time.Now().UTC()

	var delay time.Duration
	for i := 0; i < 10; i++ {
		delay = tracker.reportUnproductive("sql_relationships", 3, now)
	}
	if delay != maxBackoff {
		t.Fatalf("delay after 10 unproductive visits = %v, want %v (T_max cap)", delay, maxBackoff)
	}
	if got := tracker.countAtMax(); got != 1 {
		t.Fatalf("countAtMax = %d, want 1", got)
	}
	if _, atMax := tracker.delayFor("sql_relationships", 3); !atMax {
		t.Fatal("delayFor should report atMax for a capped partition")
	}
	if _, atMax := tracker.delayFor("sql_relationships", 4); atMax {
		t.Fatal("delayFor should not report atMax for a never-visited partition")
	}
}

// TestPartitionBackoffResetsOnProductive proves any productive visit
// (ProcessedIntents > 0 of any completion kind) resets the partition to
// full cadence.
func TestPartitionBackoffResetsOnProductive(t *testing.T) {
	t.Parallel()

	tracker := newPartitionBackoffTracker(500*time.Millisecond, 30*time.Second)
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		tracker.reportUnproductive("sql_relationships", 3, now)
	}
	if !tracker.shouldSkip("sql_relationships", 3, now) {
		t.Fatal("backed-off partition should skip before the productive visit")
	}
	tracker.reportProductive("sql_relationships", 3)
	if tracker.shouldSkip("sql_relationships", 3, now) {
		t.Fatal("productive visit must reset the partition to full cadence")
	}
	if delay, _ := tracker.delayFor("sql_relationships", 3); delay != 0 {
		t.Fatalf("delay after productive visit = %v, want 0", delay)
	}
	// The next unproductive run starts from grace again, not from the
	// previous depth.
	if delay := tracker.reportUnproductive("sql_relationships", 3, now); delay != 0 {
		t.Fatalf("first visit after reset delay = %v, want 0 (grace)", delay)
	}
}

// TestPartitionBackoffIsPerPartition proves partitions back off
// independently: one stuck partition never slows another.
func TestPartitionBackoffIsPerPartition(t *testing.T) {
	t.Parallel()

	tracker := newPartitionBackoffTracker(500*time.Millisecond, 30*time.Second)
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		tracker.reportUnproductive("sql_relationships", 3, now)
	}
	if !tracker.shouldSkip("sql_relationships", 3, now) {
		t.Fatal("stuck partition should skip")
	}
	if tracker.shouldSkip("sql_relationships", 4, now) {
		t.Fatal("sibling partition must stay at full cadence")
	}
	if tracker.shouldSkip("runs_in", 3, now) {
		t.Fatal("sibling domain must stay at full cadence")
	}
}

// TestPartitionBackoffSnapshot proves the debug surface reports every
// tracked partition's state, sorted and complete.
func TestPartitionBackoffSnapshot(t *testing.T) {
	t.Parallel()

	tracker := newPartitionBackoffTracker(500*time.Millisecond, time.Second)
	now := time.Now().UTC()

	for i := 0; i < 4; i++ {
		tracker.reportUnproductive("sql_relationships", 3, now)
	}
	tracker.reportProductive("runs_in", 0)

	snapshot := tracker.snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("snapshot has %d entries, want 2", len(snapshot))
	}
	if snapshot[0].Domain != "runs_in" || snapshot[1].Domain != "sql_relationships" {
		t.Fatalf("snapshot not sorted by domain: %+v", snapshot)
	}
	stuck := snapshot[1]
	if stuck.PartitionID != 3 || stuck.UnproductiveVisits != 4 {
		t.Fatalf("stuck entry = %+v, want partition 3 with 4 unproductive visits", stuck)
	}
	if stuck.Delay != time.Second || !stuck.AtMax {
		t.Fatalf("stuck entry = %+v, want delay 1s at max", stuck)
	}
	if !stuck.NextEligibleAt.Equal(now.Add(time.Second)) {
		t.Fatalf("stuck nextEligibleAt = %v, want %v", stuck.NextEligibleAt, now.Add(time.Second))
	}
	if snapshot[0].UnproductiveVisits != 0 || snapshot[0].Delay != 0 || snapshot[0].AtMax {
		t.Fatalf("productive entry = %+v, want zero state", snapshot[0])
	}
}

// TestPartitionBackoffConcurrentSafe hammers the tracker from concurrent
// workers; it passes under -race only when every access is guarded.
func TestPartitionBackoffConcurrentSafe(t *testing.T) {
	t.Parallel()

	tracker := newPartitionBackoffTracker(10*time.Millisecond, 100*time.Millisecond)
	now := time.Now().UTC()

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				domain := []string{"sql_relationships", "runs_in"}[worker%2]
				tracker.shouldSkip(domain, worker, now)
				tracker.reportUnproductive(domain, worker, now)
				tracker.reportProductive(domain, worker)
				tracker.delayFor(domain, worker)
				tracker.countAtMax()
				tracker.snapshot()
			}
		}(worker)
	}
	wg.Wait()
}
