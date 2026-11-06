// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10015 disposable PostgreSQL API proof that a package with several derived release scopes is covered only when every expected release is fresh, that a planned release with no state row reads pending, and that the reason names the uncovered release
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

const (
	curlPackageID = "package_id=os://os/curl"
	curlV317Scope = "vuln-intel://osv/alpine/curl?release=v3.17&version=7.87.0-r0"
	curlV318Scope = "vuln-intel://osv/alpine/curl?release=v3.18&version=7.87.0-r0"
	curlLegacy    = "vuln-intel://osv/alpine/curl?version=7.87.0-r0"
)

var curlKeys = [][2]string{{"os", "curl"}}

// plannedAlpineTarget is one single-package target the way the coordinator
// records it in a planner run's requested scope set.
func plannedAlpineTarget(scopeID, packageName string) map[string]any {
	return map[string]any{
		"scope_id": scopeID, "source": "osv", "ecosystem": "alpine",
		"package_name": packageName, "derived": true, "target_class": "owned_package",
	}
}

// plannedAlpineBatchTarget is one batch target: its package names live in
// queries and the target itself names no package.
func plannedAlpineBatchTarget(scopeID string, packageNames ...string) map[string]any {
	queries := make([]map[string]any, 0, len(packageNames))
	for _, name := range packageNames {
		queries = append(queries, map[string]any{"package_name": name, "version": "1.0.0-r0"})
	}
	return map[string]any{
		"scope_id": scopeID, "source": "osv", "ecosystem": "alpine",
		"queries": queries, "derived": true, "target_class": "owned_package",
	}
}

