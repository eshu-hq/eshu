// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"
)

// TestCommitClaimFactsRetryReturnsNilWithDeterministicFactIDs proves the
// retry half on the production subject: replaying the same bytes returns nil
// both times with pairwise-identical deterministic FactIDs, so the
// boundary upsert converges instead of duplicating. Dedup itself lives in
// the production boundary's generation-finalized skip
// (errGenerationAlreadyFinalized in storage), which no stub can prove;
// this test pins the lane's half of that contract.
func TestCommitClaimFactsRetryReturnsNilWithDeterministicFactIDs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}
	resultJSON := mustCommitLaneResultJSON(t, commitLaneResult(mutation, 2))

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-retry",
		ResultJSON:     resultJSON,
	}
	ctx := context.Background()
	if err := CommitClaimFacts(ctx, store, committer, req); err != nil {
		t.Fatalf("first CommitClaimFacts() error = %v, want nil", err)
	}
	if err := CommitClaimFacts(ctx, store, committer, req); err != nil {
		t.Fatalf("retry CommitClaimFacts() error = %v, want nil", err)
	}
	if committer.calls != 2 {
		t.Fatalf("commit calls = %d, want 2: the lane never dedupes, the boundary upsert does", committer.calls)
	}
	if len(committer.recorded) != 4 {
		t.Fatalf("recorded envelopes after retry = %d, want 4 (both attempts reach the boundary)", len(committer.recorded))
	}
	for i := 0; i < 2; i++ {
		if committer.recorded[i].FactID != committer.recorded[i+2].FactID {
			t.Fatalf("replay FactID mismatch at %d: %q vs %q", i, committer.recorded[i].FactID, committer.recorded[i+2].FactID)
		}
	}
}

// TestCommitClaimEnvelopesAreDeterministic pins the deterministic FactID
// half of the retry contract: mapping the same result twice yields identical
// FactIDs, and a different stable key yields a different FactID. A random or
// time-seeded FactID fails the first half; an ID blind to fact identity
// fails the second.
func TestCommitClaimEnvelopesAreDeterministic(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	result := commitLaneResult(mutation, 2)
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-addressed",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	first, err := mapCommitClaimEnvelopes(req, result)
	if err != nil {
		t.Fatalf("mapCommitClaimEnvelopes() error = %v, want nil", err)
	}
	second, err := mapCommitClaimEnvelopes(req, result)
	if err != nil {
		t.Fatalf("mapCommitClaimEnvelopes() error = %v, want nil", err)
	}
	for i := range first {
		if first[i].FactID != second[i].FactID {
			t.Fatalf("FactID at %d is not deterministic: %q vs %q", i, first[i].FactID, second[i].FactID)
		}
	}
	changed := commitLaneResult(mutation, 2)
	changed.Facts[0].StableKey = "stable-changed"
	changed.Facts[0].SourceRef.FactKey = "stable-changed"
	remapped, err := mapCommitClaimEnvelopes(req, changed)
	if err != nil {
		t.Fatalf("mapCommitClaimEnvelopes() error = %v, want nil", err)
	}
	if remapped[0].FactID == first[0].FactID {
		t.Fatal("FactID unchanged after a stable-key change: the ID ignores fact identity")
	}
}

// TestCommitClaimFactsNeverCompletesTheClaim guards the lane's routing on
// the production subject: CommitClaimFacts commits facts and never completes
// the claim, so the caller-owned CompleteClaim step stays the only closer.
// Adding a CompleteClaim call inside CommitClaimFacts fails this.
func TestCommitClaimFactsNeverCompletesTheClaim(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-no-complete",
		ResultJSON:     mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	if committer.calls != 1 {
		t.Fatalf("commit calls = %d, want 1", committer.calls)
	}
	if store.completeCalls != 0 {
		t.Fatalf("CompleteClaim calls = %d, want 0: the commit half never completes", store.completeCalls)
	}
}
