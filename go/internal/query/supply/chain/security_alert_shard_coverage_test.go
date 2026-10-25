// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
)

const shardCoverageBase = "sa-target-org-sharded"

// shardCoverageConfig is one org target of 150 example-org repositories, the
// requested example-org/example-repo among them, so it is collected as shards
// (#9019).
func shardCoverageConfig(t *testing.T) (string, []shard.Leaf) {
	t.Helper()
	allowed := []string{"example-org/example-repo"}
	for i := range 149 {
		allowed = append(allowed, fmt.Sprintf("example-org/repo-%03d", i))
	}
	raw, err := json.Marshal(map[string]any{"targets": []any{map[string]any{
		"provider": "github_dependabot", "scope": "org", "scope_id": shardCoverageBase,
		"organization": "example-org", "allowed_repositories": allowed,
	}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	leaves := shard.Partition(shardCoverageBase, "example-org", allowed)
	if len(leaves) < 2 {
		t.Fatalf("150 repositories gave %d shards", len(leaves))
	}
	return string(raw), leaves
}

func TestSecurityAlertDecoderExpandsAShardedOrgTargetIntoItsShards(t *testing.T) {
	t.Parallel()

	raw, leaves := shardCoverageConfig(t)
	targets := decodeSecurityAlertTargets([]string{raw})
	ids := make([]string, 0, len(leaves))
	for _, leaf := range leaves {
		ids = append(ids, leaf.ScopeID)
	}
	if got := securityAlertTargetScopeIDs(targets, nil, false); !slices.Equal(got, ids) {
		t.Fatalf("unanchored scope ids = %v, want every shard %v", got, ids)
	}
	covering, _ := shard.For(leaves, "example-org/example-repo")
	if got := securityAlertTargetScopeIDs(targets, []string{"example-org/example-repo"}, true); !slices.Equal(got, []string{covering.ScopeID}) {
		t.Fatalf("anchored scope ids = %v, want only the requested repository's shard %q", got, covering.ScopeID)
	}
	if got := securityAlertTargetScopeIDs(targets, []string{"example-org/not-allowlisted"}, true); len(got) != 0 {
		t.Fatalf("a repository outside the allowlist is covered by %v", got)
	}
	if slices.Contains(securityAlertTargetScopeIDs(targets, nil, false), shardCoverageBase) {
		t.Fatal("a sharded target still lists its base scope, which is retired")
	}
}

func TestSecurityAlertDecoderKeepsTheBaseScopeOfAnUnshardedOrgTarget(t *testing.T) {
	t.Parallel()

	targets := decodeSecurityAlertTargets([]string{coverageTestCoveringOrgTarget})
	if got := securityAlertTargetScopeIDs(targets, nil, false); !slices.Equal(got, []string{"sa-target-org-covering"}) {
		t.Fatalf("scope ids = %v, want the configured scope", got)
	}
}

// TestSecurityAlertEmptyAnswerOnAShardedTargetReadsTheCoveringShard extends the
// #8943 answer table to shards: an anchored read is decided by the requested
// repository's own shard, so another shard's failure or unread repositories
// never change it, and the anchored state read asks for that shard only. The
// response never names a shard scope id.
func TestSecurityAlertEmptyAnswerOnAShardedTargetReadsTheCoveringShard(t *testing.T) {
	t.Parallel()

	raw, leaves := shardCoverageConfig(t)
	covering, _ := shard.For(leaves, "example-org/example-repo")
	var other string
	for _, leaf := range leaves {
		if leaf.ScopeID != covering.ScopeID {
			other = leaf.ScopeID
		}
	}
	committed := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	unread := committed
	unread.UnreadTotal, unread.UnreadReasons = 1, []string{"org_repository_read_failed"}
	unread.UnreadRequested = map[string]string{"example-org/example-repo": "org_repository_read_failed"}
	cases := []struct {
		name   string
		states map[string]SecurityAlertTargetState
		want   coverageWant
	}{
		{
			name:   "covering shard read in full, another shard failed",
			states: map[string]SecurityAlertTargetState{covering.ScopeID: committed, other: terminalState("auth_denied", false)},
			want:   coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
		},
		{
			name:   "covering shard left the repository unread",
			states: map[string]SecurityAlertTargetState{covering.ScopeID: unread, other: committed},
			want:   coverageWant{"target_incomplete", []string{"org_repository_read_failed"}, "derived", "fresh", "", ""},
		},
		{
			name:   "covering shard never committed",
			states: map[string]SecurityAlertTargetState{other: committed},
			want:   coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", "missing_collector_completion", ""},
		},
	}
	for _, tc := range cases {
		for route, target := range coverageRoutes(coverageTestRepositoryID) {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				t.Parallel()
				store := &fakeSecurityAlertCollectionState{configs: []string{raw}, states: tc.states}
				code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
				if code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body = %s", code, body)
				}
				assertCoverage(t, resp, tc.want)
				assertCoverageInvariant(t, resp)
				if strings.Contains(body, shard.Marker) || strings.Contains(body, shardCoverageBase) {
					t.Fatalf("response names a target scope: %s", body)
				}
				if got := store.scopeIDs(); got != covering.ScopeID {
					t.Fatalf("anchored state read asked for %q, want only the covering shard %q", got, covering.ScopeID)
				}
			})
		}
	}
}

