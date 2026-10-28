// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9227 disposable PostgreSQL API proof that arbitrary configured vulnerability source scopes survive target resolution into readiness
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func TestSupplyChainImpactReadinessConfiguredSourceScopesLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #9227 configured source-scope proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "impact_source_scopes", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := database.ExecContext(ctx, `INSERT INTO package_manifest_consumption_key_backfill_markers (marker_name, completed_at) VALUES ($1, $2)`,
		storagepostgres.PackageManifestConsumptionKeyBackfillMarker, now); err != nil {
		t.Fatalf("seed readiness marker: %v", err)
	}
	for _, item := range []struct {
		instance string
		scope    string
		source   string
	}{
		{"intelligence-nvd", "custom-nvd-scope-9227", "nvd"},
		{"intelligence-epss", "custom-epss-scope-9227", "first_epss"},
	} {
		configuration, err := json.Marshal(map[string]any{"targets": []map[string]any{{
			"source": item.source, "scope_id": item.scope, "cve_ids": []string{"CVE-2026-9227"},
		}}})
		if err != nil {
			t.Fatalf("marshal target configuration: %v", err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO collector_instances
            (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
            VALUES ($1, 'vulnerability_intelligence', 'continuous', TRUE, $2, $3, $3, $3)`,
			item.instance, configuration, now); err != nil {
			t.Fatalf("seed collector instance %s: %v", item.instance, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO vulnerability_source_states
            (collector_instance_id, scope_id, source, freshness_state, terminal_status, last_attempt_at, last_success_at, updated_at)
            VALUES ($1, $2, $3, 'fresh', 'succeeded', $4, $4, $4)`,
			item.instance, item.scope, item.source, now); err != nil {
			t.Fatalf("seed source state %s: %v", item.scope, err)
		}
	}
	handler := &SupplyChainHandler{
		ImpactFindings: &recordingSupplyChainImpactFindingStore{},
		Readiness:      impact.NewPostgresReadinessStore(database),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9227&limit=10", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Readiness impact.ReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := map[string]string{}
	for _, state := range body.Readiness.SourceStates {
		got[state.ScopeID] = state.FreshnessState
	}
	if len(got) != 2 || got["custom-nvd-scope-9227"] != "fresh" || got["custom-epss-scope-9227"] != "fresh" {
		t.Fatalf("configured source states = %v, want both arbitrary scopes fresh; readiness = %+v", got, body.Readiness)
	}
}

func readinessScopeLiveDatabase(t *testing.T) (context.Context, *sql.DB, time.Time) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #9227 configured source-scope proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "impact_scope_cases", func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := database.ExecContext(ctx, `INSERT INTO package_manifest_consumption_key_backfill_markers (marker_name, completed_at) VALUES ($1, $2)`,
		storagepostgres.PackageManifestConsumptionKeyBackfillMarker, now); err != nil {
		t.Fatalf("seed readiness marker: %v", err)
	}
	return ctx, database, now
}

