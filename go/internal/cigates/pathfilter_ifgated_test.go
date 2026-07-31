// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// ifGatedWorkflow builds a workflow in the shape test.yml / security-scan.yml /
// mcp-schema-drift.yml actually use: a `changes` job running
// dorny/paths-filter, and a real job selected by an `if:` referencing that
// job's output, rather than by the append_gate matrix dispatch that
// static-contract-gates.yml uses.
func ifGatedWorkflow(filters, jobKey, jobName, cond string) string {
	wf := "name: Test\non: [push, pull_request]\njobs:\n" +
		"  changes:\n    runs-on: ubuntu-latest\n    outputs:\n" +
		"      code: ${{ steps.filter.outputs.code }}\n    steps:\n" +
		"      - uses: dorny/paths-filter@v3\n        id: filter\n" +
		"        with:\n          filters: |\n" + filters +
		"  " + jobKey + ":\n    runs-on: ubuntu-latest\n"
	if jobName != "" {
		wf += "    name: " + jobName + "\n"
	}
	wf += "    needs: changes\n    if: " + cond + "\n    steps:\n      - run: echo hi\n"
	return wf
}

const codeFilterNarrow = "            code:\n              - 'go/**'\n"

// TestDriftCheck_PathFilterCoverage_IfGatedJobMissingTrigger is the #5546
// direction #5855 left open. checkPathFilterCoverage resolved a gate's dorny
// filter key ONLY through append_gate, so it covered the matrix-dispatch
// workflow and silently skipped every gate whose job is selected by an `if:`
// on a paths-filter output instead. That is not a rare shape: 30 registry
// gates sit in such workflows, including the 11 blocking gates behind
// test.yml's verify-contracts. For all of them the trigger-vs-filter
// comparison never ran, so the registry could claim a trigger the CI filter
// would not select and nothing noticed.
func TestDriftCheck_PathFilterCoverage_IfGatedJobMissingTrigger(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", ifGatedWorkflow(
		codeFilterNarrow, "verify-contracts", "",
		"${{ needs.changes.outputs.code == 'true' }}",
	))

	g := gateWith("license-header", "my-gate", "test.yml")
	g.CI.Job = "verify-contracts"
	g.Triggers = []string{"sdk/go/collector/conformance/manifest.go"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	found := false
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "license-header", "sdk/go/collector/conformance/manifest.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected drift naming the trigger the if-gated job's filter does not select")
	}
}

// TestDriftCheck_PathFilterCoverage_IfGatedJobCovered is the GREEN counterpart:
// a trigger the if-gated job's filter does select must not drift.
func TestDriftCheck_PathFilterCoverage_IfGatedJobCovered(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", ifGatedWorkflow(
		codeFilterNarrow, "verify-contracts", "",
		"${{ needs.changes.outputs.code == 'true' }}",
	))

	g := gateWith("license-header", "my-gate", "test.yml")
	g.CI.Job = "verify-contracts"
	g.Triggers = []string{"go/internal/cigates/pathfilter.go"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("trigger covered by the if-gated job's filter should not drift, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_IfGatedJobResolvedByDisplayName covers the
// registry naming a job by its `name:` rather than its YAML key, which is what
// security-scan.yml's gates do (ci.job "gosec (Go static analysis)" for job key
// "gosec").
func TestDriftCheck_PathFilterCoverage_IfGatedJobResolvedByDisplayName(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "security-scan.yml", ifGatedWorkflow(
		codeFilterNarrow, "gosec", "gosec (Go static analysis)",
		"${{ needs.changes.outputs.code == 'true' }}",
	))

	g := gateWith("gosec-changed", "my-gate", "security-scan.yml")
	g.CI.Job = "gosec (Go static analysis)"
	g.Triggers = []string{"sdk/go/collector/conformance/manifest.go"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	found := false
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "gosec-changed", "sdk/go/collector/conformance/manifest.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the gate to resolve via the job's name: field and drift")
	}
}

// TestDriftCheck_PathFilterCoverage_NegatedPatternMatchesFaithfully pins
// dorny's real semantics for a `!` pattern. dorny compiles each pattern
// separately with picomatch and, under the default predicate-quantifier
// ("some"), includes a file if ANY compiled pattern returns true. picomatch
// treats a leading `!` as a negated matcher, so '!docs/**' is TRUE for a path
// outside docs/ and FALSE for one inside it.
//
// Treating '!docs/**' as an ordinary literal (the previous behaviour) makes it
// match nothing, so a filter written only in exclusions looked empty and every
// trigger under it drifted. This asserts the trigger below is selected, which
// it genuinely is in CI.
func TestDriftCheck_PathFilterCoverage_NegatedPatternMatchesFaithfully(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", ifGatedWorkflow(
		"            code:\n              - '!docs/**'\n", "verify-contracts", "",
		"${{ needs.changes.outputs.code == 'true' }}",
	))

	g := gateWith("license-header", "my-gate", "test.yml")
	g.CI.Job = "verify-contracts"
	g.Triggers = []string{"go/internal/cigates/pathfilter.go"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("a path outside the negated pattern IS selected by dorny; expected no drift, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_PredicateQuantifierEvery covers the other
// dorny quantifier. With `predicate-quantifier: 'every'` a file must satisfy
// EVERY pattern, which is the mode where a `!` exclusion genuinely subtracts:
// 'docs/x.md' fails '!docs/**' and is therefore NOT selected, so a gate
// triggering on it is drift. Modelling only the default "some" would make this
// checker silently disagree with CI the moment a workflow opts in -- which is
// one of the two fixes proposed for #5896, so it is not hypothetical.
func TestDriftCheck_PathFilterCoverage_PredicateQuantifierEvery(t *testing.T) {
	t.Parallel()

	wf := "name: Test\non: [push, pull_request]\njobs:\n" +
		"  changes:\n    runs-on: ubuntu-latest\n    outputs:\n" +
		"      code: ${{ steps.filter.outputs.code }}\n    steps:\n" +
		"      - uses: dorny/paths-filter@v3\n        id: filter\n" +
		"        with:\n          predicate-quantifier: 'every'\n          filters: |\n" +
		"            code:\n              - '**'\n              - '!docs/**'\n" +
		"  verify-contracts:\n    runs-on: ubuntu-latest\n    needs: changes\n" +
		"    if: ${{ needs.changes.outputs.code == 'true' }}\n    steps:\n      - run: echo hi\n"

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", wf)

	g := gateWith("docs-refs", "my-gate", "test.yml")
	g.CI.Job = "verify-contracts"
	g.Triggers = []string{"docs/public/reference/ci-gates.md"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	found := false
	for _, e := range cigates.DriftCheck(root, reg) {
		if e != nil && containsAll(e.Error(), "docs-refs", "docs/public/reference/ci-gates.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("under predicate-quantifier 'every' the excluded path is not selected; expected drift")
	}
}
