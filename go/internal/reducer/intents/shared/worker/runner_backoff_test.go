// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// errBackoffTestWrite fails edge writes for the error-hold test.
var errBackoffTestWrite = errors.New("backoff test write failure")

// backoffTestRunner builds a Runner with granted leases, an empty reader,
// and a controllable clock.
func backoffTestRunner(now *time.Time, configure func(*Runner)) (*Runner, *fakeLeaseManager) {
	leaseManager := &fakeLeaseManager{granted: true}
	runner := &Runner{
		IntentReader: &fakeSharedIntentReader{},
		LeaseManager: leaseManager,
		EdgeWriter:   &fakeEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: RunnerConfig{
			PartitionCount: 1,
			PollInterval:   100 * time.Millisecond,
		},
	}
	runner.nowFn = func() time.Time { return *now }
	if configure != nil {
		configure(runner)
	}
	return runner, leaseManager
}

func leaseClaims(leaseManager *fakeLeaseManager) int {
	leaseManager.mu.Lock()
	defer leaseManager.mu.Unlock()
	return leaseManager.claims
}

// TestRunnerSkipsUnproductivePartitionsAfterGrace drives whole cycles with
// a fake clock: two full-cadence grace cycles, a third visit that arms the
// delay, then skips until the delay elapses (#7724 2A).
func TestRunnerSkipsUnproductivePartitionsAfterGrace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	runner, leases := backoffTestRunner(&now, nil)
	ctx := context.Background()

	cycle1 := runner.runOneCycle(ctx)
	if cycle1.PartitionsVisited != 11 || cycle1.PartitionsBackoffSkipped != 0 {
		t.Fatalf("cycle 1 = visited %d skipped %d, want 11/0 (grace)", cycle1.PartitionsVisited, cycle1.PartitionsBackoffSkipped)
	}
	cycle2 := runner.runOneCycle(ctx)
	if cycle2.PartitionsVisited != 11 || cycle2.PartitionsBackoffSkipped != 0 {
		t.Fatalf("cycle 2 = visited %d skipped %d, want 11/0 (grace)", cycle2.PartitionsVisited, cycle2.PartitionsBackoffSkipped)
	}
	cycle3 := runner.runOneCycle(ctx)
	if cycle3.PartitionsVisited != 11 || cycle3.PartitionsBackoffSkipped != 0 {
		t.Fatalf("cycle 3 = visited %d skipped %d, want 11/0 (arms the delay)", cycle3.PartitionsVisited, cycle3.PartitionsBackoffSkipped)
	}
	if got := leaseClaims(leases); got != 33 {
		t.Fatalf("lease claims after 3 cycles = %d, want 33", got)
	}

	cycle4 := runner.runOneCycle(ctx)
	if cycle4.PartitionsVisited != 0 || cycle4.PartitionsBackoffSkipped != 11 {
		t.Fatalf("cycle 4 = visited %d skipped %d, want 0/11 (backed off)", cycle4.PartitionsVisited, cycle4.PartitionsBackoffSkipped)
	}
	if got := leaseClaims(leases); got != 33 {
		t.Fatalf("lease claims after skipped cycle = %d, want 33 (no claims while backed off)", got)
	}

	now = now.Add(200 * time.Millisecond)
	cycle5 := runner.runOneCycle(ctx)
	if cycle5.PartitionsVisited != 11 || cycle5.PartitionsBackoffSkipped != 0 {
		t.Fatalf("cycle 5 = visited %d skipped %d, want 11/0 (delay elapsed)", cycle5.PartitionsVisited, cycle5.PartitionsBackoffSkipped)
	}

	state := runner.BackoffState()
	if len(state) != 11 {
		t.Fatalf("BackoffState has %d entries, want 11", len(state))
	}
	for _, entry := range state {
		if entry.UnproductiveVisits != 4 {
			t.Fatalf("entry %+v has %d unproductive visits, want 4", entry, entry.UnproductiveVisits)
		}
	}
}

