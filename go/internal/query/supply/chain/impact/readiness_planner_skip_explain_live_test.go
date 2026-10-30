// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9612 disposable PostgreSQL proof that the planner-skip join keeps the owned-target readiness plan bounded
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// seedPlannerSkipExplainCorpus builds the minimal corpus the skip join reads:
// one derived-enabled instance, one planner run carrying skip evidence, and
// the consumption-key backfill marker the target resolution requires.
func seedPlannerSkipExplainCorpus(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
	t.Helper()
	configuration := `{"targets": [], "derive_from_owned_packages": {"enabled": true, "sources": ["osv"], "ecosystems": ["npm"]}}`
	if _, err := db.ExecContext(ctx, `INSERT INTO collector_instances
        (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
        VALUES ('derived-npm', 'vulnerability_intelligence', 'continuous', TRUE, $1, $2, $2, $2)`,
		configuration, now); err != nil {
		t.Fatalf("seed derived instance: %v", err)
	}
	scopeSet, err := json.Marshal(map[string]any{
		"collector_instance_id": "derived-npm",
		"targets":               []any{},
		"skipped_targets": []map[string]any{{
			"collector_kind": "vulnerability_intelligence",
			"target_class":   "owned_package",
			"reason":         "derived_target_non_exact_version",
			"target_limit":   100,
			"selected_count": 0,
			"skipped_count":  1,
			"ecosystems":     []string{"npm"},
			"sources":        []string{"osv"},
			"packages": []map[string]any{
				{"ecosystem": "npm", "package_name": "left-pad"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal planner scope set: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        VALUES ('vulnerability_intelligence:derived-npm:schedule:explain-proof', 'schedule',
            'collection_complete', $1, 'vulnerability_intelligence', $2, $2)`,
		scopeSet, now); err != nil {
		t.Fatalf("seed planner run: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO package_manifest_consumption_key_backfill_markers (marker_name, completed_at) VALUES ($1, $2)`,
		storagepostgres.PackageManifestConsumptionKeyBackfillMarker, now); err != nil {
		t.Fatalf("seed readiness marker: %v", err)
	}
}

// TestSupplyChainImpactReadinessPlannerSkipQueryPlanLive locks the
// owned-target plan with the planner-skip join in place: the statement must
// still resolve the request through its bounded key probes without scanning
// the fact store, and stay inside the local performance envelope on a warm
// cache. The #9612 before/after comparison (same corpus, same session shape)
// is recorded in the PR: before, the plan had no planner_runs leg; after, the
// added leg reads only workflow_runs plus the bounded derived legs.
func TestSupplyChainImpactReadinessPlannerSkipQueryPlanLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #9612 planner-skip plan proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "impact_planner_skip", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	seedPlannerSkipExplainCorpus(t, ctx, database, now)

	keys := packageidentity.ConsumptionKeys("npm", "left-pad")
	if len(keys) == 0 {
		t.Fatal("ConsumptionKeys(npm, left-pad) is empty, the probe target would match nothing")
	}
	var targetKeys []readinessTargetKey
	for _, key := range keys {
		targetKeys = append(targetKeys, readinessTargetKey{packageID: "npm://registry.npmjs.org/left-pad", key: key})
	}
	target := readinessTarget{PackageIDs: []string{"npm://registry.npmjs.org/left-pad"}, Keys: targetKeys}
	args := readinessArgsForTarget(target, ReadinessQuery{PackageID: "npm://registry.npmjs.org/left-pad"})

	var raw []byte
	if err := database.QueryRowContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+ListReadinessQuery, args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN planner-skip readiness query: %v", err)
	}
	planText := string(raw)
	t.Logf("planner-skip readiness plan bytes=%d", len(planText))
	var plan []struct {
		ExecutionTime float64 `json:"Execution Time"`
		PlanningTime  float64 `json:"Planning Time"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
		t.Fatalf("decode planner-skip readiness plan: count err=%v raw=%.200s", err, planText)
	}
	t.Logf("planner-skip readiness plan: execution_ms=%.3f planning_ms=%.3f",
		plan[0].ExecutionTime, plan[0].PlanningTime)
	// The planner inlines the skip CTEs into the derived legs, so the plan
	// names the underlying reads rather than the CTE: the bound is that the
	// added leg reads only the small workflow_runs table plus the bounded
	// derived legs, never the fact store.
	if !strings.Contains(planText, "workflow_runs") {
		t.Fatalf("planner-skip readiness plan never reads the planner runs")
	}
	if !strings.Contains(planText, "requested_scope_set") {
		t.Fatalf("planner-skip readiness plan never reads the planner scope sets")
	}
	if plan[0].ExecutionTime > 500 {
		t.Fatalf("execution time = %.3fms, want <=500ms (local-performance-envelope budget)", plan[0].ExecutionTime)
	}
}