// seedPlannerRun records the latest planner run for instance, naming the
// targets it expects the collector to write state for and any skip evidence.
func seedPlannerRun(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, instance string, targets, skipped []map[string]any) {
	t.Helper()
	scopeSet, err := json.Marshal(map[string]any{
		"collector_instance_id": instance, "targets": targets, "skipped_targets": skipped,
	})
	if err != nil {
		t.Fatalf("marshal planner scope set: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflow_runs
        (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
        VALUES ($1, 'schedule', 'collection_complete', $2, 'vulnerability_intelligence', $3, $3)`,
		"vulnerability_intelligence:"+instance+":schedule:targets", scopeSet, now); err != nil {
		t.Fatalf("seed planner run for %s: %v", instance, err)
	}
}

func seedCurlState(t *testing.T, ctx context.Context, db *sql.DB, at time.Time, scope, freshness string) {
	t.Helper()
	terminal := map[string]string{"fresh": "succeeded", "stale": "succeeded", "failed": "failed_retryable"}[freshness]
	seedDerivedScope(t, ctx, db, at, "derived-alpine", scope, "alpine", freshness, terminal, curlKeys)
}

// testSupplyChainImpactReadinessDerivedEveryReleaseMustBeFreshLive is #10015
// RED: one fresh release must not hide another release's stale or failed scope
// of the same package, whether or not a planner run names the releases, and
// the reason must name the release that is not covered.
func testSupplyChainImpactReadinessDerivedEveryReleaseMustBeFreshLive(t *testing.T) {
	for _, tc := range []struct {
		name      string
		worse     string
		planned   bool
		wantState string
		reason    string
	}{
		{"failed older release with plan", "failed", true, "failed", "osv:failed:alpine:v3.17"},
		{"failed older release without plan", "failed", false, "failed", "osv:failed:alpine:v3.17"},
		{"stale older release with plan", "stale", true, "stale", "osv:stale:alpine:v3.17"},
		{"stale older release without plan", "stale", false, "stale", "osv:stale:alpine:v3.17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
			seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV317Scope, tc.worse)
			seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			if tc.planned {
				seedPlannerRun(t, ctx, db, now, "derived-alpine", []map[string]any{
					plannedAlpineTarget(curlV317Scope, "curl"), plannedAlpineTarget(curlV318Scope, "curl"),
				}, nil)
			}

			readiness := readDerivedReadiness(t, db, curlPackageID)
			state := onlyDerivedState(t, readiness)
			if state.FreshnessState != tc.wantState || state.ScopeCount != 2 || state.PackageCount != 1 ||
				state.ScopeID != "derived://osv/alpine" || !slices.Contains(state.ScopeIDs, curlV317Scope) {
				t.Fatalf("derived aggregate = %+v, want %s across two release scopes", state, tc.wantState)
			}
			if !slices.Contains(readiness.IncompleteReasons, tc.reason) {
				t.Fatalf("incomplete reasons = %v, want %s", readiness.IncompleteReasons, tc.reason)
			}
		})
	}
}

// testSupplyChainImpactReadinessDerivedPlannedReleaseWithoutStateReadsPendingLive
// is #10015 RED: a scope the latest plan expects but the collector has not
// written counts against coverage, for single and batch targets. The source
// state and key rows are written together, so the plan is the only evidence.
func testSupplyChainImpactReadinessDerivedPlannedReleaseWithoutStateReadsPendingLive(t *testing.T) {
	const batchV317 = "vuln-intel://osv/alpine/_batch?query=curl%407.87.0-r0&query=zlib%401.2.13-r1&release=v3.17"
	for _, tc := range []struct {
		name    string
		missing map[string]any
		scope   string
	}{
		{"single target", plannedAlpineTarget(curlV317Scope, "curl"), curlV317Scope},
		{"batch target", plannedAlpineBatchTarget(batchV317, "zlib", "curl"), batchV317},
		{"plan spells the name in another case", plannedAlpineTarget(curlV317Scope, " CURL "), curlV317Scope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
			seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			seedPlannerRun(t, ctx, db, now, "derived-alpine",
				[]map[string]any{tc.missing, plannedAlpineTarget(curlV318Scope, "curl")}, nil)

			readiness := readDerivedReadiness(t, db, curlPackageID)
			state := onlyDerivedState(t, readiness)
			if state.FreshnessState != "pending" || state.TerminalStatus != "pending" ||
				state.ScopeCount != 2 || state.FreshCount != 1 || state.UnmatchedPackageCount != 0 ||
				!slices.Contains(state.ScopeIDs, tc.scope) {
				t.Fatalf("derived aggregate = %+v, want pending naming %s", state, tc.scope)
			}
			if !slices.Contains(readiness.IncompleteReasons, "osv:pending:alpine:v3.17") {
				t.Fatalf("incomplete reasons = %v, want osv:pending:alpine:v3.17", readiness.IncompleteReasons)
			}
		})
	}
}

// testSupplyChainImpactReadinessDerivedPlannedReleasesCoverLive holds the
// negative side of #10015: these shapes must stay covered, or a fix that only
// demands "every row fresh" would read stale forever, because superseded and
// retired scopes are never deleted from the state table.
func testSupplyChainImpactReadinessDerivedPlannedReleasesCoverLive(t *testing.T) {
	const curlV318Older = "vuln-intel://osv/alpine/curl?release=v3.18&version=7.86.0-r0"
	for _, tc := range []struct {
		name       string
		seed       func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time)
		targets    []map[string]any
		wantScopes []string
	}{
		{
			name: "both releases fresh",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV317Scope, "fresh")
				seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			},
			targets:    []map[string]any{plannedAlpineTarget(curlV317Scope, "curl"), plannedAlpineTarget(curlV318Scope, "curl")},
			wantScopes: []string{curlV317Scope, curlV318Scope},
		},
		{
			name: "legacy scope with no release is ignored once the plan names releases",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now.Add(-time.Hour), curlLegacy, "failed")
				seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			},
			targets:    []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")},
			wantScopes: []string{curlV318Scope},
		},
		{
			name: "newest state wins inside one release",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV318Older, "failed")
				seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			},
			targets:    []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")},
			wantScopes: []string{curlV318Scope},
		},
		{
			name: "older version covers its release until the planned upgrade writes",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now, curlV318Older, "fresh")
			},
			targets:    []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")},
			wantScopes: []string{curlV318Older},
		},
		{
			name: "plan target for another source expects nothing",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			},
			targets: []map[string]any{
				plannedAlpineTarget(curlV318Scope, "curl"),
				{"scope_id": curlV317Scope, "source": "ghsa", "ecosystem": "alpine", "package_name": "curl"},
			},
			wantScopes: []string{curlV318Scope},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
			tc.seed(t, ctx, db, now)
			seedPlannerRun(t, ctx, db, now, "derived-alpine", tc.targets, nil)

			state := onlyDerivedState(t, readDerivedReadiness(t, db, curlPackageID))
			slices.Sort(state.ScopeIDs)
			if state.FreshnessState != "fresh" || state.ScopeCount != len(tc.wantScopes) ||
				state.FreshCount != len(tc.wantScopes) || !slices.Equal(state.ScopeIDs, tc.wantScopes) {
				t.Fatalf("derived aggregate = %+v, want fresh with exactly %v", state, tc.wantScopes)
			}
		})
	}
}

// testSupplyChainImpactReadinessDerivedUnqualifiedDistroKeepsNewestWinsLive
// pins the old single-scope behavior for a distro with no release qualifier:
// a planner run must not change it.
func testSupplyChainImpactReadinessDerivedUnqualifiedDistroKeepsNewestWinsLive(t *testing.T) {
	const (
		older = "vuln-intel://osv/debian/zlib1g?version=1.0.0"
		newer = "vuln-intel://osv/debian/zlib1g?version=1.1.0"
	)
	keys := [][2]string{{"os", "zlib1g"}}
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-debian", "debian")
	seedDerivedScope(t, ctx, db, now.Add(-time.Hour), "derived-debian", older, "debian", "failed", "failed_retryable", keys)
	seedDerivedScope(t, ctx, db, now, "derived-debian", newer, "debian", "fresh", "succeeded", keys)
	seedPlannerRun(t, ctx, db, now, "derived-debian", []map[string]any{
		{"scope_id": newer, "source": "osv", "ecosystem": "debian", "package_name": "zlib1g"},
	}, nil)

	state := onlyDerivedState(t, readDerivedReadiness(t, db, "package_id=os://os/zlib1g"))
	if state.FreshnessState != "fresh" || state.ScopeCount != 1 || !slices.Equal(state.ScopeIDs, []string{newer}) {
		t.Fatalf("derived aggregate = %+v, want only the newer debian scope", state)
	}
}

// testSupplyChainImpactReadinessDerivedPlanStaysWithItsInstanceLive proves one
// instance's plan creates no expectation for another instance: only the
// instance that planned the release reads pending.
func testSupplyChainImpactReadinessDerivedPlanStaysWithItsInstanceLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
	seedDerivedInstance(t, ctx, db, now, "derived-other", "alpine")
	seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
	seedDerivedScope(t, ctx, db, now, "derived-other", curlV318Scope, "alpine", "fresh", "succeeded", curlKeys)
	seedPlannerRun(t, ctx, db, now, "derived-other", []map[string]any{
		plannedAlpineTarget(curlV317Scope, "curl"), plannedAlpineTarget(curlV318Scope, "curl"),
	}, nil)

	got := map[string]derivedStateView{}
	for _, state := range readDerivedReadiness(t, db, curlPackageID).SourceStates {
		if state.Derived {
			got[state.CollectorInstanceID] = state
		}
	}
	if got["derived-alpine"].FreshnessState != "fresh" || got["derived-alpine"].ScopeCount != 1 {
		t.Fatalf("instance without the plan = %+v, want fresh with one scope", got["derived-alpine"])
	}
	if got["derived-other"].FreshnessState != "pending" || got["derived-other"].ScopeCount != 2 {
		t.Fatalf("instance with the plan = %+v, want pending with two scopes", got["derived-other"])
	}
}

// testSupplyChainImpactReadinessDerivedEmptyReleaseValueHasNoReleaseLive pins how
// the SPLIT_PART parse reads a scope whose first release value is empty: the
// first value decides, so ?release=&release=v3.18 has no release, where a regular
// expression would have skipped to v3.18. The coordinator writes one release
// value per scope, so this shape needs a hand-written row. A state row like this
// stands on its own and fails toward not covered. A plan target like this expects
// no release, so it creates no pending row and retires nothing.
func testSupplyChainImpactReadinessDerivedEmptyReleaseValueHasNoReleaseLive(t *testing.T) {
	const emptyFirst = "vuln-intel://osv/alpine/curl?release=&release=v3.18&version=7.86.0-r0"
	t.Run("state row stays out of the release it names second", func(t *testing.T) {
		ctx, db, now := readinessScopeLiveDatabase(t)
		seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
		seedCurlState(t, ctx, db, now.Add(-time.Hour), emptyFirst, "stale")
		seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")

		readiness := readDerivedReadiness(t, db, curlPackageID)
		state := onlyDerivedState(t, readiness)
		if state.FreshnessState != "stale" || state.ScopeCount != 2 || !slices.Contains(state.ScopeIDs, emptyFirst) {
			t.Fatalf("derived aggregate = %+v, want stale across both scopes", state)
		}
		if slices.Contains(readiness.IncompleteReasons, "osv:stale:alpine:v3.18") {
			t.Fatalf("incomplete reasons = %v, want no release named for a scope with no release", readiness.IncompleteReasons)
		}
	})
	t.Run("plan target expects no release", func(t *testing.T) {
		ctx, db, now := readinessScopeLiveDatabase(t)
		seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
		seedCurlState(t, ctx, db, now, curlV317Scope, "fresh")
		seedPlannerRun(t, ctx, db, now, "derived-alpine", []map[string]any{plannedAlpineTarget(emptyFirst, "curl")}, nil)

		state := onlyDerivedState(t, readDerivedReadiness(t, db, curlPackageID))
		if state.FreshnessState != "fresh" || state.ScopeCount != 1 || !slices.Equal(state.ScopeIDs, []string{curlV317Scope}) {
			t.Fatalf("derived aggregate = %+v, want only the fresh v3.17 scope", state)
		}
	})
}

// testSupplyChainImpactReadinessDerivedSkipDoesNotHideUncoveredReleaseLive
// proves a skipped package cannot turn a request with an uncovered release
// into the terminal skipped verdict: the failed and pending scopes keep the
// aggregate incomplete and named.
func testSupplyChainImpactReadinessDerivedSkipDoesNotHideUncoveredReleaseLive(t *testing.T) {
	for _, tc := range []struct {
		name      string
		seed      func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time)
		wantState string
		reason    string
	}{
		{
			name: "failed release",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV317Scope, "failed")
				seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			},
			wantState: "failed", reason: "osv:failed:alpine:v3.17",
		},
		{
			name: "stale release",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV317Scope, "stale")
				seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			},
			wantState: "stale", reason: "osv:stale:alpine:v3.17",
		},
		{
			name: "planned release with no state",
			seed: func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
				seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
			},
			wantState: "pending", reason: "osv:pending:alpine:v3.17",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
			tc.seed(t, ctx, db, now)
			seedPlannerRun(t, ctx, db, now, "derived-alpine",
				[]map[string]any{plannedAlpineTarget(curlV317Scope, "curl"), plannedAlpineTarget(curlV318Scope, "curl")},
				[]map[string]any{plannerSkippedTarget("derived_target_missing_release", "os", "zlib")})
			seedDerivedFacts(t, ctx, db, now, "consumption-scope", "reducer_package_consumption_correlation", []map[string]any{
				{"package_id": "os://os/curl", "ecosystem": "alpine", "package_name": "curl", "repository_id": "repository:r_10015"},
				{"package_id": "os://os/zlib", "ecosystem": "alpine", "package_name": "zlib", "repository_id": "repository:r_10015"},
			})

			readiness := readDerivedReadiness(t, db, "repository_id=repository:r_10015")
			state := onlyDerivedState(t, readiness)
			if state.FreshnessState != tc.wantState || state.UnmatchedPackageCount != 1 || state.SkippedPackageCount != 1 {
				t.Fatalf("derived aggregate = %+v, want %s with the skipped package still counted", state, tc.wantState)
			}
			if !slices.Contains(readiness.IncompleteReasons, tc.reason) {
				t.Fatalf("incomplete reasons = %v, want %s", readiness.IncompleteReasons, tc.reason)
			}
		})
	}
}

// TestSupplyChainImpactReadinessDerivedReleaseCoverageLive runs the release-coverage cases of #10015 as subtests, each on its own isolated schema, so the readiness shard plan counts one test for the family.
func TestSupplyChainImpactReadinessDerivedReleaseCoverageLive(t *testing.T) {
	t.Run("EveryReleaseMustBeFreshLive", testSupplyChainImpactReadinessDerivedEveryReleaseMustBeFreshLive)
	t.Run("PlannedReleaseWithoutStateReadsPendingLive", testSupplyChainImpactReadinessDerivedPlannedReleaseWithoutStateReadsPendingLive)
	t.Run("PlannedReleasesCoverLive", testSupplyChainImpactReadinessDerivedPlannedReleasesCoverLive)
	t.Run("UnqualifiedDistroKeepsNewestWinsLive", testSupplyChainImpactReadinessDerivedUnqualifiedDistroKeepsNewestWinsLive)
	t.Run("PlanStaysWithItsInstanceLive", testSupplyChainImpactReadinessDerivedPlanStaysWithItsInstanceLive)
	t.Run("EmptyReleaseValueHasNoReleaseLive", testSupplyChainImpactReadinessDerivedEmptyReleaseValueHasNoReleaseLive)
	t.Run("SkipDoesNotHideUncoveredReleaseLive", testSupplyChainImpactReadinessDerivedSkipDoesNotHideUncoveredReleaseLive)
}
