// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// goPkgTriggerErrs runs DriftCheck and returns only the Go-package self-trigger
// findings, so a case asserts on its own rule rather than on whatever else the
// fixture repo happens to trip.
func goPkgTriggerErrs(t *testing.T, root string, reg *cigates.Registry) []string {
	t.Helper()
	var out []string
	for _, err := range cigates.DriftCheck(root, reg) {
		if msg := err.Error(); strings.Contains(msg, "runs Go package") {
			out = append(out, msg)
		}
	}
	return out
}

// goPkgGate builds a one-gate fixture registry whose local command is cmd.
func goPkgGate(triggers []string, cmd, testCmd string) *cigates.Registry {
	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = triggers
	g.Local = &cigates.Local{Command: cmd, TestCommand: testCmd}
	return minimalReg([]cigates.Gate{g}, nil, nil)
}

// THE TEETH. Every gate in the committed registry that compiles or runs a Go
// package must be selected by an edit inside that package. This is the property
// #5873 asked for and the one that decays silently: the 19 Go-implemented gates
// are invisible to checkScriptTriggerCoverage, which only derives "scripts/"
// tokens, so before this check two of them did not select on an edit to their
// own implementation. Seed a defect by deleting a gate's own package trigger
// from specs/ci-gates.v1.yaml and this test names the gate.
func TestDriftCheck_CommittedRegistryGoPackagesTriggerTheirOwnGate(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	reg, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}

	if errs := goPkgTriggerErrs(t, repoRoot, reg); len(errs) != 0 {
		t.Errorf("committed registry has %d gate(s) that do not select on an edit to their own Go package:\n%s",
			len(errs), strings.Join(errs, "\n"))
	}
}

// The core false-green: a gate runs a Go program that none of its triggers
// matches, so editing that program does not select the gate. This is
// product-claim-ledger's exact shape before the fix — it ran
// `go run ./cmd/capability-inventory -mode product-claims` while triggering
// only on specs/*.yaml.
func TestDriftCheck_GoPackageNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate([]string{"specs/thing.v1.yaml"}, "cd go && go run ./cmd/thing -mode verify", "")

	errs := goPkgTriggerErrs(t, root, reg)
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 go-package trigger error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "go/cmd/thing") || !strings.Contains(errs[0], "local.command") {
		t.Errorf("error should name the field and the package, got: %s", errs[0])
	}
}

// The same gate goes clean once the package is among its triggers.
func TestDriftCheck_GoPackageCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate([]string{"specs/thing.v1.yaml", "go/cmd/thing/**"}, "cd go && go run ./cmd/thing -mode verify", "")

	if errs := goPkgTriggerErrs(t, root, reg); len(errs) != 0 {
		t.Errorf("expected no go-package trigger errors, got %d: %v", len(errs), errs)
	}
}

// A trigger narrowed to individual files inside the package does NOT cover a
// package-level `go test`: the command compiles every file in the package, so
// an edit to a sibling helper changes what the gate does while selecting
// nothing. This is ifa-materialized-edge-coverage's shape before the fix.
func TestDriftCheck_PerFileTriggersDoNotCoverPackageTest(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate(
		[]string{"go/internal/thing/materialized_edges*.go"},
		"cd go && go test ./internal/thing -run TestEdges -count=1", "")

	errs := goPkgTriggerErrs(t, root, reg)
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 go-package trigger error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "go/internal/thing") {
		t.Errorf("error should name the package, got: %s", errs[0])
	}
}

// A trigger narrowed to the package's Go sources still counts: every file the
// command compiles matches it.
func TestDriftCheck_GoSourceGlobCoversPackage(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate([]string{"go/internal/thing/*.go"}, "cd go && go test ./internal/thing -count=1", "")

	if errs := goPkgTriggerErrs(t, root, reg); len(errs) != 0 {
		t.Errorf("expected no go-package trigger errors, got %d: %v", len(errs), errs)
	}
}