// TestSecurityAlertUnanchoredEmptyAnswerOnAShardedTargetTakesEveryShard: an
// unanchored read is complete only when every shard is, and lists each
// shard's reasons.
func TestSecurityAlertUnanchoredEmptyAnswerOnAShardedTargetTakesEveryShard(t *testing.T) {
	t.Parallel()

	raw, leaves := shardCoverageConfig(t)
	committed := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	partial := committed
	partial.UnreadTotal, partial.UnreadReasons = 2, []string{"org_repository_read_rate_limited"}
	states := map[string]SecurityAlertTargetState{}
	for _, leaf := range leaves {
		states[leaf.ScopeID] = committed
	}
	const unanchored = "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001"
	store := &fakeSecurityAlertCollectionState{configs: []string{raw}, states: states}
	_, _, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), unanchored)
	assertCoverage(t, resp, coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"})
	if got := strings.Count(store.scopeIDs(), shard.Marker); got != len(leaves) {
		t.Fatalf("unanchored state read asked for %d shards (%s), want %d", got, store.scopeIDs(), len(leaves))
	}

	states[leaves[len(leaves)-1].ScopeID] = partial
	store = &fakeSecurityAlertCollectionState{configs: []string{raw}, states: states}
	code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), unanchored)
	if code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", code, body)
	}
	assertCoverage(t, resp, coverageWant{"target_incomplete", []string{"org_repository_read_rate_limited"}, "derived", "fresh", "", ""})
}

// TestSecurityAlertEmptyAnswerOnARetiredActiveGenerationIsNotComplete is the
// #9019 review F1 regression. A scope whose active generation is a retirement
// (a merged-back parent, or any shard name planned again after it was
// retired) has no data until its next collection activates. Its empty answer
// must read as still collecting, never complete, exact, and fresh, on anchored
// and unanchored reads alike. A terminal failure on top reads as a terminal
// failure with nothing committed.
func TestSecurityAlertEmptyAnswerOnARetiredActiveGenerationIsNotComplete(t *testing.T) {
	t.Parallel()

	retired := SecurityAlertTargetState{LatestWorkItemStatus: "pending", CommittedGeneration: true, Retired: true}
	retrying := retired
	retrying.LatestWorkItemStatus, retrying.LatestFailureClass = "retrying", "rate_limited"
	collecting := coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", "missing_collector_completion", ""}
	for name, state := range map[string]SecurityAlertTargetState{"pending": retired, "retrying": retrying} {
		for route, target := range coverageRoutes(coverageTestRepositoryID) {
			t.Run(name+"/"+route, func(t *testing.T) {
				t.Parallel()
				store := &fakeSecurityAlertCollectionState{
					configs: []string{coverageTestCoveringOrgTarget},
					states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": state},
				}
				_, _, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
				assertCoverage(t, resp, collecting)
				assertCoverageInvariant(t, resp)
			})
		}
		t.Run(name+"/unanchored", func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{
				configs: []string{coverageTestCoveringOrgTarget},
				states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": state},
			}
			_, _, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0),
				"/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001")
			assertCoverage(t, resp, collecting)
		})
	}
	terminal := retired
	terminal.LatestWorkItemStatus, terminal.LatestFailureClass, terminal.LatestFailureTerminal = "failed_terminal", "auth_denied", true
	if rank, verdict := classifySecurityAlertTarget(terminal, true); rank != 1 || verdict.collectorState != securityAlertCollectorStateFailedTerminal {
		t.Fatalf("retired + terminal = rank %d %q, want 1 failed_terminal (nothing committed)", rank, verdict.collectorState)
	}
}

// TestSecurityAlertUncollectedSlugIsJudgedPerShard is the #8838 x #9019
// interaction: the target set check runs against the shard scope that covers
// the requested repository. When that shard's latest committed generation did
// not collect the repository (it joined the allowlist after that generation),
// the anchored empty answer reads as collecting. A slug listed as uncollected
// by a shard that does not cover it changes nothing.
func TestSecurityAlertUncollectedSlugIsJudgedPerShard(t *testing.T) {
	t.Parallel()

	raw, leaves := shardCoverageConfig(t)
	covering, _ := shard.For(leaves, "example-org/example-repo")
	var other string
	for _, leaf := range leaves {
		if leaf.ScopeID != covering.ScopeID {
			other = leaf.ScopeID
		}
	}
	committed := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	missing := committed
	missing.UncollectedRequested = []string{"example-org/example-repo"}
	complete := coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"}
	collecting := coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", "missing_collector_completion", ""}
	for name, tc := range map[string]struct {
		states map[string]SecurityAlertTargetState
		want   coverageWant
	}{
		"covering shard missed the slug":       {map[string]SecurityAlertTargetState{covering.ScopeID: missing, other: committed}, collecting},
		"another shard lists it, not covering": {map[string]SecurityAlertTargetState{covering.ScopeID: committed, other: missing}, complete},
	} {
		for route, target := range coverageRoutes(coverageTestRepositoryID) {
			t.Run(name+"/"+route, func(t *testing.T) {
				t.Parallel()
				store := &fakeSecurityAlertCollectionState{configs: []string{raw}, states: tc.states}
				_, _, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
				assertCoverage(t, resp, tc.want)
			})
		}
	}
}