func seedReadinessScopeInstance(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, id string, targets []map[string]any) {
	t.Helper()
	configuration, err := json.Marshal(map[string]any{"targets": targets})
	if err != nil {
		t.Fatalf("marshal targets: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO collector_instances
        (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
        VALUES ($1, 'vulnerability_intelligence', 'continuous', TRUE, $2, $3, $3, $3)`, id, configuration, now); err != nil {
		t.Fatalf("seed instance %s: %v", id, err)
	}
}

func seedReadinessScopeState(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, instance, scope, source, freshness, terminal string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO vulnerability_source_states
        (collector_instance_id, scope_id, source, freshness_state, terminal_status, last_attempt_at, last_success_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $6, $6)`,
		instance, scope, source, freshness, terminal, now); err != nil {
		t.Fatalf("seed state %s/%s: %v", instance, scope, err)
	}
}

func readReadinessScopeAPI(t *testing.T, db *sql.DB, cveID string) impact.ReadinessEnvelope {
	t.Helper()
	handler := &SupplyChainHandler{
		ImpactFindings: &recordingSupplyChainImpactFindingStore{},
		Readiness:      impact.NewPostgresReadinessStore(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id="+cveID+"&limit=10", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Readiness impact.ReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Readiness
}

func TestSupplyChainImpactReadinessSelectorAndPendingScopeLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "selector-instance", []map[string]any{
		{"source": "nvd", "scope_id": "matching", "cve_ids": []string{"CVE-2026-9227"}},
		{"source": "nvd", "scope_id": "other-cve", "cve_ids": []string{"CVE-2026-9999"}},
		{"source": "nvd", "scope_id": "missing-state", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessScopeState(t, ctx, db, now, "selector-instance", "matching", "nvd", "fresh", "succeeded")
	seedReadinessScopeState(t, ctx, db, now, "selector-instance", "other-cve", "nvd", "failed", "failed_terminal")
	seedReadinessScopeInstance(t, ctx, db, now, "second-instance", []map[string]any{
		{"source": "nvd", "scope_id": "matching", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessScopeState(t, ctx, db, now, "second-instance", "matching", "nvd", "fresh", "succeeded")
	seedReadinessScopeInstance(t, ctx, db, now, "disabled-instance", []map[string]any{
		{"source": "nvd", "scope_id": "disabled-scope", "cve_ids": []string{"CVE-2026-9227"}},
	})
	if _, err := db.ExecContext(ctx, `UPDATE collector_instances SET enabled = FALSE WHERE instance_id = 'disabled-instance'`); err != nil {
		t.Fatalf("disable unrelated collector: %v", err)
	}
	seedReadinessScopeState(t, ctx, db, now, "disabled-instance", "disabled-scope", "nvd", "failed", "failed_terminal")

	readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if len(readiness.SourceStates) != 3 {
		t.Fatalf("source states = %+v, want two matching instance scopes and one pending", readiness.SourceStates)
	}
	got := map[string]string{}
	for _, state := range readiness.SourceStates {
		got[state.CollectorInstanceID+"/"+state.ScopeID] = state.FreshnessState
	}
	if got["selector-instance/matching"] != "fresh" ||
		got["second-instance/matching"] != "fresh" ||
		got["selector-instance/missing-state"] != "pending" {
		t.Fatalf("selected states = %v, want two fresh and one pending", got)
	}
	if readiness.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("readiness = %q, want target_incomplete for configured target without state", readiness.State)
	}
}

func TestSupplyChainImpactReadinessScopeOverflowLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	targets := make([]map[string]any, 201)
	for index := range targets {
		targets[index] = map[string]any{
			"source": "nvd", "scope_id": fmt.Sprintf("scope-%03d", index),
			"cve_ids": []string{"CVE-2026-9227"},
		}
	}
	seedReadinessScopeInstance(t, ctx, db, now, "overflow-instance", targets[:200])
	seedReadinessSourceSnapshotFact(t, ctx, db, now, "scope-199", "nvd", "", "overflow-instance", true)
	readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if len(readiness.SourceStates) != 200 || len(readiness.SourceSnapshots) != 1 ||
		readiness.SourceSnapshots[0].Source != "nvd" || readiness.State == impact.ReadinessStateReadinessUnavailable {
		t.Fatalf("200 configured scopes = %d states, %d snapshots, readiness %q; want full bounded result",
			len(readiness.SourceStates), len(readiness.SourceSnapshots), readiness.State)
	}
	configuration, err := json.Marshal(map[string]any{"targets": targets})
	if err != nil {
		t.Fatalf("marshal overflow targets: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE collector_instances SET configuration = $1 WHERE instance_id = 'overflow-instance'`, configuration); err != nil {
		t.Fatalf("add 201st scope: %v", err)
	}
	seedReadinessScopeState(t, ctx, db, now, "overflow-instance", "scope-200", "nvd", "failed", "failed_terminal")
	readiness = readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if readiness.State != impact.ReadinessStateReadinessUnavailable ||
		!impact.ReadinessMissingContains(readiness.MissingEvidence, impact.MissingEvidenceReadinessUnavailable) {
		t.Fatalf("201 configured scopes = %+v, want explicit readiness_unavailable", readiness)
	}
}

func seedReadinessSourceSnapshotFact(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scope, source, ecosystem, instance string, complete bool) {
	t.Helper()
	generation := scope + "-generation"
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
        VALUES ($1, 'vulnerability_intelligence', 'vulnerability_intelligence', $1, 'vulnerability_intelligence', $1, $2, $2, 'active')`,
		scope, now); err != nil {
		t.Fatalf("seed ingestion scope %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
        (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
        VALUES ($1, $2, 'schedule', $3, $3, 'active')`, scope, generation, now); err != nil {
		t.Fatalf("seed generation %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, generation); err != nil {
		t.Fatalf("activate generation %s: %v", generation, err)
	}
	payload, err := json.Marshal(map[string]any{
		"source": source, "ecosystem": ecosystem, "collector_instance_id": instance, "complete": complete, "result_count": 0,
		"cache_freshness": "fresh", "cache_updated_at": now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
         source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ($1, $2, $3, 'vulnerability.source_snapshot', $1,
                'vulnerability_intelligence', $1, $4, $4, FALSE, $5)`,
		scope+"-snapshot", scope, generation, now, payload); err != nil {
		t.Fatalf("seed source snapshot %s: %v", scope, err)
	}
}

func TestSupplyChainImpactReadinessGLADSnapshotKeepsEcosystemBoundaryLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessSourceSnapshotFact(t, ctx, db, now, "glad-npm", "glad", "npm", "", true)
	seedReadinessSourceSnapshotFact(t, ctx, db, now, "glad-pypi", "glad", "pypi", "", true)
	store := impact.NewPostgresReadinessStore(db)
	snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, impact.ReadinessQuery{PackageID: "npm://registry.npmjs.org/example"})
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness(): %v", err)
	}
	if len(snapshot.SourceSnapshots) != 1 || snapshot.SourceSnapshots[0].Source != "glad" || snapshot.SourceSnapshots[0].Ecosystem != "npm" {
		t.Fatalf("GLAD source snapshots = %+v, want only npm", snapshot.SourceSnapshots)
	}
	readiness := impact.BuildReadiness(impact.TargetScope{PackageID: "npm://registry.npmjs.org/example"}, nil, false, snapshot)
	if readiness.State == impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("GLAD source snapshot certified clean zero without advisory coverage: %+v", readiness)
	}
}

func TestSupplyChainImpactReadinessOSVEcosystemBoundaryLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "osv-instance", []map[string]any{
		{"source": "osv", "scope_id": "osv-npm", "ecosystem": "npm", "queries": []map[string]any{{"package": map[string]any{"name": "example", "ecosystem": "npm"}, "version": "1.0.0"}}},
		{"source": "osv", "scope_id": "osv-pypi", "ecosystem": "pypi", "queries": []map[string]any{{"package": map[string]any{"name": "example", "ecosystem": "pypi"}, "version": "1.0.0"}}},
	})
	seedReadinessScopeState(t, ctx, db, now, "osv-instance", "osv-npm", "osv", "fresh", "succeeded")
	seedReadinessScopeState(t, ctx, db, now, "osv-instance", "osv-pypi", "osv", "failed", "failed_terminal")
	store := impact.NewPostgresReadinessStore(db)
	snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, impact.ReadinessQuery{PackageID: "npm://registry.npmjs.org/example"})
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness(): %v", err)
	}
	if len(snapshot.SourceStates) != 1 || snapshot.SourceStates[0].ScopeID != "osv-npm" {
		t.Fatalf("OSV source states = %+v, want only npm", snapshot.SourceStates)
	}
	readiness := impact.BuildReadiness(impact.TargetScope{PackageID: "npm://registry.npmjs.org/example"}, nil, false, snapshot)
	if readiness.State == impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("OSV source health certified advisory absence: %+v", readiness)
	}
}

func TestSupplyChainImpactReadinessSnapshotRequiresSourceMatchLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "snapshot-match-instance", []map[string]any{
		{"source": "nvd", "scope_id": "shared-name", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessSourceSnapshotFact(t, ctx, db, now, "shared-name", "osv", "", "snapshot-match-instance", true)
	readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if len(readiness.SourceSnapshots) != 0 {
		t.Fatalf("source snapshots = %+v, want mismatched source excluded", readiness.SourceSnapshots)
	}
}

func TestSupplyChainImpactReadinessCoverageEligibilityLive(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   map[string]any
		source   string
		results  int
		warnings int
		want     impact.ReadinessState
	}{
		{"exact NVD CVE", map[string]any{"source": "nvd", "scope_id": "exact", "cve_ids": []string{"CVE-2026-9227"}}, "nvd", 0, 0, impact.ReadinessStateReadyZeroFindings},
		{"NVD modified window", map[string]any{"source": "nvd", "scope_id": "window", "modified_start": "2026-05-01T00:00:00Z", "modified_end": "2026-05-02T00:00:00Z"}, "nvd", 0, 0, impact.ReadinessStateEvidenceIncomplete},
		{"KEV catalog", map[string]any{"source": "cisa_kev", "scope_id": "kev"}, "cisa_kev", 0, 0, impact.ReadinessStateEvidenceIncomplete},
		{"EPSS CVE", map[string]any{"source": "first_epss", "scope_id": "epss", "cve_ids": []string{"CVE-2026-9227"}}, "first_epss", 0, 0, impact.ReadinessStateEvidenceIncomplete},
		{"exact NVD with unprojected result", map[string]any{"source": "nvd", "scope_id": "reported", "cve_ids": []string{"CVE-2026-9227"}}, "nvd", 1, 0, impact.ReadinessStateTargetIncomplete},
		{"exact NVD with warning", map[string]any{"source": "nvd", "scope_id": "warning", "cve_ids": []string{"CVE-2026-9227"}}, "nvd", 0, 1, impact.ReadinessStateTargetIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedReadinessScopeInstance(t, ctx, db, now, "coverage-instance", []map[string]any{tc.target})
			seedReadinessScopeState(t, ctx, db, now, "coverage-instance", tc.target["scope_id"].(string), tc.source, "fresh", "succeeded")
			if tc.name == "exact NVD CVE" {
				seedReadinessNVDCommittedSnapshot(t, ctx, db, now, "exact", "coverage-instance", "CVE-2026-9227")
			}
			if _, err := db.ExecContext(ctx, `UPDATE vulnerability_source_states SET result_count = $1, warning_count = $2
                WHERE collector_instance_id = 'coverage-instance'`, tc.results, tc.warnings); err != nil {
				t.Fatalf("update source counts: %v", err)
			}
			readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
			if readiness.State != tc.want {
				t.Fatalf("readiness = %q, want %q; states=%+v", readiness.State, tc.want, readiness.SourceStates)
			}
		})
	}
}

func TestSupplyChainImpactReadinessSuccessWithoutCommittedSnapshotLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "uncommitted-instance", []map[string]any{
		{"source": "nvd", "scope_id": "uncommitted", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessScopeState(t, ctx, db, now, "uncommitted-instance", "uncommitted", "nvd", "fresh", "succeeded")
	readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if readiness.State == impact.ReadinessStateReadinessUnavailable {
		_, diagnosticErr := impact.NewPostgresReadinessStore(db).ReadSupplyChainImpactReadiness(ctx, impact.ReadinessQuery{CVEID: "CVE-2026-9227"})
		t.Logf("unavailable diagnostic: %v", diagnosticErr)
	}
	if readiness.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("uncommitted source success readiness = %+v, want target_incomplete", readiness)
	}
}

func TestSupplyChainImpactReadinessSuccessExpiresAtReadTimeLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "expiry-instance", []map[string]any{
		{"source": "nvd", "scope_id": "exact", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessScopeState(t, ctx, db, now, "expiry-instance", "exact", "nvd", "fresh", "succeeded")
	if _, err := db.ExecContext(ctx, `UPDATE vulnerability_source_states
        SET last_success_at = $1 WHERE collector_instance_id = 'expiry-instance' AND scope_id = 'exact'`,
		now.Add(-30*24*time.Hour)); err != nil {
		t.Fatalf("age source state: %v", err)
	}
	readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if len(readiness.SourceStates) != 1 || readiness.SourceStates[0].FreshnessState != "stale" ||
		readiness.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("aged source readiness = %+v, want stale target_incomplete", readiness)
	}
}

func TestSupplyChainImpactReadinessIncompleteSnapshotLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "snapshot-instance", []map[string]any{
		{"source": "nvd", "scope_id": "snapshot-scope", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessScopeState(t, ctx, db, now, "snapshot-instance", "snapshot-scope", "nvd", "fresh", "succeeded")
	seedReadinessSourceSnapshotFact(t, ctx, db, now, "snapshot-scope", "nvd", "", "snapshot-instance", false)
	readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if len(readiness.SourceSnapshots) != 1 || readiness.SourceSnapshots[0].Complete ||
		readiness.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("incomplete source snapshot readiness = %+v, want one incomplete snapshot and target_incomplete", readiness)
	}
}
