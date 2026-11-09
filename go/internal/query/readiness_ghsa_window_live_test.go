// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10464 disposable PostgreSQL API proof that a fetched GHSA window reads fresh inside the window and stale outside it: the planned window entry in the latest run is an expected row, its stream state confirms the advisory family while the fetch is current, and an uncovered window reads as ghsa:committed_snapshot_missing
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

const (
	ghsaWindowLiveStartScope = "vuln-intel://ghsa/reviewed/_window/1767225600000000-1767398400000000"
	ghsaWindowLiveStream     = "vuln-intel://ghsa/reviewed"
	ghsaWindowLiveAdvisory   = "GHSA-aaaa-bbbb-cccc"
	ghsaWindowLiveCVE        = "CVE-2026-9301"
)

// seedReadinessGHSAWindowRun plans one GHSA rolling window in the latest
// run's requested_scope_set, the coordinator's plan record the readiness
// expected rows are built from. GHSA entries carry no endpoint candidates:
// coverage is proved by the window snapshot's covered_through, not by a
// descriptor match.
func seedReadinessGHSAWindowRun(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, instance, windowScope, start, end string) {
	t.Helper()
	scopeSet, err := json.Marshal(map[string]any{
		"collector_instance_id": instance,
		"targets": []map[string]any{{
			"scope_id":         windowScope,
			"source":           "ghsa",
			"modified_start":   start,
			"modified_end":     end,
			"results_per_page": 100,
			"derived":          true,
			"target_class":     "configured_direct",
		}},
	})
	if err != nil {
		t.Fatalf("marshal GHSA window scope set: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        VALUES ($1, 'schedule', 'collection_complete', $2, 'vulnerability_intelligence', $3, $3)`,
		"ghsa-window-run", scopeSet, now); err != nil {
		t.Fatalf("seed GHSA window planner run: %v", err)
	}
}

// seedReadinessGHSAWindowState records the stream-keyed GHSA source state:
// the row lives under the anchor stream scope and carries the committed
// window bounds, so thousands of window scopes a year never crowd the
// status read.
func seedReadinessGHSAWindowState(t *testing.T, ctx context.Context, db *sql.DB, successAt time.Time, instance string, windowStart, windowEnd time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO vulnerability_source_states
        (collector_instance_id, scope_id, source, freshness_state, terminal_status, last_attempt_at, last_success_at, updated_at, window_start, window_end)
        VALUES ($1, $2, 'ghsa', 'fresh', 'succeeded', $3, $3, $3, $4, $5)`,
		instance, ghsaWindowLiveStream, successAt, windowStart, windowEnd); err != nil {
		t.Fatalf("seed GHSA window stream state: %v", err)
	}
}

// seedReadinessGHSAWindowSnapshot commits a window-mode snapshot under the
// window scope stating covered_through at the planned window end.
func seedReadinessGHSAWindowSnapshot(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scope, instance, coveredThrough string) {
	t.Helper()
	seedReadinessSourceSnapshotFact(t, ctx, db, now, scope, "ghsa", "", instance, true)
	payload, err := json.Marshal(map[string]any{"covered_through": coveredThrough})
	if err != nil {
		t.Fatalf("marshal covered_through: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records
        SET payload = payload || $2::jsonb
        WHERE fact_id = $1`, scope+"-snapshot", payload); err != nil {
		t.Fatalf("seed covered_through: %v", err)
	}
}

