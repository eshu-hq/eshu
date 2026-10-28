// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9227 disposable PostgreSQL API proof that source-state success is bound to the configured selector and malformed CVE elements refuse clean-zero coverage
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

func TestSupplyChainImpactReadinessReconfiguredExactSelectorRefusesOldSuccessLive(t *testing.T) {
	ctx, database, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, database, now, "changing-instance", []map[string]any{
		{"source": "nvd", "scope_id": "same-scope", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessScopeState(t, ctx, database, now, "changing-instance", "same-scope", "nvd", "fresh", "succeeded")
	seedReadinessNVDCommittedSnapshot(t, ctx, database, now, "same-scope", "changing-instance", "CVE-2026-9227")
	before := readReadinessScopeAPI(t, database, "CVE-2026-9227")
	if before.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("CVE A before reconfiguration = %q, want ready_zero_findings", before.State)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET updated_at = $1
        WHERE instance_id = 'changing-instance'`, now.Add(time.Second)); err != nil {
		t.Fatalf("reconcile unchanged instance: %v", err)
	}
	if unchanged := readReadinessScopeAPI(t, database, "CVE-2026-9227"); unchanged.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("unchanged reconciliation lost committed coverage: %+v", unchanged)
	}
	changed, err := json.Marshal(map[string]any{"targets": []map[string]any{{
		"source": "nvd", "scope_id": "same-scope", "cve_ids": []string{"CVE-2026-9228"},
	}}})
	if err != nil {
		t.Fatalf("marshal reconfiguration: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1, updated_at = $2
        WHERE instance_id = 'changing-instance'`, changed, now.Add(1)); err != nil {
		t.Fatalf("reconfigure exact target in place: %v", err)
	}
	after := readReadinessScopeAPI(t, database, "CVE-2026-9228")
	if after.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("CVE B inherited CVE A source success: %+v", after)
	}
	// An old A generation can commit after B is configured. Observation time
	// cannot bind its result to B's current selector.
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET observed_at = $1
        WHERE fact_id = 'same-scope-snapshot'`, now.Add(3*time.Second)); err != nil {
		t.Fatalf("record late A snapshot commit: %v", err)
	}
	if lateA := readReadinessScopeAPI(t, database, "CVE-2026-9228"); lateA.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("late A snapshot certified B: %+v", lateA)
	}
	original, err := json.Marshal(map[string]any{"targets": []map[string]any{{
		"source": "nvd", "scope_id": "same-scope", "cve_ids": []string{"CVE-2026-9227"},
	}}})
	if err != nil {
		t.Fatalf("marshal original target: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1, updated_at = $2
        WHERE instance_id = 'changing-instance'`, original, now.Add(4*time.Second)); err != nil {
		t.Fatalf("restore exact target: %v", err)
	}
	if restored := readReadinessScopeAPI(t, database, "CVE-2026-9227"); restored.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("A->B->A lost still-active A evidence: %+v", restored)
	}
}

func TestSupplyChainImpactReadinessMalformedCVEElementsRefuseCleanZeroLive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cveIDs []any
	}{
		{name: "blank", cveIDs: []any{"  "}},
		{name: "nonstring", cveIDs: []any{7}},
		{name: "null", cveIDs: []any{nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, database, now := readinessScopeLiveDatabase(t)
			seedReadinessScopeInstance(t, ctx, database, now, "malformed-elements-instance", []map[string]any{
				{"source": "nvd", "scope_id": "exact", "cve_ids": []string{"CVE-2026-9227"}},
				{"source": "nvd", "scope_id": "malformed", "cve_ids": tc.cveIDs},
			})
			seedReadinessScopeState(t, ctx, database, now, "malformed-elements-instance", "exact", "nvd", "fresh", "succeeded")
			readiness := readReadinessScopeAPI(t, database, "CVE-2026-9227")
			if readiness.State != impact.ReadinessStateReadinessUnavailable {
				t.Fatalf("malformed %s CVE element readiness = %+v, want readiness_unavailable", tc.name, readiness)
			}
		})
	}
}

