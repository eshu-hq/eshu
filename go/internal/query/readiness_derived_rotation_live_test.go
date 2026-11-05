// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10015 disposable PostgreSQL API proof, with the real installed-evidence reader and the real planner over a full rotation, that derived release coverage reads the same in every window the coordinator stores
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const rotationInstance = "derived-alpine"

// seedRotationImage writes one active scope generation holding the Alpine OS
// packages pkg-first..pkg-last at distroVersion, the way a scanned image lands in
// fact_records. layout "image-grouped" makes fact ids sort by image; any other
// layout hashes them, so the reader's row order mixes the images.
func seedRotationImage(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, image, distroVersion string, first, last int, layout string) {
	t.Helper()
	scopeID, generationID := "scan-target:rotation:"+image, "generation:rotation:"+image
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload) VALUES ($1, 'container_image', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`, scopeID, now); err != nil {
		t.Fatalf("seed scope %s: %v", image, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, generationID, scopeID, now); err != nil {
		t.Fatalf("seed generation %s: %v", image, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generationID, scopeID); err != nil {
		t.Fatalf("activate %s: %v", image, err)
	}
	factID := `'os-package:' || md5($1 || ':' || n::text)`
	if layout == "image-grouped" {
		factID = `'os-package:' || $1 || ':' || lpad(n::text, 4, '0')`
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT `+factID+`, $1, $2, 'vulnerability.os_package', $1 || ':' || n, 'synthetic', $1 || ':' || n, $3::timestamptz, $3::timestamptz, FALSE,
  jsonb_build_object('distro','alpine','distro_version',$4::text,'package_manager','apk','name','pkg-'||lpad(n::text,3,'0'),
    'arch','x86_64','repository_class','vendor','vendor_advisory_source','alpine','installed_version_raw','1.0.0-r0',
    'purl','pkg:apk/alpine/pkg-'||lpad(n::text,3,'0')||'@1.0.0-r0'), 0
FROM generate_series($5::int, $6::int) AS n`, scopeID, generationID, now, distroVersion, first, last); err != nil {
		t.Fatalf("seed os packages %s: %v", image, err)
	}
}

func rotationScope(pkg, release string) string {
	if release == "" {
		return "vuln-intel://osv/alpine/" + pkg + "?version=1.0.0-r0"
	}
	return "vuln-intel://osv/alpine/" + pkg + "?release=" + release + "&version=1.0.0-r0"
}

func seedRotationState(t *testing.T, ctx context.Context, db *sql.DB, at time.Time, pkg, release, freshness string) {
	t.Helper()
	terminal := map[string]string{"fresh": "succeeded", "stale": "succeeded", "failed": "failed_retryable"}[freshness]
	seedDerivedScope(t, ctx, db, at, rotationInstance, rotationScope(pkg, release), "alpine", freshness, terminal, [][2]string{{"os", pkg}})
}

