// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"slices"
	"testing"
	"time"
)

// TestServiceCommittedThenRefusedKeepsArrivingJoinOnly pins the #10084 end
// state: a shard that committed, drained, and is then only refused keeps
// arriving at the barrier join-only (false) on every refused drain and every
// idle poll, instead of joining the documented idle class and stranding a
// peer in an open epoch. The startup once-latch is already spent by the
// landed drain, so no later arrival may report true.
func TestServiceCommittedThenRefusedKeepsArrivingJoinOnly(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		idlePolls int
	}{
		{"one idle poll", 1},
		{"two idle polls", 2},
	} {
		idlePolls := tc.idlePolls
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scopeValue, generationValue, envelopes := testCollectedGeneration()
			source := &stubSource{
				collected: []CollectedGeneration{FactsFromSlice(scopeValue, generationValue, envelopes)},
			}
			nextBatch := 0
			idle := 0
			source.empty = func() {
				if nextBatch == 0 {
					source.collected = append(source.collected, FactsFromSlice(scopeValue, generationValue, envelopes))
					nextBatch++
					return
				}
				idle++
				if idle == idlePolls {
					cancel()
				}
			}

			commits := 0
			var drainCalls []bool
			service := Service{
				Source: source,
				Committer: markingServiceCommitter(func(commitCtx context.Context) {
					commits++
					if commits == 2 {
						MarkCommitRefused(commitCtx, testCommitRefusal)
					}
				}, func() {}),
				PollInterval:           time.Millisecond,
				AfterEmptyBatchDrained: true,
				AfterBatchDrained: func(_ context.Context, hasCommitted bool) error {
					drainCalls = append(drainCalls, hasCommitted)
					return nil
				},
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if commits != 2 {
				t.Fatalf("commits = %d, want 2 (one landed, one refused)", commits)
			}
			// The landed drain reports true, then the refused batch drain
			// and each idle poll arrive join-only: [true] plus one false
			// per refused drain and per idle poll.
			want := append([]bool{true, false}, make([]bool, idlePolls-1)...)
			if !slices.Equal(drainCalls, want) {
				t.Fatalf("AfterBatchDrained hasCommitted calls = %v, want %v", drainCalls, want)
			}
		})
	}
}
