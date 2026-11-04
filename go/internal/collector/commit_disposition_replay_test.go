// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// replayDispositions are the rows both services share: a refused generation was
// rolled back, so completing dead-letter replay against it would mark rows
// replayed by a generation that does not exist (#10037). An unchanged skip means
// the scope already holds the content, so it stays a completed replay.
var replayDispositions = []struct {
	name            string
	mark            func(context.Context)
	wantCompletions int
}{
	{name: "committed", mark: func(context.Context) {}, wantCompletions: 1},
	{name: "unchanged skip", mark: MarkCommitUnchangedSkip, wantCompletions: 1},
	{
		name:            "refused",
		mark:            func(ctx context.Context) { MarkCommitRefused(ctx, testCommitRefusal) },
		wantCompletions: 0,
	},
}

func TestClaimedServiceCompletesDeadLetterReplayOnlyOnALandedCommit(t *testing.T) {
	t.Parallel()

	for _, tc := range replayDispositions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
			item := testClaimedWorkItem(now)
			item.SourceSystem = "git"
			claim := testWorkflowClaim(item.WorkItemID, now)
			store := &stubClaimStore{item: item, claim: claim, found: true}
			store.heartbeat = func(context.Context, workflow.ClaimMutation) error {
				cancel()
				return nil
			}
			source := refusalTestSource(t, false, false)
			service := testClaimedService(now, claim, scope.CollectorGit, store, source, newMarkingCommitter(tc.mark, nil))
			sink := &stubGenerationDeadLetterSink{}
			service.DeadLetters = sink

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if got := store.completeCalls; got != 1 {
				t.Fatalf("complete calls = %d, want 1: a refusal still completes the claim", got)
			}
			if got := len(sink.completions); got != tc.wantCompletions {
				t.Fatalf("replay completions = %d, want %d", got, tc.wantCompletions)
			}
		})
	}
}

func TestServiceCompletesDeadLetterReplayOnlyOnALandedCommit(t *testing.T) {
	t.Parallel()

	for _, tc := range replayDispositions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scopeValue, generationValue, envelopes := testCollectedGeneration()
			sink := &stubGenerationDeadLetterSink{}
			service := Service{
				Source: &stubSource{collected: []CollectedGeneration{FactsFromSlice(scopeValue, generationValue, envelopes)}},
				Committer: &stubCommitter{commit: func(
					commitCtx context.Context, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope,
				) error {
					for range stream {
					}
					tc.mark(commitCtx)
					cancel()
					return nil
				}},
				DeadLetters:  sink,
				PollInterval: time.Millisecond,
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if got := len(sink.completions); got != tc.wantCompletions {
				t.Fatalf("replay completions = %d, want %d", got, tc.wantCompletions)
			}
		})
	}
}
