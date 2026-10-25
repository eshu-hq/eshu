// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alert

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fakeShardScopes stands in for the Postgres read of the scopes an org target
// already has with a non-retired active generation.
type fakeShardScopes struct {
	scopes  []string
	err     error
	calls   int
	bases   []string
	planned []string
	limit   int
}

func (f *fakeShardScopes) RetirementCandidates(_ context.Context, baseScopeIDs, plannedScopeIDs []string, limit int) ([]string, error) {
	f.calls++
	f.bases = slices.Clone(baseScopeIDs)
	f.planned = slices.Clone(plannedScopeIDs)
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	return slices.Clone(f.scopes), nil
}

func planWithScopes(t *testing.T, reader ShardScopeReader, allowed []string) []workflow.WorkItem {
	t.Helper()
	observedAt := time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC)
	_, items, err := (WorkPlanner{ShardScopes: reader}).PlanSecurityAlertWork(t.Context(), shardTestRequest(t, allowed, observedAt))
	if err != nil {
		t.Fatalf("PlanSecurityAlertWork() error = %v", err)
	}
	return items
}

func shardTestRequest(t *testing.T, allowed []string, observedAt time.Time) PlanRequest {
	t.Helper()
	return PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID: "security-alert-primary", CollectorKind: scope.CollectorSecurityAlert,
			Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
			Configuration: shardTestConfig(t, allowed), LastObservedAt: observedAt, CreatedAt: observedAt, UpdatedAt: observedAt,
		},
		ObservedAt: observedAt,
		PlanKey:    "schedule-20261025T060000Z",
	}
}

func splitItems(items []workflow.WorkItem) (collect, retire []string) {
	for _, item := range items {
		if shard.IsRetirement(item.ScopeID, item.AcceptanceUnitID) {
			retire = append(retire, item.ScopeID)
			continue
		}
		collect = append(collect, item.ScopeID)
	}
	slices.Sort(collect)
	slices.Sort(retire)
	return collect, retire
}

func TestPlannerRetiresTheBaseScopeWhenATargetSplits(t *testing.T) {
	t.Parallel()

	reader := &fakeShardScopes{scopes: []string{shardTestBaseScopeID}}
	collect, retire := splitItems(planWithScopes(t, reader, shardTestRepositories(101)))
	if len(collect) != 2 || slices.Contains(collect, shardTestBaseScopeID) {
		t.Fatalf("collection scopes = %v, want two shard scopes", collect)
	}
	if !slices.Equal(retire, []string{shardTestBaseScopeID}) {
		t.Fatalf("retired scopes = %v, want the base scope", retire)
	}
	if !slices.Equal(reader.bases, []string{shardTestBaseScopeID}) || reader.limit != MaxRetirementCandidates ||
		!slices.Equal(reader.planned, collect) {
		t.Fatalf("reader called with bases %v planned %v limit %d", reader.bases, reader.planned, reader.limit)
	}
}

func TestPlannerRetiresBothShardsWhenATargetMergesBack(t *testing.T) {
	t.Parallel()

	split := planWithScopes(t, &fakeShardScopes{}, shardTestRepositories(101))
	shards, _ := splitItems(split)
	reader := &fakeShardScopes{scopes: shards}
	collect, retire := splitItems(planWithScopes(t, reader, shardTestRepositories(100)))
	if !slices.Equal(collect, []string{shardTestBaseScopeID}) {
		t.Fatalf("collection scopes = %v, want the base scope back", collect)
	}
	if !slices.Equal(retire, shards) {
		t.Fatalf("retired scopes = %v, want both shards %v", retire, shards)
	}
}

func TestPlannerRetiresNothingWhenEveryActiveScopeIsPlanned(t *testing.T) {
	t.Parallel()

	shards, _ := splitItems(planWithScopes(t, &fakeShardScopes{}, shardTestRepositories(250)))
	_, retire := splitItems(planWithScopes(t, &fakeShardScopes{scopes: shards}, shardTestRepositories(250)))
	if len(retire) != 0 {
		t.Fatalf("retired %v, want none", retire)
	}
}

