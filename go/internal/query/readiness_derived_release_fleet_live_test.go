// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10135 disposable PostgreSQL API proof that a release absent from the installed fleet stops counting with its name in retired_releases, while a release present in the fleet but outside the plan window still counts in every window
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// seedFleetImage writes one active scope generation holding the named Alpine
// OS packages at distroVersion, the way a scanned image lands in
// fact_records: the planner's reader and the readiness fleet probe both join
// active generations, so the helper activates what it writes.
func seedFleetImage(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, image, distroVersion string, packages ...string) {
	t.Helper()
	scopeID, generationID := "scan-target:fleet:"+image, "generation:fleet:"+image
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload) VALUES ($1, 'container_image', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`, scopeID, now); err != nil {
		t.Fatalf("seed fleet scope %s: %v", image, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, generationID, scopeID, now); err != nil {
		t.Fatalf("seed fleet generation %s: %v", image, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generationID, scopeID); err != nil {
		t.Fatalf("activate fleet %s: %v", image, err)
	}
	for _, pkg := range packages {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
VALUES ('os-package:fleet:' || $1 || ':' || $2, $1, $2, 'vulnerability.os_package', $1 || ':' || $3, 'synthetic', $1 || ':' || $3,
  $4::timestamptz, $4::timestamptz, FALSE,
  jsonb_build_object('distro','alpine','distro_version',$5::text,'package_manager','apk','name',$3::text,
    'arch','x86_64','repository_class','vendor','vendor_advisory_source','alpine','installed_version_raw','1.0.0-r0',
    'purl','pkg:apk/alpine/' || $3::text || '@1.0.0-r0'), 0)`, scopeID, generationID, pkg, now, distroVersion); err != nil {
			t.Fatalf("seed fleet package %s %s: %v", image, pkg, err)
		}
	}
}

// seedFleetCurlStates writes the two-release curl shape the fleet tests share:
// v3.17 failed and older, v3.18 fresh and newer.
func seedFleetCurlStates(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
	t.Helper()
	seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
	seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV317Scope, "failed")
	seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
}

// readDerivedReadinessBody runs the same request readDerivedReadiness runs and
// returns the raw envelope, so tests can assert on response fields the decoded
// view does not carry.
func readDerivedReadinessBody(t *testing.T, db *sql.DB, rawQuery string) []byte {
	t.Helper()
	handler := &SupplyChainHandler{
		ImpactFindings: &recordingSupplyChainImpactFindingStore{},
		Readiness:      impact.NewPostgresReadinessStore(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?"+rawQuery+"&limit=10", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

// testSupplyChainImpactReadinessDerivedReleaseAbsentFromFleetStopsCountingLive
// is #10135 RED: the failed v3.17 state must stop counting once no active
// os_package fact carries that release, even with no planner run at all.
func testSupplyChainImpactReadinessDerivedReleaseAbsentFromFleetStopsCountingLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedFleetCurlStates(t, ctx, db, now)
	seedFleetImage(t, ctx, db, now, "img-only-318", "3.18.4", "curl")

	readiness := readDerivedReadiness(t, db, curlPackageID)
	state := onlyDerivedState(t, readiness)
	if state.FreshnessState != "fresh" {
		t.Fatalf("derived aggregate = %+v, want fresh: v3.17 left the fleet", state)
	}
	if slices.Contains(readiness.IncompleteReasons, "osv:failed:alpine:v3.17") {
		t.Fatalf("incomplete reasons = %v, want no v3.17: it left the fleet", readiness.IncompleteReasons)
	}
}

// testSupplyChainImpactReadinessDerivedReleasePresentOutsidePlanWindowStillCountsLive
// pins the other side of #10135: v3.17 IS still installed, so its failed state
// keeps counting in every plan window, including windows whose plan does not
// name it and a read with no plan at all.
func testSupplyChainImpactReadinessDerivedReleasePresentOutsidePlanWindowStillCountsLive(t *testing.T) {
	plans := []struct {
		name    string
		targets []map[string]any
		skipRun bool
	}{
		{"plan names only v3.17", []map[string]any{plannedAlpineTarget(curlV317Scope, "curl")}, false},
		{"plan names only v3.18", []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")}, false},
		{"plan names neither", []map[string]any{plannedAlpineTarget("vuln-intel://osv/alpine/zlib?release=v3.18&version=1.2.13-r0", "zlib")}, false},
		{"plan names both", []map[string]any{plannedAlpineTarget(curlV317Scope, "curl"), plannedAlpineTarget(curlV318Scope, "curl")}, false},
		{"no planner run", nil, true},
	}
	for _, tc := range plans {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedFleetCurlStates(t, ctx, db, now)
			seedFleetImage(t, ctx, db, now, "img-317", "3.17.3", "curl")
			seedFleetImage(t, ctx, db, now, "img-318", "3.18.4", "curl")
			if !tc.skipRun {
				seedPlannerRun(t, ctx, db, now, "derived-alpine", tc.targets, nil)
			}

			readiness := readDerivedReadiness(t, db, curlPackageID)
			state := onlyDerivedState(t, readiness)
			if state.FreshnessState != "failed" {
				t.Fatalf("derived aggregate = %+v, want failed: v3.17 is still installed", state)
			}
			if !slices.Contains(readiness.IncompleteReasons, "osv:failed:alpine:v3.17") {
				t.Fatalf("incomplete reasons = %v, want osv:failed:alpine:v3.17", readiness.IncompleteReasons)
			}
		})
	}
}

// testSupplyChainImpactReadinessDerivedRetiredReleaseIsNamedLive is #10135
// RED for the observability half: the retired release must be named in the
// response even though it no longer holds the package not covered.
func testSupplyChainImpactReadinessDerivedRetiredReleaseIsNamedLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedFleetCurlStates(t, ctx, db, now)
	seedFleetImage(t, ctx, db, now, "img-only-318", "3.18.4", "curl")

	body := readDerivedReadinessBody(t, db, curlPackageID)
	if !strings.Contains(string(body), `"retired_releases":["osv:alpine:v3.17"]`) {
		t.Fatalf("response names no retired v3.17 release: %.500s", body)
	}
	var envelope struct {
		Readiness derivedReadinessView `json:"readiness"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if slices.Contains(envelope.Readiness.IncompleteReasons, "osv:failed:alpine:v3.17") {
		t.Fatalf("incomplete reasons = %v, want no v3.17: it retired", envelope.Readiness.IncompleteReasons)
	}
}

// TestSupplyChainImpactReadinessDerivedReleaseFleetLive runs the #10135 fleet
// cases as subtests, each on its own isolated schema, so the readiness shard
// plan counts one test for the family.
func TestSupplyChainImpactReadinessDerivedReleaseFleetLive(t *testing.T) {
	t.Run("ReleaseAbsentFromFleetStopsCountingLive", testSupplyChainImpactReadinessDerivedReleaseAbsentFromFleetStopsCountingLive)
	t.Run("ReleasePresentOutsidePlanWindowStillCountsLive", testSupplyChainImpactReadinessDerivedReleasePresentOutsidePlanWindowStillCountsLive)
	t.Run("RetiredReleaseIsNamedLive", testSupplyChainImpactReadinessDerivedRetiredReleaseIsNamedLive)
}
