// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// gateWithJob returns a minimal gate declaring the given (workflow, job)
// pair, reusing gateWith's neutral Local/Triggers shape (drift_test.go) so
// this file's cases exercise only CIScriptTriggerCoverageSummary's own
// counting logic, not checkCIScriptTriggerCoverage's script-walk narrowings.
func gateWithJob(id, workflow, job string) cigates.Gate {
	g := gateWith(id, "", workflow)
	g.CI.Job = job
	return g
}

// TestCIScriptTriggerCoverageSummary_Empty proves an empty registry reports
// zero attributable and zero skipped, not a nil-slice panic or a spurious
// count.
func TestCIScriptTriggerCoverageSummary_Empty(t *testing.T) {
	t.Parallel()

	attributable, skipped, sharedPairs := cigates.CIScriptTriggerCoverageSummary(minimalReg(nil, nil, nil))
	if attributable != 0 || skipped != 0 || len(sharedPairs) != 0 {
		t.Fatalf("empty registry: got attributable=%d skipped=%d sharedPairs=%v, want all zero",
			attributable, skipped, sharedPairs)
	}
}

// TestCIScriptTriggerCoverageSummary_AllUnique proves gates with distinct
// (workflow, job) pairs are all attributable and none are skipped — the
// common case checkCIScriptTriggerCoverage actually walks.
func TestCIScriptTriggerCoverageSummary_AllUnique(t *testing.T) {
	t.Parallel()

	reg := minimalReg([]cigates.Gate{
		gateWithJob("gate-a", "a.yml", "job-a"),
		gateWithJob("gate-b", "b.yml", "job-b"),
	}, nil, nil)

	attributable, skipped, sharedPairs := cigates.CIScriptTriggerCoverageSummary(reg)
	if attributable != 2 || skipped != 0 || len(sharedPairs) != 0 {
		t.Fatalf("all-unique registry: got attributable=%d skipped=%d sharedPairs=%v, want attributable=2 skipped=0 none shared",
			attributable, skipped, sharedPairs)
	}
}

// TestCIScriptTriggerCoverageSummary_SharedPairSkipped proves gates on a
// shared (workflow, job) pair are counted as skipped, not attributable, and
// that the pair is reported exactly once with every member gate ID named and
// sorted — the exact shape checkCIScriptTriggerCoverage itself skips
// silently (#6149 follow-up item 8 review, P2(a)).
func TestCIScriptTriggerCoverageSummary_SharedPairSkipped(t *testing.T) {
	t.Parallel()

	reg := minimalReg([]cigates.Gate{
		gateWithJob("gate-c", "shared.yml", "shared-job"),
		gateWithJob("gate-a", "shared.yml", "shared-job"),
		gateWithJob("gate-b", "shared.yml", "shared-job"),
		gateWithJob("gate-solo", "solo.yml", "solo-job"),
	}, nil, nil)

	attributable, skipped, sharedPairs := cigates.CIScriptTriggerCoverageSummary(reg)
	if attributable != 1 {
		t.Errorf("attributable = %d, want 1 (gate-solo only)", attributable)
	}
	if skipped != 3 {
		t.Errorf("skipped = %d, want 3 (gate-a, gate-b, gate-c)", skipped)
	}
	want := []string{"shared.yml/shared-job (3 gates: gate-a, gate-b, gate-c)"}
	if !reflect.DeepEqual(sharedPairs, want) {
		t.Errorf("sharedPairs = %v, want %v (one entry, IDs sorted, reported once not three times)", sharedPairs, want)
	}
}

// TestCIScriptTriggerCoverageSummary_NoCIJobExcluded proves a gate with no
// CI job at all (local-only, e.g. prepr-stamp-verify-selftest) is neither
// attributable nor skipped -- it is outside this summary's universe, the
// same as it is outside checkCIScriptTriggerCoverage's walk.
func TestCIScriptTriggerCoverageSummary_NoCIJobExcluded(t *testing.T) {
	t.Parallel()

	localOnly := gateWithJob("local-only", "", "")
	localOnly.CI = cigates.CI{}
	reg := minimalReg([]cigates.Gate{
		localOnly,
		gateWithJob("gate-a", "a.yml", "job-a"),
	}, nil, nil)

	attributable, skipped, sharedPairs := cigates.CIScriptTriggerCoverageSummary(reg)
	if attributable != 1 || skipped != 0 || len(sharedPairs) != 0 {
		t.Fatalf("got attributable=%d skipped=%d sharedPairs=%v, want attributable=1 (local-only gate excluded entirely)",
			attributable, skipped, sharedPairs)
	}
}