// TestRunnerResetsBackoffOnProductiveVisit proves a partition that
// completes work returns to full cadence immediately.
func TestRunnerResetsBackoffOnProductiveVisit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	reader := &fakeSharedIntentReader{}
	runner, leases := backoffTestRunner(&now, func(runner *Runner) { runner.IntentReader = reader })
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		runner.runOneCycle(ctx)
	}
	if got := leaseClaims(leases); got != 33 {
		t.Fatalf("claims after 3 idle cycles = %d, want 33", got)
	}

	// The delay is now armed (poll*2 = 200ms). Let it elapse, inject one
	// ready intent, and run: the productive visit resets the cell.
	now = now.Add(200 * time.Millisecond)
	reader.mu.Lock()
	reader.intents = append(reader.intents, selectionTestRow(reducercontract.DomainWorkloadDependency, "scope-a", "unit-a", "gen-1", 0))
	reader.mu.Unlock()

	productive := runner.runOneCycle(ctx)
	if productive.ProcessedIntents == 0 {
		t.Fatal("cycle with a ready intent processed nothing")
	}
	for _, entry := range runner.BackoffState() {
		if entry.Domain == reducercontract.DomainWorkloadDependency && entry.UnproductiveVisits != 0 {
			t.Fatalf("productive partition still has %d unproductive visits, want 0", entry.UnproductiveVisits)
		}
	}

	// The reset partition visits at full cadence on the very next cycle
	// at the same instant; the still-idle partitions stay backed off.
	before := leaseClaims(leases)
	next := runner.runOneCycle(ctx)
	after := leaseClaims(leases)
	if after-before != 1 {
		t.Fatalf("claims on the next cycle = %d, want 1 (only the reset partition)", after-before)
	}
	if next.PartitionsVisited != 1 || next.PartitionsBackoffSkipped != 10 {
		t.Fatalf("next cycle = visited %d skipped %d, want 1/10", next.PartitionsVisited, next.PartitionsBackoffSkipped)
	}
}

// TestRunnerHoldsBackoffOnLeaseMiss proves lease-not-acquired visits never
// advance the backoff counter: every cycle retries every partition.
func TestRunnerHoldsBackoffOnLeaseMiss(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	runner, leases := backoffTestRunner(&now, func(runner *Runner) {
		runner.LeaseManager.(*fakeLeaseManager).granted = false
	})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		cycle := runner.runOneCycle(ctx)
		if cycle.PartitionsLeaseHeld != 11 {
			t.Fatalf("cycle %d lease-held = %d, want 11", i+1, cycle.PartitionsLeaseHeld)
		}
		if cycle.PartitionsBackoffSkipped != 0 {
			t.Fatalf("cycle %d skipped = %d, want 0 (holds never back off)", i+1, cycle.PartitionsBackoffSkipped)
		}
	}
	if got := leaseClaims(leases); got != 55 {
		t.Fatalf("claims = %d, want 55 (every partition every cycle)", got)
	}
	if state := runner.BackoffState(); len(state) != 0 {
		t.Fatalf("BackoffState has %d entries, want 0 (holds track nothing)", len(state))
	}
}

// TestRunnerHoldsBackoffOnError proves error visits never advance the
// backoff counter: a faulting partition keeps retrying at full cadence
// instead of hiding behind silence.
func TestRunnerHoldsBackoffOnError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	reader := &fakeSharedIntentReader{
		intents: []sharedintent.Row{selectionTestRow(reducercontract.DomainWorkloadDependency, "scope-a", "unit-a", "gen-1", 0)},
	}
	runner, leases := backoffTestRunner(&now, func(runner *Runner) {
		runner.IntentReader = reader
		runner.EdgeWriter = &fakeEdgeWriter{writeErr: errBackoffTestWrite}
	})
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		runner.runOneCycle(ctx)
	}
	// Cycles 1-3 visit everything (33 claims); cycle 4 visits only the
	// erroring partition while the ten idle ones back off.
	if got := leaseClaims(leases); got != 34 {
		t.Fatalf("claims = %d, want 34 (error partition never backs off)", got)
	}
	for _, entry := range runner.BackoffState() {
		if entry.Domain == reducercontract.DomainWorkloadDependency && entry.UnproductiveVisits != 0 {
			t.Fatalf("erroring partition has %d unproductive visits, want 0 (hold)", entry.UnproductiveVisits)
		}
	}
}

