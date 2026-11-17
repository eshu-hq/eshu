// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestNVDBaselineSettlementDropsAStaleStashOnTheNextDecision pins that a retry
// starts a new decision: an earlier attempt whose writer stashed a settlement
// but never committed (the transaction failed after the writer) must not make
// the retry's commit hook count a rotation or log a chunk the retry did not
// commit (eshu-hq/eshu#10810).
func TestNVDBaselineSettlementDropsAStaleStashOnTheNextDecision(t *testing.T) {
	t.Parallel()

	policy, reader, logs := baselineSeedPolicy(t)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	if err := armedBaselineWriter(t, policy, item)(context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("writer error = %v", err)
	}
	// The retry decides a held verdict (no snapshot): nothing is armed.
	decision, err := policy.CommitPolicy(item, testObservedAt(), []facts.Envelope{})
	if err != nil || decision.StateWriter != nil {
		t.Fatalf("held retry = (%+v, %v), want a handled hold with no writer", decision, err)
	}
	policy.SettleClaimCommitted(context.Background(), item)
	if got := seedRotationCounts(t, reader); got["forward"] != 0 {
		t.Fatalf("rotation counts = %v, want the stale stash dropped by the retry's decision", got)
	}
	if strings.Contains(logs.String(), "nvd baseline chunk committed") {
		t.Fatal("a stale stash logged a committed chunk the retry never committed")
	}
}

// TestNVDBaselineSettlementDropsAStaleStashOnAnUnchangedClaim covers the path
// that runs no policy decision: an unchanged result still reaches the commit
// hook, so a stash an earlier failed attempt left behind must be dropped when
// the claim starts, not settled as a commit.
func TestNVDBaselineSettlementDropsAStaleStashOnAnUnchangedClaim(t *testing.T) {
	t.Parallel()

	policy, reader, logs := baselineSeedPolicy(t)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	runner := &recordingRunner{result: baseResult(item, sdkcollector.ResultUnchanged)}
	source := mustNewSourceWithPolicy(t, runner, CombineClaimCommitPolicies(policy))
	if err := armedBaselineWriter(t, policy, item)(context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("writer error = %v", err)
	}
	if _, _, err := source.NextClaimed(context.Background(), item); err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v", err)
	}
	if got := seedRotationCounts(t, reader); got["forward"] != 0 {
		t.Fatalf("rotation counts = %v, want the stale stash dropped when the unchanged claim started", got)
	}
	if strings.Contains(logs.String(), "nvd baseline chunk committed") {
		t.Fatal("an unchanged claim logged a committed chunk from a stale stash")
	}
}

func mustNewSourceWithPolicy(t *testing.T, runner Runner, policy ClaimCommitPolicy) *Source {
	t.Helper()
	source, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              runner,
		Clock:               testObservedAt,
		ClaimCommitPolicy:   policy,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	return source
}