func TestPlannerRetirementItemIsMarkedAndStable(t *testing.T) {
	t.Parallel()

	reader := &fakeShardScopes{scopes: []string{shardTestBaseScopeID}}
	first := planWithScopes(t, reader, shardTestRepositories(101))
	again := planWithScopes(t, reader, shardTestRepositories(101))
	if !slices.EqualFunc(first, again, func(a, b workflow.WorkItem) bool {
		return a.WorkItemID == b.WorkItemID && a.GenerationID == b.GenerationID && a.AcceptanceUnitID == b.AcceptanceUnitID
	}) {
		t.Fatal("replayed reconciliation planned different work items")
	}
	var retirement workflow.WorkItem
	for _, item := range first {
		if shard.IsRetirement(item.ScopeID, item.AcceptanceUnitID) {
			retirement = item
		}
	}
	if retirement.ScopeID != shardTestBaseScopeID || retirement.AcceptanceUnitID != shardTestBaseScopeID+shard.RetirementMarker {
		t.Fatalf("retirement item = %+v", retirement)
	}
	if !strings.HasPrefix(retirement.GenerationID, "security_alert:") || retirement.SourceRunID != retirement.GenerationID {
		t.Fatalf("retirement generation identity = %q / %q", retirement.GenerationID, retirement.SourceRunID)
	}
	// A retirement and a collection of the same scope under the same plan key
	// must not share a generation id.
	merged := planWithScopes(t, &fakeShardScopes{}, shardTestRepositories(100))
	if merged[0].GenerationID == retirement.GenerationID {
		t.Fatalf("collection and retirement of %q share generation %q", shardTestBaseScopeID, retirement.GenerationID)
	}
}

func TestPlannerSkipsTheReaderForRepositoryOnlyConfigurations(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC)
	request := shardTestRequest(t, nil, observedAt)
	request.Instance.Configuration = `{"targets":[{"provider":"github_dependabot","scope_id":"security-alert:github:acme/a",
		"repository":"acme/a","token_env":"GITHUB_TOKEN","allowed_repositories":["acme/a"]}]}`
	if _, items, err := (WorkPlanner{}).PlanSecurityAlertWork(t.Context(), request); err != nil || len(items) != 1 {
		t.Fatalf("repository-only plan with no reader = %d items, %v; want 1, nil", len(items), err)
	}
}

func TestPlannerRequiresTheReaderForOrgTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC)
	_, _, err := (WorkPlanner{}).PlanSecurityAlertWork(t.Context(), shardTestRequest(t, shardTestRepositories(3), observedAt))
	if err == nil || !strings.Contains(err.Error(), "shard scope reader") {
		t.Fatalf("error = %v, want the missing shard scope reader refusal", err)
	}
}

func TestPlannerFailsWhenTheScopeReadFails(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.October, 25, 6, 0, 0, 0, time.UTC)
	reader := &fakeShardScopes{err: errors.New("connection refused")}
	_, _, err := (WorkPlanner{ShardScopes: reader}).PlanSecurityAlertWork(t.Context(), shardTestRequest(t, shardTestRepositories(3), observedAt))
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error = %v, want the read failure", err)
	}
}

func TestPlannerBoundsRetirementsPerScan(t *testing.T) {
	t.Parallel()

	scopes := make([]string, 0, MaxRetirementCandidates+1)
	for i := range MaxRetirementCandidates + 1 {
		scopes = append(scopes, shard.ScopeID(shardTestBaseScopeID, fmt.Sprintf("%011b", i)))
	}
	_, retire := splitItems(planWithScopes(t, &fakeShardScopes{scopes: scopes}, shardTestRepositories(3)))
	if len(retire) != MaxRetirementCandidates {
		t.Fatalf("retired %d scopes, want the per-scan bound %d", len(retire), MaxRetirementCandidates)
	}
}

func TestPlannerIgnoresReaderScopesOfOtherTargets(t *testing.T) {
	t.Parallel()

	// A defensive filter: a scope that is neither the base nor a shard of a
	// configured org target is never retired, whatever the reader returns.
	reader := &fakeShardScopes{scopes: []string{"security-alert:github-org:other#shard-0", "security-alert:github:acme/x"}}
	_, retire := splitItems(planWithScopes(t, reader, shardTestRepositories(3)))
	if len(retire) != 0 {
		t.Fatalf("retired %v, want none", retire)
	}
}
