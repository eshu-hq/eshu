// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateRepoArmGolden rewrites the executable-text golden for the readiness
// repository arms. Use it only for a deliberate change to those statements:
//
//	go test ./internal/query/supply/chain/impact -run TestReadinessRepoArmExecutableTextIsPinned -update-repo-arm-golden
var updateRepoArmGolden = flag.Bool(
	"update-repo-arm-golden",
	false,
	"rewrite testdata/readiness_repo_arm_probes.golden from the shipped SQL",
)

const repoArmGoldenPath = "testdata/readiness_repo_arm_probes.golden"

// repoArmExecutableText renders the executable SQL of the two guarded CTEs:
// comments stripped, whitespace collapsed, one section per CTE.
func repoArmExecutableText(t *testing.T) string {
	t.Helper()
	return "## package_manifest_active\n" + collapseProbeText(readinessPackageManifestActiveCTE) + "\n" +
		"## package_dependency_gap_active\n" + collapseProbeText(repoArmGapCTE(t)) + "\n"
}

// TestReadinessRepoArmExecutableTextIsPinned pins every executable token of the
// three LATERAL probes in package_manifest_active and package_dependency_gap_active.
// The individual needle guards in this package bind the conditions an operator
// is most likely to touch; this pin binds the rest. Dropping the scope bind,
// fact_kind, source_system or entity_type from one probe breaks partial-index
// predicate implication for migrations 121 and 159 and sends the probe back to
// scanning the anchored scope, and dropping the outer generation join can
// duplicate rows. None of that changes the query's text-level shape that the
// needles look for. The live plan proof that would catch it is scheduled-class
// and runs in no workflow, so this golden is the CI protection.
//
// A deliberate edit to either statement must regenerate the golden and carry
// the plan proof that justifies it, so a reviewer sees the change.
func TestReadinessRepoArmExecutableTextIsPinned(t *testing.T) {
	t.Parallel()

	got := repoArmExecutableText(t)
	if *updateRepoArmGolden {
		if err := os.MkdirAll(filepath.Dir(repoArmGoldenPath), 0o750); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(repoArmGoldenPath, []byte(got), 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(repoArmGoldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", repoArmGoldenPath, err)
	}
	if got != string(want) {
		t.Errorf("the executable SQL of the readiness repository arms changed.\n"+
			"first difference: %s\n"+
			"If the change is deliberate, regenerate with -update-repo-arm-golden and attach the plan proof "+
			"(docs/internal/evidence/7088-readiness-repo-scope.md).", firstDifference(got, string(want)))
	}
}

// firstDifference returns a short window around the first byte at which two
// texts differ, so a failure names the changed condition.
func firstDifference(got, want string) string {
	limit := len(got)
	if len(want) < limit {
		limit = len(want)
	}
	i := 0
	for i < limit && got[i] == want[i] {
		i++
	}
	window := func(s string) string {
		start := i - 60
		if start < 0 {
			start = 0
		}
		end := i + 100
		if end > len(s) {
			end = len(s)
		}
		return strings.TrimSpace(s[start:end])
	}
	return "got ..." + window(got) + "... want ..." + window(want) + "..."
}
