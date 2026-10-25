// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alert

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// The tests in this file pin the planner-visible half of #9019: an org target
// with more than 100 same-org allowlisted repositories is planned as several
// bounded shard scopes. They assert only what the work items expose (scope ids,
// their count, and their stability), so they hold for any shard function that
// meets the acceptance criteria. Shard membership (exactly one shard per
// repository, at most 100 per shard) is asserted where the shard function
// lives, once its placement is decided on #9019.

const (
	shardTestBaseScopeID = "security-alert:github-org:acme"
	shardTestOrg         = "acme"
	// shardTestScopeIDSuffixBound is the most a shard suffix may add to the
	// operator's scope_id: a bounded token, never a repository name.
	shardTestScopeIDSuffixBound = 48
)

func shardTestRepositories(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fmt.Sprintf("%s/service-%04d", shardTestOrg, i))
	}
	return out
}

func shardTestConfig(t *testing.T, allowed []string) string {
	t.Helper()
	target := map[string]any{
		"provider":             "github_dependabot",
		"scope":                "org",
		"scope_id":             shardTestBaseScopeID,
		"organization":         shardTestOrg,
		"token_env":            "GITHUB_TOKEN",
		"allowed_repositories": allowed,
		"max_pages":            1,
	}
	raw, err := json.Marshal(map[string]any{"targets": []any{target}})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return string(raw)
}

func planShardScopeIDs(t *testing.T, allowed []string) []string {
	t.Helper()
	observedAt := time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC)
	_, items, err := (WorkPlanner{}).PlanSecurityAlertWork(t.Context(), PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:     "security-alert-primary",
			CollectorKind:  scope.CollectorSecurityAlert,
			Mode:           workflow.CollectorModeContinuous,
			Enabled:        true,
			ClaimsEnabled:  true,
			Configuration:  shardTestConfig(t, allowed),
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
		ObservedAt: observedAt,
		PlanKey:    "schedule-20261025T060000Z",
	})
	if err != nil {
		t.Fatalf("PlanSecurityAlertWork() error = %v, want nil", err)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ScopeID)
	}
	slices.Sort(ids)
	return ids
}

func assertBoundedShardScopeIDs(t *testing.T, ids []string, allowed []string) {
	t.Helper()
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Fatalf("shard scope ids %v are not unique", ids)
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, shardTestBaseScopeID) {
			t.Fatalf("shard scope id %q does not extend the target scope_id %q", id, shardTestBaseScopeID)
		}
		if extra := len(id) - len(shardTestBaseScopeID); extra > shardTestScopeIDSuffixBound {
			t.Fatalf("shard scope id %q adds %d bytes, want at most %d", id, extra, shardTestScopeIDSuffixBound)
		}
		suffix := strings.TrimPrefix(id, shardTestBaseScopeID)
		for _, repository := range allowed {
			_, name, _ := strings.Cut(repository, "/")
			if suffix != "" && strings.Contains(suffix, name) {
				t.Fatalf("shard scope id %q carries repository name %q", id, name)
			}
		}
	}
}

func TestPlannerKeepsOneScopeForOrgTargetAtTheFanoutCap(t *testing.T) {
	t.Parallel()

	allowed := shardTestRepositories(100)
	if got, want := planShardScopeIDs(t, allowed), []string{shardTestBaseScopeID}; !slices.Equal(got, want) {
		t.Fatalf("scope ids for 100 repositories = %v, want %v (no shard at the cap)", got, want)
	}
}

func TestPlannerShardsOrgTargetOverTheFanoutCap(t *testing.T) {
	t.Parallel()

	for _, n := range []int{101, 250, 1000} {
		allowed := shardTestRepositories(n)
		ids := planShardScopeIDs(t, allowed)
		if floor := (n + 99) / 100; len(ids) < floor {
			t.Fatalf("N=%d: %d shard scopes %v, want at least %d", n, len(ids), ids, floor)
		}
		assertBoundedShardScopeIDs(t, ids, allowed)
	}
}

func TestPlannerShardsAreDeterministicAndIgnoreAllowlistOrder(t *testing.T) {
	t.Parallel()

	allowed := shardTestRepositories(250)
	first := planShardScopeIDs(t, allowed)
	again := planShardScopeIDs(t, allowed)
	if !slices.Equal(first, again) {
		t.Fatalf("planning twice gave %v then %v, want identical shards", first, again)
	}
	reversed := slices.Clone(allowed)
	slices.Reverse(reversed)
	if got := planShardScopeIDs(t, reversed); !slices.Equal(first, got) {
		t.Fatalf("reordered allowlist gave %v, want %v", got, first)
	}
	if len(first) < 3 {
		t.Fatalf("250 repositories planned %d scopes %v, want at least 3", len(first), first)
	}
}

func TestPlannerShardsCountCaseDuplicatesAndOtherOwnersOnce(t *testing.T) {
	t.Parallel()

	allowed := shardTestRepositories(100)
	// A case-variant duplicate and repositories of another owner do not count
	// toward the same-org cap: still one scope.
	padded := append(slices.Clone(allowed), strings.ToUpper(allowed[0]), "other-org/one", "other-org/two")
	if got, want := planShardScopeIDs(t, padded), []string{shardTestBaseScopeID}; !slices.Equal(got, want) {
		t.Fatalf("scope ids = %v, want %v", got, want)
	}
	// One more distinct same-org repository crosses the cap.
	over := append(slices.Clone(padded), shardTestOrg+"/service-extra")
	if got := planShardScopeIDs(t, over); len(got) < 2 {
		t.Fatalf("101 distinct same-org repositories planned %v, want at least 2 scopes", got)
	}
}

func TestPlannerKeepsOneScopeWhenNoAllowlistedRepositoryIsSameOrg(t *testing.T) {
	t.Parallel()

	allowed := make([]string, 0, 150)
	for i := range 150 {
		allowed = append(allowed, fmt.Sprintf("other-org/service-%04d", i))
	}
	if got, want := planShardScopeIDs(t, allowed), []string{shardTestBaseScopeID}; !slices.Equal(got, want) {
		t.Fatalf("scope ids = %v, want %v (nothing same-org to shard)", got, want)
	}
}

func TestPlannerShardAddingOneRepositoryKeepsEveryExistingShard(t *testing.T) {
	t.Parallel()

	allowed := shardTestRepositories(250)
	before := planShardScopeIDs(t, allowed)
	for i := range 20 {
		after := planShardScopeIDs(t, append(slices.Clone(allowed), fmt.Sprintf("%s/added-%02d", shardTestOrg, i)))
		for _, id := range before {
			if !slices.Contains(after, id) {
				t.Fatalf("adding one repository dropped shard %q: before %v, after %v", id, before, after)
			}
		}
		if len(after) > len(before)+1 {
			t.Fatalf("adding one repository added %d shards, want at most 1", len(after)-len(before))
		}
	}
}
