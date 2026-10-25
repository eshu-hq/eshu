// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9019 disposable PostgreSQL 18 measurement of the security-alert planner's scan cost on 50 collector instances, each with one org target of 150 repositories (2 shards) whose scopes hold active generations, beside 8,000 unrelated scopes: one-repository repository-target planning (a different input, the floor of the planner's own cost), partition-only planning, and partition plus the Postgres retirement read, median of 15 scans; creates and drops its own database from the bootstrap schema; about 3 s on PostgreSQL 18.6 locally; timings are logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/security/alert"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/retirement"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

type noRetirements struct{}

func (noRetirements) RetirementCandidates(context.Context, []string, []string, int) ([]string, error) {
	return nil, nil
}

// TestShardPlannerScanCostLive measures what #9019 adds to one coordinator
// scan of 50 security-alert instances: the partition and the retirement read.
func TestShardPlannerScanCostLive(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'repo-scope-' || g, 'repository', 'git', 'k' || g, NULL, 'git', 'p', now(), now(), 'active', NULL, '{}'::jsonb
FROM generate_series(1, 8000) AS g`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	const instances = 50
	orgConfigs := make([]string, instances)
	wantItems := 0
	repoConfigs := make([]string, instances)
	for i := range instances {
		base := fmt.Sprintf("security-alert:github-org:org%02d", i)
		allowed := make([]string, 0, 150)
		for r := range 150 {
			allowed = append(allowed, fmt.Sprintf("org%02d/svc-%03d", i, r))
		}
		orgConfigs[i] = fmt.Sprintf(`{"targets":[{"provider":"github_dependabot","scope":"org","scope_id":%q,"organization":"org%02d","token_env":"T","allowed_repositories":%s}]}`,
			base, i, mustJSON(t, allowed))
		repoConfigs[i] = fmt.Sprintf(`{"targets":[{"provider":"github_dependabot","scope_id":"security-alert:github:org%02d/svc-000","repository":"org%02d/svc-000","token_env":"T","allowed_repositories":["org%02d/svc-000"]}]}`, i, i, i)
		leaves := shard.Partition(base, fmt.Sprintf("org%02d", i), allowed)
		wantItems += len(leaves)
		for _, leaf := range leaves {
			generation := "gen-" + leaf.ScopeID
			if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'security_alert', 'security_alert', 'k', $2, 'security_alert', 'github_dependabot', now(), now(), 'active', NULL, '{}'::jsonb)`,
				leaf.ScopeID, base); err != nil {
				t.Fatalf("seed shard scope: %v", err)
			}
			if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', now(), now(), 'active')`, generation, leaf.ScopeID); err != nil {
				t.Fatalf("seed generation: %v", err)
			}
			if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generation, leaf.ScopeID); err != nil {
				t.Fatalf("activate: %v", err)
			}
		}
	}
	if _, err := database.ExecContext(ctx, `ANALYZE ingestion_scopes`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	scan := func(planner alert.WorkPlanner, configs []string) (time.Duration, int) {
		observedAt := time.Now().UTC()
		start := time.Now()
		total := 0
		for i, config := range configs {
			_, items, err := planner.PlanSecurityAlertWork(ctx, alert.PlanRequest{
				Instance: workflow.CollectorInstance{
					InstanceID: fmt.Sprintf("inst-%02d", i), CollectorKind: scope.CollectorSecurityAlert,
					Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Configuration: config,
					LastObservedAt: observedAt, CreatedAt: observedAt, UpdatedAt: observedAt,
				},
				ObservedAt: observedAt, PlanKey: "schedule-20261025T060000Z",
			})
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			total += len(items)
		}
		return time.Since(start), total
	}
	median := func(planner alert.WorkPlanner, configs []string) (time.Duration, int) {
		var samples []time.Duration
		items := 0
		for range 15 {
			d, n := scan(planner, configs)
			samples = append(samples, d)
			items = n
		}
		slices.Sort(samples)
		return samples[len(samples)/2], items
	}
	before, beforeItems := median(alert.WorkPlanner{}, repoConfigs)
	partition, partitionItems := median(alert.WorkPlanner{ShardScopes: noRetirements{}}, orgConfigs)
	after, afterItems := median(alert.WorkPlanner{ShardScopes: retirement.NewReader(postgres.SQLDB{DB: database})}, orgConfigs)
	if afterItems != partitionItems || afterItems != wantItems {
		t.Fatalf("items: partition %d, with read %d, want %d (no retirement on a converged plan)", partitionItems, afterItems, wantItems)
	}
	t.Logf("PERF #9019 planner scan, %d instances (median of 15): one-repository repository targets %s (%d items, %.3f s; a different input, not the pre-#9019 planner on these org targets, which the #9019 review measured at 5.09 ms); partition only %s (%d items, %.3f s); partition + retirement read %s (%d items, %.3f s); read adds %s per instance",
		instances, before, beforeItems, before.Seconds(), partition, partitionItems, partition.Seconds(), after, afterItems, after.Seconds(),
		(after-partition)/instances)
}