// TestRunnerBlockedOnlyCycleBacksOffGlobally proves the #7724 2A global
// pin removal: a cycle with blocked rows but zero completions engages the
// global consecutive-empty backoff instead of pinning at the poll
// interval. The first three cycles run at full cadence (per-partition
// grace), so their doubling intervals prove the pin is gone.
func TestRunnerBlockedOnlyCycleBacksOffGlobally(t *testing.T) {
	t.Parallel()

	pending := []sharedintent.Row{selectionTestRow(reducercontract.DomainSQLRelationships, "scope-a", "unit-a", "gen-1", 0)}
	neverReady := func(_ context.Context, _ []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) { return false, false }, nil
	}
	acceptAll := func(_ context.Context, _ []sharedintent.Row) (sharedintent.AcceptedGenerationLookup, error) {
		return func(sharedintent.AcceptanceKey) (string, bool) { return "gen-1", true }, nil
	}

	var intervals []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &Runner{
		IntentReader:        &fakeSharedIntentReader{intents: pending},
		LeaseManager:        &fakeLeaseManager{granted: true},
		EdgeWriter:          &fakeEdgeWriter{},
		AcceptedGen:         acceptedGenerationFixed("gen-1", true),
		AcceptedGenPrefetch: acceptAll,
		ReadinessPrefetch:   neverReady,
		Config: RunnerConfig{
			PartitionCount: 1,
			PollInterval:   50 * time.Millisecond,
		},
		Wait: func(ctx context.Context, interval time.Duration) error {
			intervals = append(intervals, interval)
			if len(intervals) >= 4 {
				cancel()
				return ctx.Err()
			}
			return nil
		},
	}

	_ = runner.Run(ctx)

	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	if len(intervals) != len(want) {
		t.Fatalf("wait intervals = %v, want %v", intervals, want)
	}
	for i := range want {
		if intervals[i] != want[i] {
			t.Fatalf("wait intervals = %v, want %v (blocked rows must not pin the poll interval)", intervals, want)
		}
	}
}

// TestRunnerPicksUpWorkWithinBackoffBound is the #7724 2A starvation soak:
// every partition backs off to T_max behind a blocked head, then ready
// work arrives and must drain within T_max. No sleeps: the fake clock
// advances one poll per cycle.
func TestRunnerPicksUpWorkWithinBackoffBound(t *testing.T) {
	t.Parallel()

	const domain = reducercontract.DomainSQLRelationships
	const poll = 50 * time.Millisecond
	const maxBackoff = 300 * time.Millisecond

	var pending []sharedintent.Row
	for i := 0; i < 10; i++ {
		pending = append(pending, selectionTestRow(domain, "scope-blocked", "unit-a", "gen-1", i))
	}
	reader := &fakeSharedIntentReader{intents: pending}
	// Blocked head rows never become ready; the scope-ready row behind
	// them is ready from the moment it is injected.
	readyBehindBlocked := func(_ context.Context, _ []gpphase.PhaseKey, _ gpphase.Phase) (gpphase.ReadinessLookup, error) {
		return func(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
			return key.ScopeID == "scope-ready", true
		}, nil
	}
	acceptAll := func(_ context.Context, _ []sharedintent.Row) (sharedintent.AcceptedGenerationLookup, error) {
		return func(sharedintent.AcceptanceKey) (string, bool) { return "gen-1", true }, nil
	}

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	runner := &Runner{
		IntentReader:        reader,
		LeaseManager:        &fakeLeaseManager{granted: true},
		EdgeWriter:          &fakeEdgeWriter{},
		AcceptedGen:         acceptedGenerationFixed("gen-1", true),
		AcceptedGenPrefetch: acceptAll,
		ReadinessPrefetch:   readyBehindBlocked,
		Config: RunnerConfig{
			PartitionCount:      1,
			PollInterval:        poll,
			PartitionBackoffMax: maxBackoff,
		},
	}
	runner.nowFn = func() time.Time { return now }
	ctx := context.Background()

	// Saturate every partition to T_max behind the blocked head. Each
	// cycle advances the clock past any armed delay so every cycle visits
	// (skipped visits report nothing, so a frozen clock could never
	// deepen past the first armed delay).
	for i := 0; i < 8; i++ {
		cycle := runner.runOneCycle(ctx)
		if i < 3 && cycle.PartitionsBackoffSkipped != 0 {
			t.Fatalf("warm-up cycle %d skipped %d, want 0 (grace)", i+1, cycle.PartitionsBackoffSkipped)
		}
		now = now.Add(time.Second)
	}
	// Idle partitions are unproductive (blocked rows complete nothing),
	// so the sql partition must be pinned at T_max now.
	pinned := false
	for _, entry := range runner.BackoffState() {
		if entry.Domain == domain && entry.AtMax {
			pinned = true
		}
	}
	if !pinned {
		t.Fatal("sql partition should be pinned at T_max after warm-up")
	}

	// Inject the ready row behind the blocked head and step the clock.
	readyRow := selectionTestRow(domain, "scope-ready", "unit-ready", "gen-1", 100)
	reader.mu.Lock()
	reader.intents = append(reader.intents, readyRow)
	reader.mu.Unlock()
	injectedAt := now

	var pickupAt time.Time
	for step := 0; step < 20; step++ {
		now = now.Add(poll)
		runner.runOneCycle(ctx)
		reader.mu.Lock()
		marked := false
		for _, id := range reader.marked {
			if id == readyRow.IntentID {
				marked = true
				break
			}
		}
		reader.mu.Unlock()
		if marked {
			pickupAt = now
			break
		}
	}
	if pickupAt.IsZero() {
		t.Fatal("ready row behind the blocked head was never picked up")
	}
	if pickupAt.Sub(injectedAt) > maxBackoff {
		t.Fatalf("pickup took %v, want within T_max %v", pickupAt.Sub(injectedAt), maxBackoff)
	}
	// The blocked head stays pending: only the ready row drained.
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.marked) != 1 {
		t.Fatalf("marked intents = %d, want 1 (only the ready row)", len(reader.marked))
	}
}

