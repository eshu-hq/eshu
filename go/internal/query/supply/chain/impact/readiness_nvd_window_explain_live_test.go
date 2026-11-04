// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9971 review F-4 disposable PostgreSQL EXPLAIN (ANALYZE, BUFFERS) proof for the rolling-window readiness CTEs: on a fixture with 200 historical planner runs the window leg prunes to the latest run's single window, the superseded static anchor contributes no configured candidate, and the full ListReadinessQuery stays inside the local performance envelope
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// seedNVDWindowExplainCorpus builds the F-4 fixture: one rolling-enabled
// instance with an anchor-bearing config, 200 historical planner runs plus
// the latest run (each carrying one window entry among twenty CVE
// entries), a fresh source state for the latest window, and the
// consumption-key backfill marker.
func seedNVDWindowExplainCorpus(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, windowScope string) {
	t.Helper()
	configuration := `{"targets":[
		{"source":"nvd","scope_id":"vuln-intel://nvd/anchor","modified_start":"2026-01-01T00:00:00Z","modified_end":"2026-04-01T00:00:00Z","results_per_page":50}
	],"derive_nvd_rolling_windows":{"enabled":true}}`
	if _, err := db.ExecContext(ctx, `INSERT INTO collector_instances
        (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
        VALUES ('nvd-rolling', 'vulnerability_intelligence', 'continuous', TRUE, $1, $2, $2, $2)`,
		configuration, now); err != nil {
		t.Fatalf("seed rolling instance: %v", err)
	}
	windowTarget := func(scopeID, start, end string) map[string]any {
		return map[string]any{
			"scope_id": scopeID, "source": "nvd",
			"modified_start": start, "modified_end": end, "results_per_page": 50,
			"endpoint_candidates": []string{"https://services.nvd.nist.gov/rest/json/cves/2.0"},
			"derived":             true, "target_class": "configured_direct",
		}
	}
	for run := 0; run <= 200; run++ {
		targets := []map[string]any{}
		if run == 200 {
			targets = append(targets, windowTarget(windowScope, "2026-01-01T00:00:00Z", "2026-01-03T00:00:00Z"))
		} else {
			start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(run) * 48 * time.Hour)
			targets = append(targets, windowTarget(
				fmt.Sprintf("vuln-intel://nvd/_window/%d-%d", start.UnixMicro(), start.Add(48*time.Hour).UnixMicro()),
				start.Format(time.RFC3339), start.Add(48*time.Hour).Format(time.RFC3339)))
		}
		for i := 0; i < 20; i++ {
			targets = append(targets, map[string]any{
				"source": "nvd", "scope_id": fmt.Sprintf("vuln-intel://nvd/cve-%d", i),
				"cve_ids": []string{"CVE-2026-0001"}, "results_per_page": 100,
			})
		}
		scopeSet, err := json.Marshal(map[string]any{
			"collector_instance_id": "nvd-rolling",
			"targets":               targets,
		})
		if err != nil {
			t.Fatalf("marshal scope set: %v", err)
		}
		runAt := now.Add(-time.Duration(200-run) * time.Minute)
		if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
            (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
            VALUES ($1, 'schedule', 'collection_complete', $2, 'vulnerability_intelligence', $3, $3)`,
			fmt.Sprintf("nvd-window-explain-run-%d", run), scopeSet, runAt); err != nil {
			t.Fatalf("seed planner run %d: %v", run, err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO vulnerability_source_states
        (collector_instance_id, scope_id, source, freshness_state, terminal_status, last_attempt_at, last_success_at, updated_at)
        VALUES ('nvd-rolling', $1, 'nvd', 'fresh', 'succeeded', $2, $2, $2)`, windowScope, now); err != nil {
		t.Fatalf("seed window state: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO package_manifest_consumption_key_backfill_markers (marker_name, completed_at) VALUES ($1, $2)`,
		storagepostgres.PackageManifestConsumptionKeyBackfillMarker, now); err != nil {
		t.Fatalf("seed readiness marker: %v", err)
	}
}

// TestSupplyChainImpactReadinessNVDWindowQueryPlanLive is the #9971 review
// F-4 perf cell. The pruning counts derive from the shipped
// ListReadinessQuery by truncation at a stable boundary (never a hand
// copy, with a byte-prefix guard), and the full statement runs under
// EXPLAIN (ANALYZE, BUFFERS) on the same fixture.
func TestSupplyChainImpactReadinessNVDWindowQueryPlanLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #9971 NVD window plan proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "impact_nvd_window_explain", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	windowScope := "vuln-intel://nvd/_window/1767225600000000-1767398400000000"
	seedNVDWindowExplainCorpus(t, ctx, database, now, windowScope)
	args := readinessArgsForQuery(ReadinessQuery{CVEID: "CVE-2026-0001"})

	// Pruning proof: 201 seeded runs each carry a window entry, but the
	// latest-run leg keeps exactly one; the superseded anchor adds no
	// configured candidate.
	const boundary = "\nSELECT family, fact_count, latest_observed_at"
	if strings.Count(ListReadinessQuery, boundary) != 1 {
		t.Fatalf("boundary %q found %d times in ListReadinessQuery, want exactly once", boundary, strings.Count(ListReadinessQuery, boundary))
	}
	head, _, _ := strings.Cut(ListReadinessQuery, boundary)
	if !strings.HasPrefix(ListReadinessQuery, head) || head == ListReadinessQuery {
		t.Fatal("derived head is not a strict byte-prefix of ListReadinessQuery")
	}
	derived := head + `
SELECT (SELECT COUNT(*) FROM nvd_rolling_window_scopes),
       (SELECT COUNT(*) FROM target_vulnerability_source_scopes WHERE scope_id = 'vuln-intel://nvd/anchor')`
	var windows, anchors int64
	if err := database.QueryRowContext(ctx, derived, args...).Scan(&windows, &anchors); err != nil {
		t.Fatalf("derived pruning query: %v", err)
	}
	t.Logf("nvd window pruning: rolling_window_scopes=%d anchor_candidates=%d (201 seeded runs)", windows, anchors)
	if windows != 1 {
		t.Fatalf("rolling window scopes = %d, want 1 (latest run only; historical runs pruned)", windows)
	}
	if anchors != 0 {
		t.Fatalf("anchor configured candidates = %d, want 0 with rolling windows enabled", anchors)
	}

	// Full-statement plan proof, warmed once so the asserted run reads a
	// warm cache like the planner-skip precedent.
	for attempt := 1; attempt <= 2; attempt++ {
		var raw []byte
		if err := database.QueryRowContext(ctx,
			"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+ListReadinessQuery, args...).Scan(&raw); err != nil {
			t.Fatalf("EXPLAIN readiness query (attempt %d): %v", attempt, err)
		}
		var plan []struct {
			ExecutionTime float64 `json:"Execution Time"`
			PlanningTime  float64 `json:"Planning Time"`
			Plan          struct {
				SharedHitBlocks  int64 `json:"Shared Hit Blocks"`
				SharedReadBlocks int64 `json:"Shared Read Blocks"`
			} `json:"Plan"`
		}
		if err := json.Unmarshal(raw, &plan); err != nil || len(plan) != 1 {
			t.Fatalf("decode readiness plan: count err=%v raw=%.200s", err, raw)
		}
		t.Logf("readiness plan attempt %d: execution_ms=%.3f planning_ms=%.3f shared_hit=%d shared_read=%d",
			attempt, plan[0].ExecutionTime, plan[0].PlanningTime, plan[0].Plan.SharedHitBlocks, plan[0].Plan.SharedReadBlocks)
		if !strings.Contains(string(raw), "workflow_runs") {
			t.Fatal("readiness plan never reads the planner runs the window leg expands")
		}
		if attempt == 2 && plan[0].ExecutionTime > 500 {
			t.Fatalf("execution time = %.3fms, want <=500ms (local-performance-envelope budget)", plan[0].ExecutionTime)
		}
	}
	rows := readinessResultRows(t, ctx, database, ListReadinessQuery, args...)
	if len(rows) == 0 {
		t.Fatal("full readiness query returned no family rows on the seeded fixture")
	}
}
