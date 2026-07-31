// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestServiceRunSkipsAfterBatchDrainedOnEmptyBatchByDefault(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hookCalls := 0
	service := Service{
		Source: &stubSource{
			empty: func() {
				cancel()
			},
		},
		Committer:    &stubCommitter{},
		PollInterval: time.Millisecond,
		AfterBatchDrained: func(context.Context, bool) error {
			hookCalls++
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := hookCalls; got != 0 {
		t.Fatalf("AfterBatchDrained() calls = %d, want 0", got)
	}
}

func TestServiceRunCallsAfterBatchDrainedForConfiguredEmptyBatch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hookCalls := 0
	hasCommittedValues := []bool{}
	service := Service{
		Source: &stubSource{
			empty: func() {
				cancel()
			},
		},
		Committer:              &stubCommitter{},
		PollInterval:           time.Millisecond,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(_ context.Context, hasCommitted bool) error {
			hookCalls++
			hasCommittedValues = append(hasCommittedValues, hasCommitted)
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := hookCalls, 1; got != want {
		t.Fatalf("AfterBatchDrained() calls = %d, want %d", got, want)
	}
	if got, want := hasCommittedValues, []bool{false}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("AfterBatchDrained() hasCommitted values = %v, want %v (the never-committed escape must report hasCommitted=false so callers never open a barrier epoch on its behalf)", got, want)
	}
}

// scriptedPollSource replays a fixed script of poll outcomes — true yields a
// generation to commit, false reports an exhausted batch — then cancels the run.
type scriptedPollSource struct {
	script []bool
	index  int
	cancel context.CancelFunc
}

func (s *scriptedPollSource) Next(context.Context) (CollectedGeneration, bool, error) {
	if s.index >= len(s.script) {
		s.cancel()
		return CollectedGeneration{}, false, nil
	}
	commit := s.script[s.index]
	s.index++
	if !commit {
		return CollectedGeneration{}, false, nil
	}
	stream := make(chan facts.Envelope)
	close(stream)
	return CollectedGeneration{Facts: stream}, true, nil
}

// TestServiceRunEmptyBatchEscapeAddsExactlyOneDrainPerProcess pins the escape's
// blast radius, for a shard that DOES eventually commit, by measuring the
// observable — the drain count — rather than reasoning about the latch
// flags, which is easy to get wrong from the source.
//
// The escape leg fires while `everCommitted` is false (service.go:226) and
// `everCommitted` latches true permanently on this shard's first commit
// (service.go:273) — it is never cleared again for the rest of the process.
// So for a script that commits at least once, the escape leg can matter only
// during the idle polls before that first commit; every commit-to-idle cycle
// after it is driven by `committedSinceDrain` alone, on or off. (A shard that
// never commits at all is a different case — see
// TestServiceRunCallsEmptyBatchDrainHookOnEveryIdlePollForANeverCommittingShard,
// which pins the opposite behavior: the escape recurs on every idle poll for
// as long as `everCommitted` stays false.)
//
// Running the same script with the escape on and off isolates the escape's own
// contribution: the delta must be exactly one drain for the whole process, no
// matter how many commit-to-idle cycles the script contains, because this
// script's leading idle poll is the only one that happens before any commit.
func TestServiceRunEmptyBatchEscapeAddsExactlyOneDrainPerProcess(t *testing.T) {
	t.Parallel()

	const cycles = 10
	// One idle poll before any commit (the only state the escape leg decides),
	// then repeated commit-to-idle cycles that the committedSinceDrain leg drives.
	script := []bool{false}
	for range cycles {
		script = append(script, true, false, false)
	}

	drainsWithEscape := func(escape bool) int {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		hookCalls := 0
		service := Service{
			Source: &scriptedPollSource{
				script: append([]bool(nil), script...),
				cancel: cancel,
			},
			Committer:              &stubCommitter{},
			PollInterval:           time.Microsecond,
			AfterEmptyBatchDrained: escape,
			AfterBatchDrained: func(context.Context, bool) error {
				hookCalls++
				return nil
			},
		}
		if err := service.Run(ctx); err != nil {
			t.Fatalf("Run(escape=%t) error = %v, want nil", escape, err)
		}
		return hookCalls
	}

	withEscape := drainsWithEscape(true)
	withoutEscape := drainsWithEscape(false)

	// The escape-off arm proves the recurrence is committedSinceDrain's: one
	// drain per commit-to-idle cycle, with none for the leading idle poll.
	if withoutEscape != cycles {
		t.Fatalf("drains without escape = %d, want %d (one per commit-to-idle cycle)",
			withoutEscape, cycles)
	}
	if got := withEscape - withoutEscape; got != 1 {
		t.Fatalf("drains added by AfterEmptyBatchDrained = %d (%d with, %d without), want exactly 1 per process",
			got, withEscape, withoutEscape)
	}
}

// TestServiceRunCallsEmptyBatchDrainHookOnEveryIdlePollForANeverCommittingShard
// reproduces #5852: a sharded ingester shard that owns no repositories never
// commits a generation, so a latch that only re-arms on commit fires the
// escape drain hook exactly once (at startup) and then never again. That
// drains the fleet's deferred-maintenance barrier for its first epoch and
// then permanently strands every later one, because
// waitDeferredMaintenanceBarrierCompletion
// (go/internal/storage/postgres/deferred_maintenance_barrier.go) requires a
// fresh arrival from every shard, including this one, each epoch, and has no
// arrival deadline.
//
// Before the fix, this test failed: emptyDrainObserved latched true after the
// first drain and nothing but a commit ever cleared it, so hookCalls stayed
// at 1 no matter how many idle polls followed. The fix re-arms the escape on
// "has this shard ever committed," not on "did the last drain happen" — a
// shard that has never committed keeps re-firing on every idle poll, one
// arrival attempt per barrier cycle, for as long as it stays empty. A shard
// that does commit is unaffected: `committedSinceDrain` takes over as soon as
// its first commit lands, exactly as before.
func TestServiceRunCallsEmptyBatchDrainHookOnEveryIdlePollForANeverCommittingShard(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// wantIdlePolls stands in for at least two fleet-barrier epochs (three
	// idle polls give a clear third data point beyond the two-epoch minimum
	// the issue asks for).
	const wantIdlePolls = 3
	emptyPolls := 0
	hookCalls := 0
	hasCommittedValues := []bool{}
	service := Service{
		Source: &stubSource{
			empty: func() {
				emptyPolls++
				if emptyPolls == wantIdlePolls {
					cancel()
				}
			},
		},
		Committer:              &stubCommitter{},
		PollInterval:           time.Millisecond,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(_ context.Context, hasCommitted bool) error {
			hookCalls++
			hasCommittedValues = append(hasCommittedValues, hasCommitted)
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := hookCalls, wantIdlePolls; got != want {
		t.Fatalf("AfterBatchDrained() calls = %d, want %d (a shard that never commits must re-arrive at the barrier every idle poll, not just once)", got, want)
	}
	for i, hasCommitted := range hasCommittedValues {
		if hasCommitted {
			t.Fatalf("AfterBatchDrained() hasCommitted[%d] = true, want false for every call on a shard that never commits (it must never be allowed to open a barrier epoch)", i)
		}
	}
}
