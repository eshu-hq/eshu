// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// Split out of drift_test.go (#5762 round 9, P3-4): drift_test.go was 483
// lines against the repo's 500-line cap, and this file holds one coherent
// check (checkJobNamesResolve, issue #5010) that does not need the rest of
// drift_test.go's fixtures beyond the shared helpers defined there
// (buildDriftRepo, minimalPreCommit, gateWith, minimalReg), which every
// _test.go file in this package already shares.

// writeWorkflow writes a single workflow file with explicit content under the
// repo's .github/workflows dir, for tests that need specific job names or
// append_gate displays rather than the default stub.
func writeWorkflow(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, ".github", "workflows", name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDriftCheck_CIJobNotInWorkflow(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.CI.Job = "Static Contract Gates" // the workflow TITLE, not a check name
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	errs := cigates.DriftCheck(root, reg)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "ci.job") && strings.Contains(e.Error(), "my-gate") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a ci.job resolution error for the workflow-title mismatch, got: %v", errs)
	}
}

func TestDriftCheck_CIJobMatchesAppendGateDisplay(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "matrix.yml", "name: Static Contract Gates\non: [push]\njobs:\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          append_gate \"${{ steps.filter.outputs.x }}\" \"x\" \"Verify X gate\" \"cmd\" \"cmd\"\n")
	g := gateWith("my-gate", "my-gate", "matrix.yml")
	g.CI.Job = "Verify X gate" // the append_gate display = the real check name
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("append_gate display should resolve, got: %v", errs)
	}
}

func TestDriftCheck_CIJobMatchesStaticJobName(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "det.yml", "name: Determinism\non: [push]\njobs:\n"+
		"  determinism-matrix:\n    name: determinism-matrix\n    runs-on: ubuntu-latest\n    steps: []\n")
	g := gateWith("my-gate", "my-gate", "det.yml")
	g.CI.Job = "determinism-matrix"
	reg := minimalReg([]cigates.Gate{g}, nil, nil)

	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("static job name should resolve, got: %v", errs)
	}
}

func TestDriftCheck_CIJobMatchesMatrixJobKey(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	// Matrix job whose name is a ${{ }} expression and NO append_gate: the only
	// resolvable check name is the job KEY, which exercises the key-fallback
	// branch distinctly from the append_gate-display path.
	writeWorkflow(t, root, "matrix2.yml", "name: Matrix\non: [push]\njobs:\n"+
		"  corpus-gate:\n    name: corpus-gate (${{ matrix.backend }})\n    runs-on: ubuntu-latest\n"+
		"    strategy:\n      matrix:\n        backend: [a, b]\n    steps: []\n")

	g := gateWith("my-gate", "my-gate", "matrix2.yml")
	g.CI.Job = "corpus-gate" // the job key (matrix name is dynamic, no append_gate)
	reg := minimalReg([]cigates.Gate{g}, nil, nil)
	if errs := cigates.DriftCheck(root, reg); len(errs) != 0 {
		t.Errorf("matrix job key should resolve via the key fallback, got: %v", errs)
	}

	// A non-existent key must still error — the fallback resolves the real key,
	// not a blanket pass for any matrix workflow.
	g.CI.Job = "wrong-key"
	reg = minimalReg([]cigates.Gate{g}, nil, nil)
	if errs := cigates.DriftCheck(root, reg); len(errs) == 0 {
		t.Error("a non-existent matrix job key must still error")
	}
}

func TestDriftCheck_ConcreteMatrixCheckNamesMustResolve(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), nil)
	writeWorkflow(t, root, "matrix.yml", "name: End-to-end Tests\non: [pull_request]\njobs:\n"+
		"  test:\n    name: test (${{ matrix.graph_backend }})\n    runs-on: ubuntu-latest\n"+
		"    strategy:\n      matrix:\n        include:\n          - graph_backend: nornicdb\n          - graph_backend: neo4j\n    steps: []\n")
	g := gateWith("my-gate", "my-gate", "matrix.yml")
	g.CI.Job = "test"
	g.CI.CheckNames = []string{"test (nornicdb)", "test (typo)"}

	errs := cigates.DriftCheck(root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) == 0 {
		t.Fatal("a declared concrete matrix check name that cannot run must fail drift")
	}
	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "test (typo)") && strings.Contains(err.Error(), "check_names") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unresolved concrete check_names error, got: %v", errs)
	}
}
