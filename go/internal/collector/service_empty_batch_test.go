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
		AfterBatchDrained: func(context.Context) error {
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
	service := Service{
		Source: &stubSource{
			empty: func() {
				cancel()
			},
		},
		Committer:              &stubCommitter{},
		PollInterval:           time.Millisecond,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(context.Context) error {
			hookCalls++
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := hookCalls, 1; got != want {
		t.Fatalf("AfterBatchDrained() calls = %d, want %d", got, want)
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
// blast radius by measuring the observable — the drain count — rather than
// reasoning about the two latch flags, which is easy to get wrong from the source.
//
// AfterEmptyBatchDrained is easy to misread as recurring, because
// emptyDrainObserved is cleared on every successful commit (service.go:265). It
// does not recur: the same commit sets committedSinceDrain (service.go:264), so
// the next exhausted batch drains through the committedSinceDrain leg whether or
// not the escape is enabled, and the escape leg is decisive only in the initial
// state before any commit. Drains DO recur once per commit-to-idle cycle; that
// recurrence belongs to committedSinceDrain and is present with the escape off.
//
// Running the same script with the escape on and off isolates the escape's own
// contribution: the delta must be exactly one drain for the whole process, no
// matter how many commit-to-idle cycles the script contains.
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
			AfterBatchDrained: func(context.Context) error {
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

func TestServiceRunCallsEmptyBatchDrainHookOnceWhileIdle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emptyPolls := 0
	hookCalls := 0
	service := Service{
		Source: &stubSource{
			empty: func() {
				emptyPolls++
				if emptyPolls == 2 {
					cancel()
				}
			},
		},
		Committer:              &stubCommitter{},
		PollInterval:           time.Millisecond,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(context.Context) error {
			hookCalls++
			return nil
		},
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := hookCalls, 1; got != want {
		t.Fatalf("AfterBatchDrained() calls = %d, want %d", got, want)
	}
}
