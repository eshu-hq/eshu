// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// checkRunnerWaitKeysAreExclusive mirrors
// TestIfaFamilyRegistryHandlerWaitKeysAreExclusive's rule
// (materialized_edge_family_blocker_shape_test.go:604-636), scoped instead to
// wait_stage=runner: no two runner-stage families may claim the same
// wait_key. The accounting failure is identical in kind to the handler-stage
// one that test's doc comment describes -- a shared wait_key would let one
// runner-stage non-vacuity proof be double-counted as evidence for two
// families -- just against shared_projection_intents.projection_domain (the
// runner queue) instead of fact_work_items.domain (the handler queue).
// Factored into its own pure function, rather than inlined in the test body,
// so a synthetic violating input can prove the assertion actually fires
// independent of whether any real wait_stage=runner row exists yet -- see
// TestIfaFamilyRegistryRunnerWaitKeysAreExclusiveCatchesDuplicate.
func checkRunnerWaitKeysAreExclusive(waitStages, waitKeys map[string]string) error {
	owner := make(map[string]string, len(waitKeys))
	families := make([]string, 0, len(waitStages))
	for family := range waitStages {
		families = append(families, family)
	}
	sort.Strings(families)

	for _, family := range families {
		if waitStages[family] != "runner" {
			continue
		}
		key := waitKeys[family]
		if key == "" {
			continue
		}
		if prior, clash := owner[key]; clash {
			return fmt.Errorf("families %q and %q both declare wait_stage=runner with wait_key=%q -- one shared runner-stage wait cannot be counted as a per-family proof for both. The wait_key owner keeps the runner-stage wait; the other family must prove its own seam (a distinct wait_key naming its own projection domain, or a blocker that engages a write only it performs).", prior, family, key)
		}
		owner[key] = family
	}
	return nil
}

// TestIfaFamilyRegistryRunnerWaitKeysAreExclusiveCatchesDuplicate is the
// deliberate-break tooth: it proves checkRunnerWaitKeysAreExclusive itself
// fires, and names both families, on a synthetic pair of wait_stage=runner
// rows sharing one wait_key. It stays in the suite even though
// handles_route/runs_in/invokes_cloud_action have since landed their own
// wait_stage=runner rows and made TestIfaFamilyRegistryRunnerWaitKeysAreExclusive
// itself exercised (see that test's doc comment): a synthetic, deterministic
// proof that the check logic can fail at all is worth keeping regardless of
// what the live registry happens to contain today, the same way
// TestMaterializedEdgeFamilyBlockerLockstepCatchesWrongTableDeclaration keeps
// its own synthetic tooth after the live bug it modeled was fixed.
func TestIfaFamilyRegistryRunnerWaitKeysAreExclusiveCatchesDuplicate(t *testing.T) {
	t.Parallel()

	waitStages := map[string]string{"handles_route": "runner", "runs_in": "runner", "invokes_cloud_action": "runner"}
	waitKeys := map[string]string{"handles_route": "handles_route", "runs_in": "handles_route", "invokes_cloud_action": "invokes_cloud_action"}

	err := checkRunnerWaitKeysAreExclusive(waitStages, waitKeys)
	if err == nil {
		t.Fatal("checkRunnerWaitKeysAreExclusive(two runner-stage families sharing one wait_key) = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "handles_route") || !strings.Contains(err.Error(), "runs_in") {
		t.Errorf("error %q does not name both colliding families", err.Error())
	}
	if strings.Contains(err.Error(), "invokes_cloud_action") {
		t.Errorf("error %q names invokes_cloud_action, which did not collide with anything", err.Error())
	}
}

// TestIfaFamilyRegistryRunnerWaitKeysAreExclusive runs
// checkRunnerWaitKeysAreExclusive against the real, live-parsed registry
// rows. As of this writing handles_route, runs_in, and invokes_cloud_action
// each declare wait_stage=runner with wait_key=<own family> (verified: three
// mutually distinct wait_keys), so this is a genuinely exercised, non-vacuous
// assertion, not merely a placeholder waiting for those rows to land. Unlike
// its handler-stage sibling (TestIfaFamilyRegistryHandlerWaitKeysAreExclusive),
// this test does not itself assert checked > 0: a handler-stage row was
// already guaranteed to exist before that test was written, making its own
// zero-checked guard a real parse-failure signal, whereas this test was
// written before any wait_stage=runner row existed and could not make the
// same guarantee about the future. Leaving that guard out here rather than
// asserting a stale non-zero count keeps this test honest if the registry
// ever changes shape again.
func TestIfaFamilyRegistryRunnerWaitKeysAreExclusive(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)

	if err := checkRunnerWaitKeysAreExclusive(waitStages, waitKeys); err != nil {
		t.Fatal(err)
	}
}
