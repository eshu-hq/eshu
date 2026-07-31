// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// ifGatedWorkflowRaw builds a dorny workflow whose filter step lives in the
// `changes` job, plus one arbitrary job block supplied by the caller.
func ifGatedWorkflowRaw(job string) string {
	return "name: Test\non: [push, pull_request]\njobs:\n" +
		"  changes:\n    runs-on: ubuntu-latest\n    outputs:\n" +
		"      code: ${{ steps.filter.outputs.code }}\n    steps:\n" +
		"      - uses: dorny/paths-filter@v3\n        id: filter\n" +
		"        with:\n          filters: |\n" +
		"            code:\n              - 'go/**'\n" + job
}

func uncoveredGate() cigates.Gate {
	g := gateWith("license-header", "my-gate", "test.yml")
	g.CI.Job = "verify-contracts"
	g.Triggers = []string{"sdk/go/x.go"}
	return g
}

// TestDriftCheck_PathFilterCoverage_IfGatedWrongProducerJobNotResolved:
// resolving on the output KEY alone ignores which job produces it. A job
// gated on a DIFFERENT job's `code` output is not path-gated by the dorny
// filter at all, so treating it as such compares the gate's triggers against a
// filter that does not describe when the job runs (#5546 review).
func TestDriftCheck_PathFilterCoverage_IfGatedWrongProducerJobNotResolved(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", ifGatedWorkflowRaw(
		"  setup:\n    runs-on: ubuntu-latest\n    outputs:\n      code: ${{ steps.x.outputs.code }}\n    steps:\n      - run: echo\n"+
			"  verify-contracts:\n    runs-on: ubuntu-latest\n    needs: [changes, setup]\n"+
			"    if: ${{ needs.setup.outputs.code == 'true' }}\n    steps:\n      - run: echo\n",
	))

	reg := minimalReg([]cigates.Gate{uncoveredGate()}, nil, nil)
	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("job gated on a different job's output must not resolve to the dorny key, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_IfGatedNegativeConditionNotResolved:
// `== 'false'` selects the job when the paths did NOT change, so the filter
// does not describe when it runs. Matching the output reference without its
// comparison treats that as positive selection (#5546 review).
func TestDriftCheck_PathFilterCoverage_IfGatedNegativeConditionNotResolved(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "test.yml", ifGatedWorkflowRaw(
		"  verify-contracts:\n    runs-on: ubuntu-latest\n    needs: changes\n"+
			"    if: ${{ needs.changes.outputs.code == 'false' }}\n    steps:\n      - run: echo\n",
	))

	reg := minimalReg([]cigates.Gate{uncoveredGate()}, nil, nil)
	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("negative selection condition must not resolve to the dorny key, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_IfGatedDuplicateDisplayNameAmbiguous: two
// if-gated jobs sharing a `name:` but resolving to different filter keys write
// the same map entry, and Go's randomised job-map iteration decides which
// survives -- a nondeterministic pass or failure. This is the same collision
// appendGateKeysByDisplay already reports for append_gate displays, and it must
// be reported here too rather than silently overwritten (#5546 review).
func TestDriftCheck_PathFilterCoverage_IfGatedDuplicateDisplayNameAmbiguous(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	wf := "name: Test\non: [push, pull_request]\njobs:\n" +
		"  changes:\n    runs-on: ubuntu-latest\n    outputs:\n" +
		"      code: ${{ steps.filter.outputs.code }}\n      docs: ${{ steps.filter.outputs.docs }}\n    steps:\n" +
		"      - uses: dorny/paths-filter@v3\n        id: filter\n" +
		"        with:\n          filters: |\n" +
		"            code:\n              - 'go/**'\n            docs:\n              - 'docs/**'\n" +
		"  a:\n    runs-on: ubuntu-latest\n    name: Shared Display\n    needs: changes\n" +
		"    if: ${{ needs.changes.outputs.code == 'true' }}\n    steps:\n      - run: echo\n" +
		"  b:\n    runs-on: ubuntu-latest\n    name: Shared Display\n    needs: changes\n" +
		"    if: ${{ needs.changes.outputs.docs == 'true' }}\n    steps:\n      - run: echo\n"
	writeWorkflow(t, root, "test.yml", wf)

	g := gateWith("license-header", "my-gate", "test.yml")
	g.CI.Job = "Shared Display"
	// Matched by NEITHER filter (code: go/**, docs: docs/**). That is what
	// makes the second assertion below able to bite: if the code fell through
	// to the trigger comparison after detecting ambiguity, whichever key won
	// the map race would report a mismatch. A trigger the surviving filter
	// happens to match would look clean either way, so the test could not tell
	// the two behaviours apart (#5546 review).
	g.Triggers = []string{"sdk/go/x.go"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	foundAmbiguous, foundMismatch := false, false
	for _, e := range cigates.DriftCheck(root, reg) {
		if e == nil {
			continue
		}
		msg := e.Error()
		if containsAll(msg, "license-header", "Shared Display") {
			foundAmbiguous = true
		}
		if containsAll(msg, "license-header", "sdk/go/x.go") {
			foundMismatch = true
		}
	}
	if !foundAmbiguous {
		t.Errorf("a display name resolving to two different filter keys must be reported as ambiguous")
	}
	if foundMismatch {
		t.Errorf("an ambiguous display must skip the trigger comparison, not fall through to it")
	}
}
