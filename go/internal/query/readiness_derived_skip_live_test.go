// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9612 disposable PostgreSQL proof that a planner-skipped derived package reports a skip state with the recorded reason instead of pending
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// seedPlannerSkippedRun records one vulnerability-intelligence planner run
// whose requested scope set carries skip evidence for packages the planner
// will never plan, the same JSON shape the coordinator writes.
func seedPlannerSkippedRun(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, instance, runID string, skippedTargets []map[string]any) {
	t.Helper()
	scopeSet, err := json.Marshal(map[string]any{
		"collector_instance_id": instance,
		"targets":               []any{},
		"skipped_targets":       skippedTargets,
	})
	if err != nil {
		t.Fatalf("marshal planner scope set: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        VALUES ($1, 'schedule', 'collection_complete', $2, 'vulnerability_intelligence', $3, $3)`,
		runID, scopeSet, now); err != nil {
		t.Fatalf("seed planner run %s: %v", runID, err)
	}
}

func plannerSkippedTarget(reason, ecosystem, packageName string) map[string]any {
	return map[string]any{
		"collector_kind": "vulnerability_intelligence",
		"target_class":   "owned_package",
		"reason":         reason,
		"target_limit":   100,
		"selected_count": 0,
		"skipped_count":  1,
		"ecosystems":     []string{ecosystem},
		"sources":        []string{"osv"},
		"packages": []map[string]any{
			{"ecosystem": ecosystem, "package_name": packageName},
		},
	}
}

// TestSupplyChainImpactReadinessPlannerSkippedPackageReportsSkipLive is #9612
// RED: a derived npm package the planner skipped for a non-exact version
// reports a skip state with the recorded reason, not osv:pending with
// target_incomplete.
func TestSupplyChainImpactReadinessPlannerSkippedPackageReportsSkipLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-npm", "npm")
	seedPlannerSkippedRun(t, ctx, db, now, "derived-npm",
		"vulnerability_intelligence:derived-npm:schedule:skipped-proof",
		[]map[string]any{
			plannerSkippedTarget("derived_target_non_exact_version", "npm", "left-pad"),
		})

	readiness := readDerivedReadiness(t, db, "package_id=npm://registry.npmjs.org/left-pad")
	state := onlyDerivedState(t, readiness)
	if state.FreshnessState != "skipped" || state.TerminalStatus != "skipped" {
		t.Fatalf("derived aggregate = %+v, want freshness/terminal skipped", state)
	}
	if state.UnmatchedPackageCount != 1 || state.SkippedPackageCount != 1 {
		t.Fatalf("derived aggregate = %+v, want unmatched 1 and skipped 1", state)
	}
	if readiness.State != string(impact.ReadinessStateTargetSkipped) {
		t.Fatalf("readiness state = %q, want %q", readiness.State, string(impact.ReadinessStateTargetSkipped))
	}
	if !slices.Contains(readiness.IncompleteReasons, "osv:skipped:derived_target_non_exact_version") {
		t.Fatalf("incomplete reasons = %v, want osv:skipped:derived_target_non_exact_version", readiness.IncompleteReasons)
	}
}
