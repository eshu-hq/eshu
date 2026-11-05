// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10015 disposable PostgreSQL API proof that a derived package's release coverage is decided by the state rows alone, so the slice of releases a rotating planner run names cannot change a verdict, that a legacy scope with no release stops counting only once a real release state exists, and the aged-out reason, plan-distro binding and unqualified-group rules the statement relies on
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"
)

// replacePlannerRun makes plan the only planner run for instance, the way the
// next reconcile tick replaces the window the coordinator stored before it.
// A nil plan leaves the instance with no run at all.
func replacePlannerRun(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, instance string, plan []map[string]any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM workflow_runs WHERE requested_collector = 'vulnerability_intelligence'`); err != nil {
		t.Fatalf("clear planner runs: %v", err)
	}
	if plan != nil {
		seedPlannerRun(t, ctx, db, now, instance, plan, nil)
	}
}

// curlVerdict is everything a reader of the readiness answer sees about the
// derived osv source for the curl package, in a form two reads can be compared by.
type curlVerdict struct {
	State   string
	Scopes  int
	ScopeID string
	Reasons string
}

func readCurlVerdict(t *testing.T, db *sql.DB) (curlVerdict, derivedStateView) {
	t.Helper()
	readiness := readDerivedReadiness(t, db, curlPackageID)
	state := onlyDerivedState(t, readiness)
	scopes := slices.Clone(state.ScopeIDs)
	slices.Sort(scopes)
	var reasons []string
	for _, reason := range readiness.IncompleteReasons {
		if strings.HasPrefix(reason, "osv:") {
			reasons = append(reasons, reason)
		}
	}
	slices.Sort(reasons)
	return curlVerdict{
		State: state.FreshnessState, Scopes: state.ScopeCount,
		ScopeID: strings.Join(scopes, "|"), Reasons: strings.Join(reasons, ","),
	}, state
}

// TestSupplyChainImpactReadinessDerivedPlanSliceCannotHideReleaseLive is #10015
// RED for the rotating plan. The coordinator reads a limit-sized window of the
// installed fleet each tick and plans only the releases inside it, so a plan can
// name one release of a package that is installed on two. Whichever release the
// plan leaves out still has a state row, and that row must still count: a plan
// that does not name a release says nothing about whether it left the fleet.
func TestSupplyChainImpactReadinessDerivedPlanSliceCannotHideReleaseLive(t *testing.T) {
	for _, tc := range []struct {
		name       string
		older      string
		newer      string
		planned    string
		wantReason string
	}{
		{
			name:  "plan names only the fresh release while the other failed",
			older: "fresh", newer: "failed", planned: curlV317Scope, wantReason: "osv:failed:alpine:v3.18",
		},
		{
			name:  "plan names only the newer release while the older one failed",
			older: "failed", newer: "fresh", planned: curlV318Scope, wantReason: "osv:failed:alpine:v3.17",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
			seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV317Scope, tc.older)
			seedCurlState(t, ctx, db, now, curlV318Scope, tc.newer)
			seedPlannerRun(t, ctx, db, now, "derived-alpine", []map[string]any{plannedAlpineTarget(tc.planned, "curl")}, nil)

			readiness := readDerivedReadiness(t, db, curlPackageID)
			state := onlyDerivedState(t, readiness)
			if state.FreshnessState != "failed" || state.ScopeCount != 2 || state.FreshCount != 1 {
				t.Fatalf("derived aggregate = %+v, want failed across both release scopes", state)
			}
			if !slices.Contains(readiness.IncompleteReasons, tc.wantReason) {
				t.Fatalf("incomplete reasons = %v, want %s", readiness.IncompleteReasons, tc.wantReason)
			}
		})
	}
}

// TestSupplyChainImpactReadinessDerivedVerdictIgnoresPlanWindowLive is #10015
// RED: the same state rows must read the same under every plan the rotation can
// store, including no plan at all. Only a release with no state may follow the
// plan, and none is planned here.
func TestSupplyChainImpactReadinessDerivedVerdictIgnoresPlanWindowLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
	seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV317Scope, "fresh")
	seedCurlState(t, ctx, db, now, curlV318Scope, "failed")

	plans := []struct {
		name string
		plan []map[string]any
	}{
		{"plan names only v3.17", []map[string]any{plannedAlpineTarget(curlV317Scope, "curl")}},
		{"plan names only v3.18", []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")}},
		{"plan names neither release", []map[string]any{plannedAlpineTarget("vuln-intel://osv/alpine/zlib?release=v3.18&version=1.2.13-r1", "zlib")}},
		{"plan names both releases", []map[string]any{plannedAlpineTarget(curlV317Scope, "curl"), plannedAlpineTarget(curlV318Scope, "curl")}},
		{"no planner run", nil},
	}
	// The states say v3.18 failed, so every window must read failed across both
	// scopes and name v3.18. A single expected value is the equality check: if every
	// window matches it, every window matches every other.
	want := curlVerdict{
		State: "failed", Scopes: 2, Reasons: "osv:failed,osv:failed:alpine:v3.18",
		ScopeID: curlV317Scope + "|" + curlV318Scope,
	}
	for _, tc := range plans {
		replacePlannerRun(t, ctx, db, now, "derived-alpine", tc.plan)
		if got, _ := readCurlVerdict(t, db); got != want {
			t.Errorf("%s: verdict = %+v, want %+v", tc.name, got, want)
		}
	}
}

// TestSupplyChainImpactReadinessDerivedLegacyScopeStopsCountingOnStateLive is
// #10015 RED for the legacy half. A scope with no release parameter never covered
// a release, because OSV answers a bare Alpine query with no advisories. Once a
// real release state exists for the same instance, package and distro, the legacy
// row is dead weight that would read stale for good, whether or not any plan runs.
func TestSupplyChainImpactReadinessDerivedLegacyScopeStopsCountingOnStateLive(t *testing.T) {
	for _, legacy := range []string{"stale", "failed"} {
		t.Run("legacy "+legacy+" with a fresh release and no plan", func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
			seedCurlState(t, ctx, db, now.Add(-time.Hour), curlLegacy, legacy)
			seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")

			state := onlyDerivedState(t, readDerivedReadiness(t, db, curlPackageID))
			if state.FreshnessState != "fresh" || state.ScopeCount != 1 || !slices.Equal(state.ScopeIDs, []string{curlV318Scope}) {
				t.Fatalf("derived aggregate = %+v, want fresh with only the release scope", state)
			}
		})
	}
}

// TestSupplyChainImpactReadinessDerivedLegacyScopeNeverMasksReleaseLive guards the
// direction of that rule: a legacy row that is fresh and newest must not hide a
// failed release state, and a legacy row alone, with only a planned release that
// has no state yet, still reads pending.
func TestSupplyChainImpactReadinessDerivedLegacyScopeNeverMasksReleaseLive(t *testing.T) {
	t.Run("fresh newest legacy does not hide a failed release", func(t *testing.T) {
		ctx, db, now := readinessScopeLiveDatabase(t)
		seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
		seedCurlState(t, ctx, db, now.Add(-time.Hour), curlV318Scope, "failed")
		seedCurlState(t, ctx, db, now, curlLegacy, "fresh")

		readiness := readDerivedReadiness(t, db, curlPackageID)
		state := onlyDerivedState(t, readiness)
		if state.FreshnessState != "failed" || !slices.Contains(state.ScopeIDs, curlV318Scope) {
			t.Fatalf("derived aggregate = %+v, want failed naming the release scope", state)
		}
		if !slices.Contains(readiness.IncompleteReasons, "osv:failed:alpine:v3.18") {
			t.Fatalf("incomplete reasons = %v, want osv:failed:alpine:v3.18", readiness.IncompleteReasons)
		}
	})
	t.Run("legacy alone with a planned release reads pending", func(t *testing.T) {
		ctx, db, now := readinessScopeLiveDatabase(t)
		seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
		seedCurlState(t, ctx, db, now, curlLegacy, "fresh")
		seedPlannerRun(t, ctx, db, now, "derived-alpine", []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")}, nil)

		readiness := readDerivedReadiness(t, db, curlPackageID)
		state := onlyDerivedState(t, readiness)
		if state.FreshnessState != "pending" || !slices.Contains(state.ScopeIDs, curlV318Scope) {
			t.Fatalf("derived aggregate = %+v, want pending naming the planned release scope", state)
		}
		if !slices.Contains(readiness.IncompleteReasons, "osv:pending:alpine:v3.18") {
			t.Fatalf("incomplete reasons = %v, want osv:pending:alpine:v3.18", readiness.IncompleteReasons)
		}
	})
	t.Run("a planned release with no state does not retire a failed legacy scope", func(t *testing.T) {
		ctx, db, now := readinessScopeLiveDatabase(t)
		seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
		seedCurlState(t, ctx, db, now, curlLegacy, "failed")
		seedPlannerRun(t, ctx, db, now, "derived-alpine", []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")}, nil)

		state := onlyDerivedState(t, readDerivedReadiness(t, db, curlPackageID))
		if state.FreshnessState != "failed" || state.ScopeCount != 2 || !slices.Contains(state.ScopeIDs, curlLegacy) {
			t.Fatalf("derived aggregate = %+v, want failed across the legacy scope and the pending release", state)
		}
	})
}

// TestSupplyChainImpactReadinessDerivedAgedOutReleaseIsNamedStaleLive pins the
// reason for a state that is stored fresh but older than the freshness window:
// the aggregate reads stale, so the reason must name the aged verdict
// (osv:stale:alpine:v3.17) and not the stored one, or an operator is told a
// release is fresh while the package reads not covered.
func TestSupplyChainImpactReadinessDerivedAgedOutReleaseIsNamedStaleLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
	seedCurlState(t, ctx, db, now.Add(-15*24*time.Hour), curlV317Scope, "fresh")
	seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")

	readiness := readDerivedReadiness(t, db, curlPackageID)
	state := onlyDerivedState(t, readiness)
	if state.FreshnessState != "stale" || state.ScopeCount != 2 {
		t.Fatalf("derived aggregate = %+v, want stale across two release scopes", state)
	}
	for _, want := range []string{"osv:stale", "osv:stale:alpine:v3.17"} {
		if !slices.Contains(readiness.IncompleteReasons, want) {
			t.Fatalf("incomplete reasons = %v, want %s", readiness.IncompleteReasons, want)
		}
	}
	if slices.Contains(readiness.IncompleteReasons, "osv:fresh:alpine:v3.17") {
		t.Fatalf("incomplete reasons = %v, must not name the stored fresh state of an aged-out release", readiness.IncompleteReasons)
	}
}

// TestSupplyChainImpactReadinessDerivedPlanForAnotherDistroExpectsNothingLive
// pins that a plan target counts only for a distro the instance derives from:
// this instance derives alpine, so a debian release in the plan is not a scope
// it will ever write and must not read pending.
func TestSupplyChainImpactReadinessDerivedPlanForAnotherDistroExpectsNothingLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
	seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
	debian := plannedAlpineTarget("vuln-intel://osv/debian/curl?release=bookworm&version=7.88.1-10", "curl")
	debian["ecosystem"] = "debian"
	seedPlannerRun(t, ctx, db, now, "derived-alpine", []map[string]any{plannedAlpineTarget(curlV318Scope, "curl"), debian}, nil)

	readiness := readDerivedReadiness(t, db, curlPackageID)
	state := onlyDerivedState(t, readiness)
	if state.FreshnessState != "fresh" || state.ScopeCount != 1 || !slices.Equal(state.ScopeIDs, []string{curlV318Scope}) {
		t.Fatalf("derived aggregate = %+v, want fresh with only the alpine release scope", state)
	}
	if len(readiness.IncompleteReasons) != 0 {
		t.Fatalf("incomplete reasons = %v, want none", readiness.IncompleteReasons)
	}
}

// TestSupplyChainImpactReadinessDerivedUnqualifiedScopesShareOneGroupLive pins
// the documented limit that scopes with no release qualifier share one newest-wins
// group per package whatever their distro: a newer fresh Alpine scope hides an
// older failed Debian one. Only a release qualifier opens a group of its own.
func TestSupplyChainImpactReadinessDerivedUnqualifiedScopesShareOneGroupLive(t *testing.T) {
	const (
		debianOlder = "vuln-intel://osv/debian/curl?version=7.88.1-10"
		alpineNewer = "vuln-intel://osv/alpine/curl?version=7.87.0-r0"
	)
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-os", "alpine", "debian")
	seedDerivedScope(t, ctx, db, now.Add(-time.Hour), "derived-os", debianOlder, "debian", "failed", "failed_retryable", curlKeys)
	seedDerivedScope(t, ctx, db, now, "derived-os", alpineNewer, "alpine", "fresh", "succeeded", curlKeys)

	state := onlyDerivedState(t, readDerivedReadiness(t, db, curlPackageID))
	if state.FreshnessState != "fresh" || state.ScopeCount != 1 || !slices.Equal(state.ScopeIDs, []string{alpineNewer}) {
		t.Fatalf("derived aggregate = %+v, want only the newer unqualified scope", state)
	}
}

// TestSupplyChainImpactReadinessDerivedLegacyRuleStaysWithItsInstanceLive pins
// that a real release state retires a legacy scope only inside its own instance:
// another instance's release state for the same package and distro says nothing
// about this instance's legacy scope, which still fails the package.
func TestSupplyChainImpactReadinessDerivedLegacyRuleStaysWithItsInstanceLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
	seedDerivedInstance(t, ctx, db, now, "derived-other", "alpine")
	seedCurlState(t, ctx, db, now, curlV318Scope, "fresh")
	seedDerivedScope(t, ctx, db, now, "derived-other", curlLegacy, "alpine", "failed", "failed_retryable", curlKeys)

	got := map[string]derivedStateView{}
	for _, state := range readDerivedReadiness(t, db, curlPackageID).SourceStates {
		if state.Derived {
			got[state.CollectorInstanceID] = state
		}
	}
	if got["derived-alpine"].FreshnessState != "fresh" || got["derived-alpine"].ScopeCount != 1 {
		t.Fatalf("instance with the release state = %+v, want fresh with one scope", got["derived-alpine"])
	}
	if got["derived-other"].FreshnessState != "failed" || !slices.Equal(got["derived-other"].ScopeIDs, []string{curlLegacy}) {
		t.Fatalf("instance with only the legacy scope = %+v, want failed on that scope", got["derived-other"])
	}
}

// TestSupplyChainImpactReadinessDerivedPendingScopeIDsStayWithTheirInstanceLive
// pins that a pending row takes its scope id from its own instance's plan. Two
// instances plan different releases of one package with no state yet; each must
// report exactly its own planned scope, not the other's.
func TestSupplyChainImpactReadinessDerivedPendingScopeIDsStayWithTheirInstanceLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-alpine", "alpine")
	seedDerivedInstance(t, ctx, db, now, "derived-other", "alpine")
	seedPlannerRun(t, ctx, db, now, "derived-alpine", []map[string]any{plannedAlpineTarget(curlV317Scope, "curl")}, nil)
	seedPlannerRun(t, ctx, db, now, "derived-other", []map[string]any{plannedAlpineTarget(curlV318Scope, "curl")}, nil)

	got := map[string]derivedStateView{}
	for _, state := range readDerivedReadiness(t, db, curlPackageID).SourceStates {
		if state.Derived {
			got[state.CollectorInstanceID] = state
		}
	}
	for instance, want := range map[string]string{"derived-alpine": curlV317Scope, "derived-other": curlV318Scope} {
		state := got[instance]
		if state.FreshnessState != "pending" || state.ScopeCount != 1 || !slices.Equal(state.ScopeIDs, []string{want}) {
			t.Fatalf("instance %s = %+v, want pending on only %s", instance, state, want)
		}
	}
}