// TestRunnerConcurrentCycleBacksOff proves the sequential and concurrent
// cycle paths share one backoff state: 8 workers over 88 cells visit
// through grace, then skip together.
func TestRunnerConcurrentCycleBacksOff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	leases := &fakeLeaseManager{granted: true}
	runner := &Runner{
		IntentReader: &fakeSharedIntentReader{},
		LeaseManager: leases,
		EdgeWriter:   &fakeEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: RunnerConfig{
			PartitionCount: 8,
			PollInterval:   100 * time.Millisecond,
			Workers:        8,
		},
	}
	runner.nowFn = func() time.Time { return now }
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		cycle := runner.runOneCycle(ctx)
		if cycle.PartitionsVisited != 88 || cycle.PartitionsBackoffSkipped != 0 {
			t.Fatalf("cycle %d = visited %d skipped %d, want 88/0", i+1, cycle.PartitionsVisited, cycle.PartitionsBackoffSkipped)
		}
	}
	cycle4 := runner.runOneCycle(ctx)
	if cycle4.PartitionsVisited != 0 || cycle4.PartitionsBackoffSkipped != 88 {
		t.Fatalf("cycle 4 = visited %d skipped %d, want 0/88", cycle4.PartitionsVisited, cycle4.PartitionsBackoffSkipped)
	}
	if got := leaseClaims(leases); got != 88*3 {
		t.Fatalf("claims = %d, want %d", got, 88*3)
	}
	if state := runner.BackoffState(); len(state) != 88 {
		t.Fatalf("BackoffState has %d entries, want 88", len(state))
	}
}

// TestRunnerBackoffStateEmptyWithoutVisits proves the debug surface is
// safe on a fresh runner: no visits, no entries, no panic.
func TestRunnerBackoffStateEmptyWithoutVisits(t *testing.T) {
	t.Parallel()

	runner := &Runner{}
	if state := runner.BackoffState(); len(state) != 0 {
		t.Fatalf("fresh BackoffState has %d entries, want 0", len(state))
	}
}

// TestLoadConfigPartitionBackoffMax proves the T_max default, env
// override, and 5-minute hard cap.
func TestLoadConfigPartitionBackoffMax(t *testing.T) {
	t.Parallel()

	if got := LoadConfig(func(string) string { return "" }).backoffMax(); got != 30*time.Second {
		t.Fatalf("default backoffMax = %v, want 30s", got)
	}
	getenv := func(key string) string {
		if key == "ESHU_SHARED_PROJECTION_PARTITION_BACKOFF_MAX" {
			return "45s"
		}
		return ""
	}
	if got := LoadConfig(getenv).backoffMax(); got != 45*time.Second {
		t.Fatalf("configured backoffMax = %v, want 45s", got)
	}
	capped := func(key string) string {
		if key == "ESHU_SHARED_PROJECTION_PARTITION_BACKOFF_MAX" {
			return "10m"
		}
		return ""
	}
	if got := LoadConfig(capped).backoffMax(); got != 5*time.Minute {
		t.Fatalf("capped backoffMax = %v, want 5m", got)
	}
	bogus := func(key string) string {
		if key == "ESHU_SHARED_PROJECTION_PARTITION_BACKOFF_MAX" {
			return "bogus"
		}
		return ""
	}
	if got := LoadConfig(bogus).backoffMax(); got != 30*time.Second {
		t.Fatalf("bogus backoffMax = %v, want 30s default", got)
	}
}
