// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9971 disposable PostgreSQL API proof that readiness reports a committed rolling NVD window as covered: the planned window entry in the latest run matches the committed snapshot descriptor, while a mismatched or missing snapshot reads as nvd:committed_snapshot_missing
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// seedReadinessNVDWindowRun plans one rolling window in the latest run's
// requested_scope_set, the coordinator's plan record the readiness
// expected descriptor is built from.
func seedReadinessNVDWindowRun(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, instance, windowScope, start, end string) {
	t.Helper()
	scopeSet, err := json.Marshal(map[string]any{
		"collector_instance_id": instance,
		"targets": []map[string]any{{
			"scope_id":            windowScope,
			"source":              "nvd",
			"modified_start":      start,
			"modified_end":        end,
			"results_per_page":    50,
			"endpoint_candidates": []string{"https://services.nvd.nist.gov/rest/json/cves/2.0"},
			"derived":             true,
			"target_class":        "configured_direct",
		}},
	})
	if err != nil {
		t.Fatalf("marshal window scope set: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        VALUES ($1, 'schedule', 'collection_complete', $2, 'vulnerability_intelligence', $3, $3)`,
		"nvd-window-run", scopeSet, now); err != nil {
		t.Fatalf("seed window planner run: %v", err)
	}
}

// seedReadinessNVDWindowSnapshot commits a window-mode snapshot under the
// window scope and stamps its coverage descriptor, mirroring the collector's
// canonical descriptor for the planned bounds.
func seedReadinessNVDWindowSnapshot(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scope, instance string, startMicros, endMicros int64) {
	t.Helper()
	seedReadinessSourceSnapshotFact(t, ctx, db, now, scope, "nvd", "", instance, true)
	descriptor, err := json.Marshal(map[string]any{
		"version": "nvd.v1", "source": "nvd", "mode": "window",
		"cve_ids":                []string{},
		"modified_start_unix_us": startMicros, "modified_end_unix_us": endMicros,
		"results_per_page":    50,
		"endpoint_candidates": []string{"https://services.nvd.nist.gov/rest/json/cves/2.0"},
		"executed_endpoint":   "https://services.nvd.nist.gov/rest/json/cves/2.0",
	})
	if err != nil {
		t.Fatalf("marshal window descriptor: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records
        SET payload = JSONB_SET(payload, '{coverage_descriptor}', $2::jsonb) || '{"result_count": 1}'::jsonb
        WHERE fact_id = $1`, scope+"-snapshot", descriptor); err != nil {
		t.Fatalf("seed window descriptor: %v", err)
	}
}

// TestSupplyChainImpactReadinessRollingWindowCoveredLive is the #9971
// criterion-4 cell: a committed rolling window whose snapshot descriptor
// matches the planned window entry reads as covered (no
// nvd:committed_snapshot_missing), while a bounds mismatch or a missing
// snapshot reads as missing.
func TestSupplyChainImpactReadinessRollingWindowCoveredLive(t *testing.T) {
	windowStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	windowScope := "vuln-intel://nvd/_window/1767225600000000-1767398400000000"
	for _, tc := range []struct {
		name        string
		snapshot    bool
		mismatchEnd bool
		wantMissing bool
	}{
		{name: "committed window covered", snapshot: true, wantMissing: false},
		{name: "mismatched window missing", snapshot: true, mismatchEnd: true, wantMissing: true},
		{name: "absent snapshot missing", snapshot: false, wantMissing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, database, now := readinessScopeLiveDatabase(t)
			seedReadinessActiveAdvisoryFact(t, ctx, database, now, "CVE-2026-9227")
			seedReadinessScopeInstance(t, ctx, database, now, "nvd-rolling", []map[string]any{})
			seedReadinessNVDWindowRun(t, ctx, database, now, "nvd-rolling", windowScope,
				windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339))
			seedReadinessScopeState(t, ctx, database, now, "nvd-rolling", windowScope, "nvd", "fresh", "succeeded")
			if _, err := database.ExecContext(ctx, `UPDATE vulnerability_source_states
                SET window_start = $1, window_end = $2 WHERE collector_instance_id = 'nvd-rolling'`,
				windowStart, windowEnd); err != nil {
				t.Fatalf("seed window collection bounds: %v", err)
			}
			if tc.snapshot {
				endMicros := windowEnd.UnixMicro()
				if tc.mismatchEnd {
					endMicros += 3600 * 1000000
				}
				seedReadinessNVDWindowSnapshot(t, ctx, database, now, windowScope, "nvd-rolling", windowStart.UnixMicro(), endMicros)
			}
			readiness := readReadinessScopeAPI(t, database, "CVE-2026-9227")
			missing := slices.Contains(readiness.IncompleteReasons, "nvd:committed_snapshot_missing")
			if missing != tc.wantMissing {
				t.Fatalf("committed_snapshot_missing = %v, want %v (reasons %v)", missing, tc.wantMissing, readiness.IncompleteReasons)
			}
			found, fresh := false, false
			for _, state := range readiness.SourceStates {
				if strings.TrimSpace(state.ScopeID) == windowScope {
					found, fresh = true, state.FreshnessState == "fresh"
				}
			}
			if !found {
				t.Fatalf("window scope %q absent from source states %+v", windowScope, readiness.SourceStates)
			}
			if !fresh {
				t.Fatalf("window scope reads as %v, want fresh by success time", readiness.SourceStates)
			}
		})
	}
}

// TestSupplyChainImpactReadinessRollingWindowAnchorSupersededLive is the
// #9971 review F-1 criterion-4 cell with an anchor-bearing config: when
// derive_nvd_rolling_windows is enabled the configured static anchor is
// superseded by the rolling windows, so it must not appear as a configured
// candidate at all. With the window committed, readiness is fully
// complete (no incomplete reasons), not merely free of
// committed_snapshot_missing.
func TestSupplyChainImpactReadinessRollingWindowAnchorSupersededLive(t *testing.T) {
	ctx, database, now := readinessScopeLiveDatabase(t)
	windowStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	windowScope := "vuln-intel://nvd/_window/1767225600000000-1767398400000000"
	anchorScope := "vuln-intel://nvd/anchor"
	seedReadinessActiveAdvisoryFact(t, ctx, database, now, "CVE-2026-9227")
	configuration, err := json.Marshal(map[string]any{
		"targets": []map[string]any{{
			"source": "nvd", "scope_id": anchorScope,
			"modified_start": "2026-01-01T00:00:00Z", "modified_end": "2026-04-01T00:00:00Z",
			"results_per_page": 50,
		}},
		"derive_nvd_rolling_windows": map[string]any{"enabled": true},
	})
	if err != nil {
		t.Fatalf("marshal anchor configuration: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO collector_instances
        (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
        VALUES ($1, 'vulnerability_intelligence', 'continuous', TRUE, $2, $3, $3, $3)`,
		"nvd-rolling", configuration, now); err != nil {
		t.Fatalf("seed anchor instance: %v", err)
	}
	seedReadinessNVDWindowRun(t, ctx, database, now, "nvd-rolling", windowScope,
		windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339))
	seedReadinessScopeState(t, ctx, database, now, "nvd-rolling", windowScope, "nvd", "fresh", "succeeded")
	seedReadinessNVDWindowSnapshot(t, ctx, database, now, windowScope, "nvd-rolling", windowStart.UnixMicro(), windowEnd.UnixMicro())

	readiness := readReadinessScopeAPI(t, database, "CVE-2026-9227")
	for _, state := range readiness.SourceStates {
		if strings.TrimSpace(state.ScopeID) == anchorScope {
			t.Fatalf("superseded anchor still a configured candidate: %+v (readiness %+v)", state, readiness)
		}
	}
	if len(readiness.IncompleteReasons) != 0 {
		t.Fatalf("incomplete reasons = %v, want fully complete with the anchor superseded (state %q)", readiness.IncompleteReasons, readiness.State)
	}
	if string(readiness.State) == "target_incomplete" {
		t.Fatalf("readiness state = target_incomplete, want complete: %+v", readiness)
	}
}