// seedReadinessGHSAAdvisoryFacts stores one GHSA advisory copy (CVE plus
// affected_package facts) under the window scope with an old observed_at, so
// the advisory family is stale by fact age and only a fresh fetch confirms
// it. The facts share the window generation the snapshot seeder creates,
// the way a committed window generation holds advisory facts and its source
// snapshot together; the inserts are idempotent so the seeder runs with or
// without a snapshot.
func seedReadinessGHSAAdvisoryFacts(t *testing.T, ctx context.Context, db *sql.DB, observedAt time.Time, scope, generation, cveID, advisoryID, modifiedAt string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
        VALUES ($1, 'vulnerability_intelligence', 'vulnerability_intelligence', $1,
                'vulnerability_intelligence', $1, $2, $2, 'active', $3)
        ON CONFLICT DO NOTHING`,
		scope, observedAt, generation); err != nil {
		t.Fatalf("seed GHSA advisory scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
        (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
        VALUES ($1, $2, 'schedule', $3, $3, 'active')
        ON CONFLICT DO NOTHING`, scope, generation, observedAt); err != nil {
		t.Fatalf("seed GHSA advisory generation: %v", err)
	}
	cvePayload, err := json.Marshal(map[string]any{
		"cve_id": cveID, "advisory_id": advisoryID, "ghsa_id": advisoryID,
		"source": "ghsa", "modified_at": modifiedAt, "severity_label": "HIGH", "cvss_score": 9.8,
	})
	if err != nil {
		t.Fatalf("marshal GHSA cve payload: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
         source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ($1, $2, $3, 'vulnerability.cve', $1, 'vulnerability_intelligence',
                $1, $4, $4, FALSE, $5)`,
		"ghsa-cve-"+generation, scope, generation, observedAt, cvePayload); err != nil {
		t.Fatalf("seed GHSA cve fact: %v", err)
	}
	// No affected_package fact: the advisory family only needs the CVE fact,
	// and a package fact would resolve through manifest consumption.
}

// TestSupplyChainImpactReadinessGHSAWindowFreshnessLive is the #10464
// acceptance cell: a fetched GHSA window reads fresh inside the window and
// stale outside it. Old advisory facts keep the observed_at of the last real
// change, so the advisory family is stale by fact age; a fresh stream fetch
// confirms it, an aged fetch leaves it stale, and a planned window with no
// snapshot reads as ghsa:committed_snapshot_missing.
func TestSupplyChainImpactReadinessGHSAWindowFreshnessLive(t *testing.T) {
	windowStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		successAge   time.Duration
		snapshot     bool
		wantFamily   string
		wantState    packet.ReadinessState
		wantMissing  bool
		wantStreamIn bool
	}{
		{
			name: "fetched window fresh inside the window", successAge: 0, snapshot: true,
			wantFamily: packet.FreshnessLabelFresh, wantState: packet.ReadinessStateReadyZeroFindings, wantStreamIn: true,
		},
		{
			name: "aged fetch stale outside the window", successAge: 30 * 24 * time.Hour, snapshot: true,
			wantFamily: packet.FreshnessLabelStale, wantState: packet.ReadinessStateTargetIncomplete, wantStreamIn: true,
		},
		{
			name: "planned window without snapshot missing", successAge: 0, snapshot: false,
			wantFamily: packet.FreshnessLabelFresh, wantState: packet.ReadinessStateTargetIncomplete, wantMissing: true, wantStreamIn: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, database, now := readinessScopeLiveDatabase(t)
			old := now.Add(-30 * 24 * time.Hour)
			seedReadinessScopeInstance(t, ctx, database, now, "ghsa-rolling", []map[string]any{{
				"source": "ghsa", "scope_id": ghsaWindowLiveStream, "advisory_type": "reviewed",
			}})
			seedReadinessGHSAWindowRun(t, ctx, database, now, "ghsa-rolling", ghsaWindowLiveStartScope,
				windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339))
			seedReadinessGHSAWindowState(t, ctx, database, now.Add(-tc.successAge), "ghsa-rolling", windowStart, windowEnd)
			if tc.snapshot {
				seedReadinessGHSAWindowSnapshot(t, ctx, database, now, ghsaWindowLiveStartScope, "ghsa-rolling", windowEnd.Format(time.RFC3339))
			}
			seedReadinessGHSAAdvisoryFacts(t, ctx, database, old, ghsaWindowLiveStartScope, ghsaWindowLiveStartScope+"-generation",
				ghsaWindowLiveCVE, ghsaWindowLiveAdvisory, "2025-12-01T00:00:00Z")

			readiness := readReadinessScopeAPI(t, database, ghsaWindowLiveCVE)

			family := ""
			for _, source := range readiness.EvidenceSources {
				if source.Family == packet.EvidenceFamilyVulnerabilityAdvisory {
					family = source.Freshness
				}
			}
			if family != tc.wantFamily {
				t.Fatalf("advisory family freshness = %q, want %q (state %q, reasons %v)",
					family, tc.wantFamily, readiness.State, readiness.IncompleteReasons)
			}
			if readiness.State != tc.wantState {
				t.Fatalf("readiness state = %q, want %q (reasons %v, missing %v)",
					readiness.State, tc.wantState, readiness.IncompleteReasons, readiness.MissingEvidence)
			}
			missing := slices.Contains(readiness.IncompleteReasons, "ghsa:committed_snapshot_missing")
			if missing != tc.wantMissing {
				t.Fatalf("ghsa:committed_snapshot_missing = %v, want %v (reasons %v)", missing, tc.wantMissing, readiness.IncompleteReasons)
			}
			found := false
			for _, state := range readiness.SourceStates {
				if strings.TrimSpace(state.ScopeID) == ghsaWindowLiveStartScope {
					found = true
				}
			}
			if found != tc.wantStreamIn {
				t.Fatalf("window scope in source states = %v, want %v (%+v)", found, tc.wantStreamIn, readiness.SourceStates)
			}
		})
	}
}
