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
	if got, want := hasCommittedValues, []bool{true}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("AfterBatchDrained() hasCommitted values = %v, want %v (the never-committed escape's first-ever call in this process must report hasCommitted=true — the startupMaintenanceEscapeUsed once-latch — so a shard that owns no repositories still gets the one-time startup maintenance pass)", got, want)
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

// TestServiceRunEmptyBatchEscapeAddsJoinOnlyDrainsPerIdlePoll pins the escape's
// blast radius, for a shard that DOES eventually commit, by measuring the
// observable — the drain count and values — rather than reasoning about the
// latch flags, which is easy to get wrong from the source.
//
// The escape leg fires on EVERY idle poll while AfterEmptyBatchDrained is set
// (#10084): a shard that committed and then goes idle keeps arriving join-only
// (false) instead of falling silent, so a peer in an open epoch is never
// stranded. Only the process-first drain call may report true via the
// startupMaintenanceEscapeUsed once-latch; every later escape-driven call
// reports the real, still-false commit status. A shard that never commits at
// all is the same recurrence from the first poll — see
// TestServiceRunCallsEmptyBatchDrainHookOnEveryIdlePollForANeverCommittingShard.
//
// Running the same script with the escape on and off isolates the escape's own
// contribution: the delta equals the number of idle polls with nothing pending
// (the leading poll plus one trailing idle poll per cycle), and exactly the
// leading call plus one per commit-to-idle cycle report true.
func TestServiceRunEmptyBatchEscapeAddsJoinOnlyDrainsPerIdlePoll(t *testing.T) {
	t.Parallel()

	const cycles = 10
	// One idle poll before any commit, then repeated commit-to-idle cycles of
	// one commit poll and two idle polls: the first idle poll of each cycle
	// follows a commit (committedSinceDrain drives it), the second is a true
	// idle poll (only the escape drives it).
	script := []bool{false}
	for range cycles {
		script = append(script, true, false, false)
	}

	drainsWithEscape := func(escape bool) []bool {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var values []bool
		service := Service{
			Source: &scriptedPollSource{
				script: append([]bool(nil), script...),
				cancel: cancel,
			},
			Committer:              &stubCommitter{},
			PollInterval:           time.Microsecond,
			AfterEmptyBatchDrained: escape,
			AfterBatchDrained: func(_ context.Context, hasCommitted bool) error {
				values = append(values, hasCommitted)
				return nil
			},
		}
		if err := service.Run(ctx); err != nil {
			t.Fatalf("Run(escape=%t) error = %v, want nil", escape, err)
		}
		return values
	}

	withEscape := drainsWithEscape(true)
	withoutEscape := drainsWithEscape(false)

	// The escape-off arm proves the recurrence is committedSinceDrain's: one
	// drain per commit-to-idle cycle, with none for the leading idle poll.
	if len(withoutEscape) != cycles {
		t.Fatalf("drains without escape = %d, want %d (one per commit-to-idle cycle)",
			len(withoutEscape), cycles)
	}
	for i, v := range withoutEscape {
		if !v {
			t.Fatalf("drains without escape[%d] = false, want all true (every drain follows a commit)", i)
		}
	}
	// One leading idle drain plus two drains per cycle (commit drain, then one
	// join-only idle drain), plus the terminal post-cancel idle poll, which
	// also arrives join-only.
	if want := 2 + 2*cycles; len(withEscape) != want {
		t.Fatalf("drains with escape = %d, want %d (one per idle poll plus one per commit drain, plus terminal)",
			len(withEscape), want)
	}
	trues := 0
	for i, v := range withEscape {
		if v {
			trues++
			continue
		}
		if i == 0 {
			t.Fatal("drains with escape[0] = false, want true (the process-first drain opens the startup epoch)")
		}
	}
	// The leading escape drain plus one commit drain per cycle report true;
	// every trailing idle drain stays join-only.
	if want := 1 + cycles; trues != want {
		t.Fatalf("true drains with escape = %d, want %d (leading drain plus one per commit)",
			trues, want)
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
// Before the first fix, this test failed: emptyDrainObserved latched true
// after the first drain and nothing but a commit ever cleared it, so
// hookCalls stayed at 1 no matter how many idle polls followed. The
// `everCommitted` fix re-arms the escape on "has this shard ever committed,"
// not on "did the last drain happen" — a shard that has never committed keeps
// re-firing on every idle poll, one arrival attempt per barrier cycle, for as
// long as it stays empty. A shard that does commit is unaffected:
// `committedSinceDrain` takes over as soon as its first commit lands, exactly
// as before.
//
// The P1 follow-up adds startupMaintenanceEscapeUsed: only the FIRST of these
// recurring escape calls reports hasCommitted=true (letting this shard open
// the fleet barrier's first epoch and receive the one-time startup
// maintenance pass origin/main always ran); every later call reports false
// and stays join-only, so a quiet fleet does not reopen and rerun the
// corpus-wide maintenance pass on every idle poll.
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
	want := []bool{true, false, false}
	if len(hasCommittedValues) != len(want) {
		t.Fatalf("AfterBatchDrained() hasCommitted values = %v, want %v", hasCommittedValues, want)
	}
	for i, hasCommitted := range hasCommittedValues {
		if hasCommitted != want[i] {
			t.Fatalf("AfterBatchDrained() hasCommitted[%d] = %v, want %v (only the first-ever escape call may open a barrier epoch; every later call on a shard that never commits must stay join-only)", i, hasCommitted, want[i])
		}
	}
}
