// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

func intentIDs(intents []reducer.Intent) []string {
	ids := make([]string, len(intents))
	for i, in := range intents {
		ids[i] = in.IntentID
	}
	return ids
}

// TestScheduledWorkSourceClaimDeterministicOrder proves single-claim delivery
// follows the scripted order exactly, including duplicate entries, and reports
// drained once exhausted.
func TestScheduledWorkSourceClaimDeterministicOrder(t *testing.T) {
	t.Parallel()

	schedule := []reducer.Intent{
		{IntentID: "a"}, {IntentID: "b"}, {IntentID: "a"}, {IntentID: "c"},
	}
	src := schedulereplay.NewScheduledWorkSource(schedule)

	var got []string
	for {
		intent, ok, err := src.Claim(context.Background())
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if !ok {
			break
		}
		got = append(got, intent.IntentID)
	}

	want := []string{"a", "b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("claimed %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("claim order = %v, want %v", got, want)
		}
	}
	if !src.Drained() {
		t.Fatal("source not drained after exhausting schedule")
	}
}

// TestScheduledWorkSourceClaimBatchHonorsLimitAndOrder proves the BatchWorkSource
// path returns scripted items in order, never exceeds the limit, and increments
// the batch-call counter so callers can prove the batch path actually ran.
func TestScheduledWorkSourceClaimBatchHonorsLimitAndOrder(t *testing.T) {
	t.Parallel()

	schedule := []reducer.Intent{
		{IntentID: "a"}, {IntentID: "b"}, {IntentID: "c"}, {IntentID: "d"}, {IntentID: "e"},
	}
	src := schedulereplay.NewScheduledWorkSource(schedule)

	first, err := src.ClaimBatch(context.Background(), 2)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	if got := intentIDs(first); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("first batch = %v, want [a b]", intentIDs(first))
	}

	var rest []string
	for {
		batch, err := src.ClaimBatch(context.Background(), 10)
		if err != nil {
			t.Fatalf("ClaimBatch: %v", err)
		}
		if len(batch) == 0 {
			break
		}
		rest = append(rest, intentIDs(batch)...)
	}
	if len(rest) != 3 || rest[0] != "c" || rest[2] != "e" {
		t.Fatalf("remaining batches = %v, want [c d e]", rest)
	}
	if src.ClaimBatchCalls() == 0 {
		t.Fatal("ClaimBatchCalls not tracked")
	}
}

// TestScheduledWorkSourceSatisfiesReducerInterfaces is a compile-time guard that
// the source implements both reducer claim interfaces.
func TestScheduledWorkSourceSatisfiesReducerInterfaces(t *testing.T) {
	t.Parallel()

	var _ reducer.WorkSource = (*schedulereplay.ScheduledWorkSource)(nil)
	var _ reducer.BatchWorkSource = (*schedulereplay.ScheduledWorkSource)(nil)
}
