// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// TestCommitClaimFactsAdmitsCompleteStatus pins the single-call lane status
// disposition: a complete result carrying only the non-signaling success
// marker satellites append (complete-class, no partial flag) commits its
// facts. The marker carries no signal the lane would drop, so rejecting it
// would refuse every satellite success carrier.
func TestCommitClaimFactsAdmitsCompleteStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete, FactCount: 1}}
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-complete-status",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil for a complete-class status", err)
	}
	if committer.calls != 1 {
		t.Fatalf("commit calls = %d, want 1: a complete-class status commits", committer.calls)
	}
	if len(committer.recorded) != 1 {
		t.Fatalf("recorded envelopes = %d, want 1: facts commit under the marker", len(committer.recorded))
	}
}

// TestCommitClaimFactsRejectsSignalingStatuses pins the other half of the
// disposition: any failure, warning, or partial signal still fails closed
// with the status verdict and commits nothing, so signaled results take the
// full claim path where their statuses are recorded.
func TestCommitClaimFactsRejectsSignalingStatuses(t *testing.T) {
	t.Parallel()

	cases := map[string]sdkcollector.Status{
		"failure":          {Class: sdkcollector.StatusFailure, FailureClass: "source_unreachable"},
		"warning":          {Class: sdkcollector.StatusWarning, WarningCount: 1},
		"complete-partial": {Class: sdkcollector.StatusComplete, Partial: true},
	}
	for name, status := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
			mutation := commitLaneMutation(now)
			store := &commitLaneStore{}
			committer := &commitLaneCommitter{}

			result := commitLaneResult(mutation, 1)
			result.Statuses = []sdkcollector.Status{status}
			req := CommitClaimFactsRequest{
				Mutation:       mutation,
				Scope:          commitLaneScope(),
				Generation:     commitLaneGeneration(now),
				ComponentID:    "git-commit-lane",
				IdempotencyKey: "commit-lane-key-signaling-" + name,
				ResultJSON:     mustCommitLaneResultJSON(t, result),
			}
			err := CommitClaimFacts(context.Background(), store, committer, req)
			if err == nil {
				t.Fatalf("CommitClaimFacts() error = nil, want signaling-status rejection (%s)", name)
			}
			if !strings.Contains(err.Error(), "status") {
				t.Fatalf("CommitClaimFacts() error = %v, want it to name statuses (%s)", err, name)
			}
			if committer.calls != 0 {
				t.Fatalf("commit calls = %d, want 0 for a signaling status (%s)", committer.calls, name)
			}
		})
	}
}

// TestCommitClaimFactsCommitsWithoutStatuses pins the existing zero-status
// behavior under the new disposition: a complete result carrying no statuses
// at all still commits, unchanged.
func TestCommitClaimFactsCommitsWithoutStatuses(t *testing.T) {
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
		IdempotencyKey: "commit-lane-key-no-statuses",
		ResultJSON:     mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil for a status-free result", err)
	}
	if committer.calls != 1 {
		t.Fatalf("commit calls = %d, want 1: a status-free result commits", committer.calls)
	}
}
