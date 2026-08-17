// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coveredExcept returns a trigger set covering every anchor and the referenced
// package except the named globs, so a test can prove one anchor is
// load-bearing without riding on another anchor's finding.
func coveredExcept(pkgGlob string, dropped ...string) []string {
	skip := map[string]struct{}{}
	for _, d := range dropped {
		skip[d] = struct{}{}
	}
	globs := []string{pkgGlob}
	for _, glob := range anchorTriggerGlobs() {
		if _, ok := skip[glob]; ok {
			continue
		}
		globs = append(globs, glob)
	}
	return globs
}

// TestValidatorGateTriggerCoverage_GateRegistryAnchorReported pins the input
// this check itself reads. validateGateTriggerCoverage opens
// specs/ci-gates.v1.yaml to learn its own gate's triggers, so an edit there
// decides what this gate reports -- yet nothing else in the repo requires a
// gate to trigger on the registry. checkPathFilterCoverage cross-checks the
// registry's triggers against the workflow filter, which stays satisfied when a
// path is dropped from BOTH sides. So without this anchor, one PR could remove
// the registry from both trigger sets and pass every gate, and the next PR
// narrowing a package trigger would never run this check at all -- the #6131
// blind spot re-opened on the input the fix introduced.
func TestValidatorGateTriggerCoverage_GateRegistryAnchorReported(t *testing.T) {
	covered := coveredExcept("go/internal/query/**", gateRegistryPath)
	root := writeGateTriggerFixture(t, covered, covered)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "selects "+gateRegistryPath)
	for _, f := range findings {
		if !strings.Contains(f.Message, gateRegistryPath) {
			t.Fatalf("every other input is covered by the fixture yet something else was reported: %s", f.Message)
		}
	}
}

// TestValidatorGateTriggerCoverage_GateWorkflowAnchorReported is the same claim
// for the other file this check reads. The workflow carries the dorny filter
// that selects the gate in CI, so an edit to it can narrow the gate's reach
// directly; a trigger set that does not name it lets that edit land without
// running the check that would catch it.
func TestValidatorGateTriggerCoverage_GateWorkflowAnchorReported(t *testing.T) {
	covered := coveredExcept("go/internal/query/**", gateWorkflowPath)
	root := writeGateTriggerFixture(t, covered, covered)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "selects "+gateWorkflowPath)
	for _, f := range findings {
		if !strings.Contains(f.Message, gateWorkflowPath) {
			t.Fatalf("every other input is covered by the fixture yet something else was reported: %s", f.Message)
		}
	}
}

// TestValidatorGateTriggerCoverage_FilenameNarrowedFragmentGlobRejected is the
// directory-family half of the filename-narrowing rule
// TestValidatorGateTriggerCoverage_FilenameNarrowedGlobRejected pins for
// packages. loadCapabilities reads every *.yaml directly in
// specs/capability-matrix/, so a trigger of "specs/capability-matrix/a*.yaml"
// leaves most fragments outside the gate's reach. A single probe path could not
// tell that glob from a directory-wide one -- it would match "a.yaml" and read
// as coverage -- which is why the fragment family carries two differently named
// probes.
func TestValidatorGateTriggerCoverage_FilenameNarrowedFragmentGlobRejected(t *testing.T) {
	covered := append(
		coveredExcept("go/internal/query/**", capabilityFragmentDir+"/**"),
		capabilityFragmentDir+"/a*.yaml",
	)
	root := writeGateTriggerFixture(t, covered, covered)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "selects "+capabilityFragmentDir+"/")
	for _, f := range findings {
		if !strings.Contains(f.Message, capabilityFragmentDir+"/") {
			t.Fatalf("every other input is covered by the fixture yet something else was reported: %s", f.Message)
		}
	}
}

// TestValidatorInputPathsExistInRepo keeps the anchor paths tied to the files
// they claim to anchor. The anchors are string constants, so a rename of a spec
// or of the generated inventory would leave them pointing at a path that no
// longer exists -- the trigger check would still pass (the stale path stays
// covered) while the real input went unwatched. Resolving each one against this
// repository turns that drift into a test failure.
func TestValidatorInputPathsExistInRepo(t *testing.T) {
	root := repoRoot(t)

	for _, rel := range []string{
		contractSpecPath,
		capabilityMatrixPath,
		surfaceInventoryPath,
		gateRegistryPath,
		gateWorkflowPath,
	} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("validator input %s does not resolve in this repo: %v", rel, err)
			continue
		}
		if info.IsDir() {
			t.Errorf("validator input %s is a directory; the anchor claims a file", rel)
		}
	}

	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(capabilityFragmentDir)))
	if err != nil {
		t.Fatalf("capability matrix fragment dir %s does not resolve in this repo: %v", capabilityFragmentDir, err)
	}
	if !info.IsDir() {
		t.Fatalf("capability matrix fragment dir %s is not a directory", capabilityFragmentDir)
	}
}