// plannedRotationReleases decodes a stored requested scope set into the releases
// it names for each package, the way the coordinator wrote them: single targets
// name their package, batch targets list theirs in queries.
func plannedRotationReleases(t *testing.T, raw string) map[string]map[string]bool {
	t.Helper()
	var plan struct {
		Targets []struct {
			ScopeID string `json:"scope_id"`
			Package string `json:"package_name"`
			Queries []struct {
				Package string `json:"package_name"`
			} `json:"queries"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatalf("decode stored plan: %v", err)
	}
	out := map[string]map[string]bool{}
	for _, target := range plan.Targets {
		release := ""
		if _, query, ok := strings.Cut(target.ScopeID, "?"); ok {
			values, _ := url.ParseQuery(query)
			release = values.Get("release")
		}
		names := []string{}
		if target.Package != "" {
			names = append(names, target.Package)
		}
		for _, entry := range target.Queries {
			names = append(names, entry.Package)
		}
		for _, name := range names {
			if out[name] == nil {
				out[name] = map[string]bool{}
			}
			out[name][release] = true
		}
	}
	return out
}

// TestSupplyChainImpactReadinessDerivedRotationKeepsVerdictsLive is #10015 RED
// for the rotating plan, against the plan the coordinator really writes. A fleet
// of 450 Alpine OS package rows (150 packages on two releases) is larger than one
// target window, so the real reader and the real planner store a different slice
// of it every tick. The verdicts below depend on the state rows alone, so each
// must read the same in every window:
//
//	pkg-104  v3.17 fresh, v3.18 failed and newest  -> failed, naming v3.18 (on both releases)
//	pkg-200  v3.18 fresh, a stale legacy scope     -> fresh
//	pkg-250  v3.18 fresh, v3.17 failed, gone       -> failed, naming v3.17
//
// pkg-120 has no v3.17 state row, so it reads pending in exactly the windows whose
// plan names that release, and fresh in the rest.
//
// The planner only mints release scope ids once #9993 lands. Until then there is
// nothing for readiness to read from the plan, so the test skips with that reason.
func TestSupplyChainImpactReadinessDerivedRotationKeepsVerdictsLive(t *testing.T) {
	for _, layout := range []string{"hash", "image-grouped"} {
		t.Run(layout, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedRotationImage(t, ctx, db, now, "img-a", "3.17.3", 0, 149, layout)
			seedRotationImage(t, ctx, db, now, "img-b", "3.18.4", 0, 299, layout)
			configuration := `{"targets":[],"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["alpine"]}}`
			if _, err := db.ExecContext(ctx, `INSERT INTO collector_instances
        (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
        VALUES ($1, 'vulnerability_intelligence', 'continuous', TRUE, $2, $3, $3, $3)`, rotationInstance, configuration, now); err != nil {
				t.Fatalf("seed instance: %v", err)
			}
			instance := workflow.CollectorInstance{
				InstanceID: rotationInstance, CollectorKind: scope.CollectorVulnerabilityIntelligence,
				Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
				Configuration: configuration, LastObservedAt: now, CreatedAt: now, UpdatedAt: now,
			}
			seedRotationState(t, ctx, db, now.Add(-time.Hour), "pkg-104", "v3.17", "fresh")
			seedRotationState(t, ctx, db, now, "pkg-104", "v3.18", "failed")
			seedRotationState(t, ctx, db, now, "pkg-120", "v3.18", "fresh")
			seedRotationState(t, ctx, db, now.Add(-time.Hour), "pkg-200", "", "stale")
			seedRotationState(t, ctx, db, now, "pkg-200", "v3.18", "fresh")
			seedRotationState(t, ctx, db, now.Add(-time.Hour), "pkg-250", "v3.17", "failed")
			seedRotationState(t, ctx, db, now, "pkg-250", "v3.18", "fresh")

			store := storagepostgres.NewFactStore(storagepostgres.SQLDB{DB: db})
			interval := schedule.DefaultReconcileInterval
			targetLimit := vulnerability.DerivedTargetLimit(0)
			base := now.Truncate(interval)
			sliced := 0
			for window := 0; window < 9; window++ {
				observedAt := base.Add(time.Duration(window) * interval)
				rows, err := store.ListOSPackageAdvisoryTargets(ctx, workflow.OSPackageAdvisoryTargetFilter{
					Ecosystems:     []string{"alpine"},
					Limit:          schedule.DerivedTargetReadLimit(targetLimit),
					RotationOffset: schedule.DerivedTargetRotationOffsetForMode("", observedAt, interval, targetLimit),
				})
				if err != nil {
					t.Fatalf("window %d: read installed evidence: %v", window, err)
				}
				run, _, _, err := vulnerability.IntelligenceWorkPlanner{}.PlanVulnerabilityIntelligenceWork(ctx, vulnerability.IntelligencePlanRequest{
					Instance: instance, ObservedAt: observedAt, PlanKey: schedule.DerivedTargetPlanKey("continuous", observedAt, interval, ""),
					OSPackageTargets: rows,
				})
				if err != nil {
					t.Fatalf("window %d: plan: %v", window, err)
				}
				if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $6)
        ON CONFLICT (run_id) DO UPDATE SET requested_scope_set = EXCLUDED.requested_scope_set, updated_at = EXCLUDED.updated_at`,
					run.RunID, string(run.TriggerKind), string(run.Status), run.RequestedScopeSet, run.RequestedCollector, observedAt); err != nil {
					t.Fatalf("window %d: store plan: %v", window, err)
				}
				planned := plannedRotationReleases(t, run.RequestedScopeSet)
				if window == 0 && !strings.Contains(run.RequestedScopeSet, "release=") {
					t.Skip("the planner mints no release scope ids until #9993 lands, so the stored plan has no release for readiness to follow")
				}
				if len(planned["pkg-104"]) == 1 {
					sliced++
				}

				verdict := func(pkg string) (string, []string) {
					readiness := readDerivedReadiness(t, db, "package_id=os://os/"+pkg)
					var reasons []string
					for _, reason := range readiness.IncompleteReasons {
						if strings.HasPrefix(reason, "osv:") {
							reasons = append(reasons, reason)
						}
					}
					return onlyDerivedState(t, readiness).FreshnessState, reasons
				}
				if state, reasons := verdict("pkg-104"); state != "failed" || !slices.Contains(reasons, "osv:failed:alpine:v3.18") {
					t.Errorf("window %d: pkg-104 = %s %v, want failed naming v3.18 (plan names %v)", window, state, reasons, planned["pkg-104"])
				}
				if state, reasons := verdict("pkg-200"); state != "fresh" {
					t.Errorf("window %d: pkg-200 = %s %v, want fresh (plan names %v)", window, state, reasons, planned["pkg-200"])
				}
				if state, reasons := verdict("pkg-250"); state != "failed" || !slices.Contains(reasons, "osv:failed:alpine:v3.17") {
					t.Errorf("window %d: pkg-250 = %s %v, want failed naming v3.17 (plan names %v)", window, state, reasons, planned["pkg-250"])
				}
				wantState := "fresh"
				if planned["pkg-120"]["v3.17"] {
					wantState = "pending"
				}
				if state, reasons := verdict("pkg-120"); state != wantState {
					t.Errorf("window %d: pkg-120 = %s %v, want %s (plan names %v)", window, state, reasons, wantState, planned["pkg-120"])
				}
			}
			if sliced == 0 {
				t.Fatalf("no window named exactly one release for pkg-104: the fixture does not exercise a sliced plan (limit %d)", targetLimit)
			}
			t.Logf("%s: %d of 9 windows named one release of the two-release package pkg-104", layout, sliced)
		})
	}
}