// A recursive spec compiles nested packages too, so a trigger that only reaches
// the top directory does not cover it. race-graph-writes and go-build both use
// this form.
func TestDriftCheck_RecursiveSpecNeedsNestedCoverage(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})

	shallow := goPkgGate([]string{"go/cmd/thing/*.go"}, "cd go && go test ./cmd/thing/... -count=1", "")
	if errs := goPkgTriggerErrs(t, root, shallow); len(errs) != 1 {
		t.Fatalf("a non-recursive trigger should not cover ./...; got %d errors: %v", len(errs), errs)
	}

	deep := goPkgGate([]string{"go/cmd/thing/**"}, "cd go && go test ./cmd/thing/... -count=1", "")
	if errs := goPkgTriggerErrs(t, root, deep); len(errs) != 0 {
		t.Errorf("a recursive trigger should cover ./...; got %v", errs)
	}
}

// `cd` is honoured mid-command, not only as a prefix: ci-gate-registry reaches
// its own package through a subshell late in a compound test_command, and
// missing that would report a package the gate does in fact trigger on.
func TestDriftCheck_MidCommandSubshellCdIsResolved(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/test-thing.sh", "#!/usr/bin/env bash\ntrue\n")
	reg := goPkgGate(
		[]string{"scripts/test-thing.sh", "go/internal/thing/**"},
		"", "bash scripts/test-thing.sh && (cd go && go test ./internal/thing -count=1)")

	if errs := goPkgTriggerErrs(t, root, reg); len(errs) != 0 {
		t.Errorf("mid-command subshell cd should resolve to go/internal/thing, got: %v", errs)
	}
}

// `go list` reports on packages without building or running them, so a change
// inside a merely-listed package cannot alter the gate's verdict and must not
// be demanded as a trigger. The sdk-go-* gates end in `go list -deps ./...`.
func TestDriftCheck_GoListPackagesAreNotDemanded(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate([]string{"sdk/go/thing/**"},
		"cd sdk/go/thing && go build ./... && go list -deps ./... | rg -q eshu", "")

	if errs := goPkgTriggerErrs(t, root, reg); len(errs) != 0 {
		t.Errorf("go list should not demand its own trigger, got: %v", errs)
	}
}

// A command with no Go package argument has nothing to assert.
func TestDriftCheck_NonGoCommandYieldsNoPackageFindings(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/verify-thing.sh", "#!/usr/bin/env bash\ntrue\n")
	reg := goPkgGate([]string{"scripts/verify-thing.sh"}, "bash scripts/verify-thing.sh", "")

	if errs := goPkgTriggerErrs(t, root, reg); len(errs) != 0 {
		t.Errorf("expected no go-package trigger errors, got: %v", errs)
	}
}

// The repository root is not a package worth asserting on: every path is inside
// it, so the property is vacuous rather than violated. A gate run from the repo
// root with `go test ./...` must not produce a finding demanding a `/**`
// trigger, which is not a legal glob here.
func TestDriftCheck_RepoRootPackageSpecIsVacuous(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate([]string{"go/**"}, "go test ./... -count=1", "")

	if errs := goPkgTriggerErrs(t, root, reg); len(errs) != 0 {
		t.Errorf("repo-root package spec should be vacuous, got: %v", errs)
	}
}

// A package argument that escapes the repository names no in-repo file, so it
// is skipped rather than reported as uncovered.
func TestDriftCheck_EscapingPackageArgSkipped(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate([]string{"go/**"}, "cd go && go test ./../../outside -count=1", "")

	if errs := goPkgTriggerErrs(t, root, reg); len(errs) != 0 {
		t.Errorf("escaping package arg should be skipped, got: %v", errs)
	}
}

// Every package in a multi-package command is checked, not just the first.
// race-graph-writes names nine, and ifa-materialized-edge-coverage's second
// package (./internal/reducer) was the one missing a trigger.
func TestDriftCheck_EveryPackageInCompoundCommandChecked(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	reg := goPkgGate([]string{"go/internal/covered/**"},
		"cd go && go test ./internal/covered -count=1 && go test ./internal/uncovered -count=1", "")

	errs := goPkgTriggerErrs(t, root, reg)
	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error for the uncovered second package, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "go/internal/uncovered") {
		t.Errorf("error should name the uncovered package, got: %s", errs[0])
	}
}