func seedReadinessNVDCommittedSnapshot(t *testing.T, ctx context.Context, db *sql.DB,
	now time.Time, scopeID, instanceID, cveID string,
) {
	t.Helper()
	seedReadinessSourceSnapshotFact(t, ctx, db, now, scopeID, "nvd", "", instanceID, true)
	descriptor, err := json.Marshal(map[string]any{
		"version": "nvd.v1", "source": "nvd", "mode": "cve",
		"cve_ids":                []string{cveID},
		"modified_start_unix_us": 0, "modified_end_unix_us": 0,
		"results_per_page":    0,
		"endpoint_candidates": []string{"https://services.nvd.nist.gov/rest/json/cves/2.0"},
		"executed_endpoint":   "https://services.nvd.nist.gov/rest/json/cves/2.0",
	})
	if err != nil {
		t.Fatalf("marshal NVD coverage descriptor: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records
        SET payload = JSONB_SET(payload, '{coverage_descriptor}', $2::jsonb)
        WHERE fact_id = $1`, scopeID+"-snapshot", descriptor); err != nil {
		t.Fatalf("seed NVD coverage descriptor: %v", err)
	}
}

func TestSupplyChainImpactReadinessConflictingDuplicateScopeUnavailableLive(t *testing.T) {
	ctx, database, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, database, now, "duplicate-instance", []map[string]any{
		{"source": "nvd", "scope_id": "same-scope", "cve_ids": []string{"CVE-2026-9227"}},
		{"source": "nvd", "scope_id": "same-scope", "cve_ids": []string{"CVE-2026-9228"}},
	})
	seedReadinessScopeState(t, ctx, database, now, "duplicate-instance", "same-scope", "nvd", "fresh", "succeeded")
	readiness := readReadinessScopeAPI(t, database, "CVE-2026-9227")
	if readiness.State != impact.ReadinessStateReadinessUnavailable {
		t.Fatalf("conflicting duplicate scope readiness = %+v, want readiness_unavailable", readiness)
	}
	assertReadinessTargetsMalformed(t, ctx, database, "conflicting duplicate scope")

	// A garbage NVD window or page size beside a healthy CVE target must
	// classify as source_targets_malformed, never abort the read with a
	// Postgres cast error (query_error). Each value is one the collector's
	// strict config parse refuses.
	healthy := map[string]any{"source": "nvd", "scope_id": "healthy", "cve_ids": []string{"CVE-2026-9227"}}
	window := func(field string, value any) map[string]any {
		target := map[string]any{
			"source": "nvd", "scope_id": "window",
			"modified_start": "2026-05-01T00:00:00Z", "modified_end": "2026-05-02T00:00:00Z",
			"results_per_page": 100,
		}
		target[field] = value
		return target
	}
	for _, tc := range []struct {
		name   string
		target map[string]any
	}{
		{"modified_start garbage text", window("modified_start", "garbage")},
		{"modified_start out-of-range month", window("modified_start", "2026-13-01T00:00:00Z")},
		{"modified_start without zone", window("modified_start", "2026-05-01")},
		{"modified_start JSON number", window("modified_start", 1714521600)},
		{"modified_end garbage text", window("modified_end", "not-a-time")},
		{"modified_end hour 24", window("modified_end", "2026-05-02T24:00:00Z")},
		{"modified_end time without zone", window("modified_end", "2026-05-02T00:00:00")},
		{"modified_end JSON object", window("modified_end", map[string]any{"at": "x"})},
		{"results_per_page text", window("results_per_page", "abc")},
		{"results_per_page fraction", window("results_per_page", 100.5)},
		{"results_per_page int overflow", window("results_per_page", 99999999999)},
		{"results_per_page boolean", window("results_per_page", true)},
		{"results_per_page without window", map[string]any{
			"source": "nvd", "scope_id": "window",
			"cve_ids": []string{"CVE-2026-0001"}, "results_per_page": "abc",
		}},
	} {
		configuration, err := json.Marshal(map[string]any{"targets": []map[string]any{healthy, tc.target}})
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1
        WHERE instance_id = 'duplicate-instance'`, configuration); err != nil {
			t.Fatalf("%s: plant malformed target: %v", tc.name, err)
		}
		assertReadinessTargetsMalformed(t, ctx, database, tc.name)
	}
	// Control: a strict RFC 3339 window with fraction and offset stays valid.
	valid := window("modified_start", " 2026-05-01T00:00:00.5+02:00 ")
	configuration, err := json.Marshal(map[string]any{"targets": []map[string]any{healthy, valid}})
	if err != nil {
		t.Fatalf("control: marshal: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1
        WHERE instance_id = 'duplicate-instance'`, configuration); err != nil {
		t.Fatalf("control: plant valid window: %v", err)
	}
	if _, err := impact.NewPostgresReadinessStore(database).ReadSupplyChainImpactReadiness(ctx,
		impact.ReadinessQuery{CVEID: "CVE-2026-9227"}); err != nil {
		t.Fatalf("control: valid RFC 3339 window read failed: %v", err)
	}
}

func assertReadinessTargetsMalformed(t *testing.T, ctx context.Context, db *sql.DB, name string) {
	t.Helper()
	_, err := impact.NewPostgresReadinessStore(db).ReadSupplyChainImpactReadiness(ctx,
		impact.ReadinessQuery{CVEID: "CVE-2026-9227"})
	if cause := impact.ReadinessUnavailableCause(err); cause != impact.ReadinessUnavailableCauseTargetsMalformed {
		t.Errorf("%s: readiness cause = %q (err %v), want %q", name, cause, err,
			impact.ReadinessUnavailableCauseTargetsMalformed)
	}
}

func TestSupplyChainImpactReadinessNVDConfiguredFallbackIdentityLive(t *testing.T) {
	ctx, database, now := readinessScopeLiveDatabase(t)
	first := "https://nvd-a.example.test/api"
	second := "https://nvd-b.example.test/api"
	seedReadinessScopeInstance(t, ctx, database, now, "fallback-instance", []map[string]any{{
		"source": "nvd", "scope_id": "fallback-scope", "cve_ids": []string{"CVE-2026-9227"},
		"base_url": first, "fallback_urls": []string{second},
	}})
	seedReadinessScopeState(t, ctx, database, now, "fallback-instance", "fallback-scope", "nvd", "fresh", "succeeded")
	seedReadinessNVDCommittedSnapshot(t, ctx, database, now, "fallback-scope", "fallback-instance", "CVE-2026-9227")
	descriptor, err := json.Marshal(map[string]any{
		"version": "nvd.v1", "source": "nvd", "mode": "cve", "cve_ids": []string{"CVE-2026-9227"},
		"modified_start_unix_us": 0, "modified_end_unix_us": 0, "results_per_page": 0,
		"endpoint_candidates": []string{first, second}, "executed_endpoint": second,
	})
	if err != nil {
		t.Fatalf("marshal fallback descriptor: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_records SET payload = JSONB_SET(payload,
        '{coverage_descriptor}', $1::jsonb) WHERE fact_id = 'fallback-scope-snapshot'`, descriptor); err != nil {
		t.Fatalf("seed executed fallback: %v", err)
	}
	if initial := readReadinessScopeAPI(t, database, "CVE-2026-9227"); initial.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("configured fallback coverage = %+v, want ready_zero_findings", initial)
	}
	reordered, err := json.Marshal(map[string]any{"targets": []map[string]any{{
		"source": "nvd", "scope_id": "fallback-scope", "cve_ids": []string{"CVE-2026-9227"},
		"base_url": second, "fallback_urls": []string{first},
	}}})
	if err != nil {
		t.Fatalf("marshal reordered endpoints: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1
        WHERE instance_id = 'fallback-instance'`, reordered); err != nil {
		t.Fatalf("reorder configured endpoints: %v", err)
	}
	if sameSet := readReadinessScopeAPI(t, database, "CVE-2026-9227"); sameSet.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("reordered configured endpoints lost coverage: %+v", sameSet)
	}
	withBlankFallback, err := json.Marshal(map[string]any{"targets": []map[string]any{{
		"source": "nvd", "scope_id": "fallback-scope", "cve_ids": []string{"CVE-2026-9227"},
		"base_url": second, "fallback_urls": []string{"", first},
	}}})
	if err != nil {
		t.Fatalf("marshal blank fallback: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1
        WHERE instance_id = 'fallback-instance'`, withBlankFallback); err != nil {
		t.Fatalf("add blank fallback: %v", err)
	}
	if sameSet := readReadinessScopeAPI(t, database, "CVE-2026-9227"); sameSet.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("ignored blank fallback changed coverage: %+v", sameSet)
	}
	changed, err := json.Marshal(map[string]any{"targets": []map[string]any{{
		"source": "nvd", "scope_id": "fallback-scope", "cve_ids": []string{"CVE-2026-9227"},
		"base_url": second, "fallback_urls": []string{"https://other.example.test/api"},
	}}})
	if err != nil {
		t.Fatalf("marshal changed endpoint set: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1
        WHERE instance_id = 'fallback-instance'`, changed); err != nil {
		t.Fatalf("change configured endpoint set: %v", err)
	}
	if newSet := readReadinessScopeAPI(t, database, "CVE-2026-9227"); newSet.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("changed endpoint set inherited old coverage: %+v", newSet)
	}
}
