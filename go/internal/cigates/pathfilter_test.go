// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestDriftCheck_PathFilterCoverage_MissingFromWorkflowFilter reproduces the
// exact #5855 P1: a gate's registry trigger names a concrete script file, but
// the CI workflow's dorny/paths-filter block for that gate's key uses a glob
// that does not match it (dorny is gitignore-style: a single `*` does not
// cross `/`). A PR touching only that file would not select the gate's CI
// check, so the local-only pre-push hook is the sole enforcement -- exactly
// the drift the third review round of #5855 found.
func TestDriftCheck_PathFilterCoverage_MissingFromWorkflowFilter(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "matrix.yml", "name: Static Contract Gates\non: [push, pull_request]\njobs:\n"+
		"  changes:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - uses: dorny/paths-filter@v3\n"+
		"        with:\n"+
		"          filters: |\n"+
		"            telemetry:\n"+
		"              - 'scripts/*verify-telemetry-coverage*.sh'\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          append_gate \"${{ steps.filter.outputs.telemetry }}\" \"telemetry\" \"Verify telemetry coverage gate\" \"cmd\" \"cmd\"\n")

	g := gateWith("telemetry-coverage", "my-gate", "matrix.yml")
	g.CI.Job = "Verify telemetry coverage gate"
	g.Triggers = []string{"go/internal/**", "scripts/lib/telemetry-coverage-bucket-check.sh"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	errs := cigates.DriftCheck(root, reg)
	found := false
	for _, e := range errs {
		if e == nil {
			continue
		}
		if containsAll(e.Error(), "telemetry-coverage", "scripts/lib/telemetry-coverage-bucket-check.sh") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a path-filter-coverage drift error naming the uncovered literal trigger, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_CoveredByWorkflowFilter is the GREEN
// counterpart: once the workflow's filter gains a pattern that genuinely
// matches the literal trigger under dorny glob semantics, the check passes.
func TestDriftCheck_PathFilterCoverage_CoveredByWorkflowFilter(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "matrix.yml", "name: Static Contract Gates\non: [push, pull_request]\njobs:\n"+
		"  changes:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - uses: dorny/paths-filter@v3\n"+
		"        with:\n"+
		"          filters: |\n"+
		"            telemetry:\n"+
		"              - 'scripts/*verify-telemetry-coverage*.sh'\n"+
		"              - 'scripts/lib/telemetry-coverage-*.sh'\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          append_gate \"${{ steps.filter.outputs.telemetry }}\" \"telemetry\" \"Verify telemetry coverage gate\" \"cmd\" \"cmd\"\n")

	g := gateWith("telemetry-coverage", "my-gate", "matrix.yml")
	g.CI.Job = "Verify telemetry coverage gate"
	g.Triggers = []string{"go/internal/**", "scripts/lib/telemetry-coverage-bucket-check.sh"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("literal trigger matched by the filter glob should not drift, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_GlobTriggerSkipped proves a glob-form
// registry trigger (e.g. "go/internal/**") is never checked against the CI
// filter -- there is no single concrete path to test a glob against, so this
// check must not false-positive on one, regardless of what the workflow's
// filter contains.
func TestDriftCheck_PathFilterCoverage_GlobTriggerSkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "matrix.yml", "name: Static Contract Gates\non: [push, pull_request]\njobs:\n"+
		"  changes:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - uses: dorny/paths-filter@v3\n"+
		"        with:\n"+
		"          filters: |\n"+
		"            telemetry:\n"+
		"              - 'totally-unrelated/*.sh'\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          append_gate \"${{ steps.filter.outputs.telemetry }}\" \"telemetry\" \"Verify telemetry coverage gate\" \"cmd\" \"cmd\"\n")

	g := gateWith("telemetry-coverage", "my-gate", "matrix.yml")
	g.CI.Job = "Verify telemetry coverage gate"
	g.Triggers = []string{"go/internal/**", "go/cmd/**"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("glob-form triggers must be skipped, not false-flagged, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_NonMatrixWorkflowSkipped proves a gate
// whose CI workflow does not use the dorny/paths-filter matrix pattern (no
// such step at all) is skipped entirely -- this check only fires where the
// gate-to-filter-key mapping is unambiguous.
func TestDriftCheck_PathFilterCoverage_NonMatrixWorkflowSkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"scripts/some-script.sh"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("a workflow with no dorny/paths-filter step must be skipped, got: %v", errs)
	}
}

// TestDriftCheck_PathFilterCoverage_UnresolvedKeySkipped proves a gate whose
// ci.job resolves to a real check (a plain job's static name, so
// checkJobNamesResolve is satisfied) but does NOT match any append_gate
// display is skipped by the path-filter check specifically -- the
// gate-to-dorny-key mapping cannot be determined for a non-matrix job, so
// this check must not false-flag it.
func TestDriftCheck_PathFilterCoverage_UnresolvedKeySkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "matrix.yml", "name: Static Contract Gates\non: [push, pull_request]\njobs:\n"+
		"  changes:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - uses: dorny/paths-filter@v3\n"+
		"        with:\n"+
		"          filters: |\n"+
		"            telemetry:\n"+
		"              - 'scripts/*verify-telemetry-coverage*.sh'\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          append_gate \"${{ steps.filter.outputs.telemetry }}\" \"telemetry\" \"Verify telemetry coverage gate\" \"cmd\" \"cmd\"\n"+
		"  plain-job:\n    name: A Plain Non-Matrix Check\n    runs-on: ubuntu-latest\n    steps: []\n")

	g := gateWith("my-gate", "my-gate", "matrix.yml")
	g.CI.Job = "A Plain Non-Matrix Check"
	g.Triggers = []string{"scripts/lib/telemetry-coverage-bucket-check.sh"}
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("a gate whose ci.job resolves to no dorny filter key must be skipped, got: %v", errs)
	}
}

// containsAll reports whether s contains every one of substrs.
func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
