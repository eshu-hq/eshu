// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencecontinuity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateTriggerContract returns a contract whose only go-test proof refs name
// ./internal/query, the package the fixtures below cover or uncover.
func gateTriggerContract() Contract {
	return Contract{
		Version: "v1",
		Rows: []Row{{
			ID:         "trigger-coverage-row",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
			SourceFact: ProofRef{Ref: "go test ./internal/query -run '^(TestSomething)$' -count=1"},
		}},
	}
}

// writeGateTriggerFixture lays out a minimal repo root: a ci-gates registry
// whose evidence-continuity gate carries registryTriggers, and a
// static-contract-gates workflow whose evidence filter carries filterPaths.
func writeGateTriggerFixture(t *testing.T, registryTriggers, filterPaths []string) string {
	t.Helper()

	root := t.TempDir()
	var registry strings.Builder
	registry.WriteString("version: v1\ngates:\n  - id: evidence-continuity\n    name: Verify Evidence Continuity\n    category: exactness\n    tier: pre-pr\n    blocking: true\n    triggers:\n")
	for _, trigger := range registryTriggers {
		registry.WriteString("      - \"" + trigger + "\"\n")
	}
	registry.WriteString("    local:\n      command: \"bash scripts/verify-evidence-continuity.sh\"\n      test_command: \"bash scripts/test-verify-evidence-continuity.sh\"\n    ci:\n      workflow: static-contract-gates.yml\n      job: \"Verify evidence continuity gate\"\n    requirements:\n      - go\n    ci_only_reason: \"\"\n")
	mustWriteFile(t, filepath.Join(root, "specs", "ci-gates.v1.yaml"), registry.String())

	var workflow strings.Builder
	workflow.WriteString("jobs:\n  changes:\n    steps:\n      - uses: dorny/paths-filter@v3\n        with:\n          filters: |\n            evidence:\n")
	for _, filterPath := range filterPaths {
		workflow.WriteString("              - '" + filterPath + "'\n")
	}
	mustWriteFile(t, filepath.Join(root, ".github", "workflows", "static-contract-gates.yml"), workflow.String())
	return root
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// validatorInputTriggerGlobs are trigger globs that span every entry in
// validatorInputAnchors. They live beside the anchors so adding a validator
// input updates both, and the "fully covered" fixtures below build from them
// rather than each repeating a literal list -- otherwise a new anchor turns
// every clean fixture red and the tempting fix is to trim the anchor list.
var validatorInputTriggerGlobs = []string{
	contractSpecPath,
	"specs/capability-matrix.v1.yaml",
	"specs/capability-matrix/**",
}

// fullyCoveredTriggers returns pkgGlobs plus a glob for every validator input,
// i.e. the trigger set of a repository with no blind spot at all.
func fullyCoveredTriggers(pkgGlobs ...string) []string {
	return append(append([]string{}, pkgGlobs...), validatorInputTriggerGlobs...)
}

// TestValidatorInputGlobsCoverEveryAnchor closes the loop between the two
// lists. Without it, adding an anchor and forgetting its glob would make every
// clean fixture fail, and adding a glob for an anchor that does not exist would
// go unnoticed -- either way the fixtures would stop describing a covered repo.
func TestValidatorInputGlobsCoverEveryAnchor(t *testing.T) {
	if len(validatorInputAnchors) == 0 {
		t.Fatal("validatorInputAnchors is empty; the anchor check would evaluate nothing")
	}
	for _, anchor := range validatorInputAnchors {
		if !oneGlobMatchesAll(validatorInputTriggerGlobs, []string{anchor}) {
			t.Errorf("no glob in validatorInputTriggerGlobs selects anchor %q; a clean fixture would report it as a gap", anchor)
		}
	}
}

func TestValidatorGateTriggerCoverage_CoveredPackageClean(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		fullyCoveredTriggers("go/internal/query/**"),
		fullyCoveredTriggers("go/internal/query/**"),
	)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got:\n%s", FormatFindings(findings))
	}
}

func TestValidatorGateTriggerCoverage_UncoveredRegistryTriggerReported(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/collector/**"},
		[]string{"go/internal/query/**"},
	)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "go/internal/query")
}

// The spec file itself must stay in both trigger sets: it is the anchor that
// guarantees this check runs on the edit that could create a blind spot. The
// needle is the "itself" tail so this cannot pass on a package-gap finding
// that merely mentions the registry path in its label.
func TestValidatorGateTriggerCoverage_MissingSpecAnchorReported(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/query/**"},
		[]string{"go/internal/query/**", "specs/evidence-continuity.v1.yaml"},
	)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "selects specs/evidence-continuity.v1.yaml")
}

// TestValidatorGateTriggerCoverage_MissingCapabilityMatrixAnchorReported pins
// the anchors that were missing. ValidateRepository reads the capability matrix
// and its fragments as well as the contract spec, but only the contract was
// anchored, so a trigger set that dropped `specs/capability-matrix/**` passed
// this check green. A capability-id rename would then surface as
// `unknown_capability` on an unrelated pull request instead of here -- the
// blind-spot class this gate exists to close, re-opened one input over.
//
// The fixture covers the contract spec deliberately, so the only thing that can
// produce a finding is the capability-matrix anchor: a test that left both
// uncovered would pass on the contract's finding alone and prove nothing about
// the new ones.
func TestValidatorGateTriggerCoverage_MissingCapabilityMatrixAnchorReported(t *testing.T) {
	covered := []string{
		"go/internal/query/**",
		"specs/evidence-continuity.v1.yaml",
	}
	root := writeGateTriggerFixture(t, covered, covered)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "selects specs/capability-matrix.v1.yaml")
	mustContainFinding(t, findings, FindingGateTriggerGap, "selects specs/capability-matrix/")

	for _, f := range findings {
		if strings.Contains(f.Message, "selects specs/evidence-continuity.v1.yaml") {
			t.Fatalf("contract spec is covered by the fixture yet still reported: %s", f.Message)
		}
	}
}

// TestValidatorGateTriggerCoverage_AllValidatorInputsAnchored is the totality
// half: every path in validatorInputAnchors must actually be checked. Asserting
// two named findings would still pass if a third anchor were added to the list
// and never consulted, which is how an anchor list quietly becomes decorative.
func TestValidatorGateTriggerCoverage_AllValidatorInputsAnchored(t *testing.T) {
	root := writeGateTriggerFixture(t, []string{"go/internal/query/**"}, []string{"go/internal/query/**"})

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	if len(validatorInputAnchors) == 0 {
		t.Fatal("validatorInputAnchors is empty; the anchor check would evaluate nothing")
	}
	for _, anchor := range validatorInputAnchors {
		mustContainFinding(t, findings, FindingGateTriggerGap, "selects "+anchor)
	}
}

func TestValidatorGateTriggerCoverage_UncoveredWorkflowFilterReported(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/query/**"},
		[]string{"go/internal/collector/**"},
	)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "static-contract-gates.yml")
}

// A filename-narrowed glob must not count as package coverage: it can match
// one probe name while excluding every other _test.go file in the package,
// which is exactly the shape that would let a test rename slip past the gate.
func TestValidatorGateTriggerCoverage_FilenameNarrowedGlobRejected(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/query/a*_test.go"},
		[]string{"go/internal/query/**"},
	)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	mustContainFinding(t, findings, FindingGateTriggerGap, "go/internal/query")
}

// A `dir/*_test.go` glob spans every test file directly in the package dir,
// which is the exact file set this validator reads, so it counts as coverage.
func TestValidatorGateTriggerCoverage_TestFileGlobAccepted(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		fullyCoveredTriggers("go/internal/query/*_test.go"),
		fullyCoveredTriggers("go/internal/query/**"),
	)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got:\n%s", FormatFindings(findings))
	}
}

func TestValidatorGateTriggerCoverage_MissingGateEntryFailsLoudly(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/query/**"},
		[]string{"go/internal/query/**"},
	)
	registry := "version: v1\ngates:\n  - id: some-other-gate\n    name: Some Other Gate\n    category: exactness\n    tier: pre-pr\n    blocking: true\n    triggers:\n      - \"go/internal/query/**\"\n    local:\n      command: \"bash scripts/x.sh\"\n      test_command: \"bash scripts/test-x.sh\"\n    ci:\n      workflow: static-contract-gates.yml\n      job: \"Some other gate\"\n    requirements:\n      - go\n    ci_only_reason: \"\"\n"
	mustWriteFile(t, filepath.Join(root, "specs", "ci-gates.v1.yaml"), registry)

	if _, err := validateGateTriggerCoverage(root, gateTriggerContract()); err == nil {
		t.Fatal("expected an error for a registry without the evidence-continuity gate")
	}
}

func TestValidatorGateTriggerCoverage_MissingEvidenceFilterFailsLoudly(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/query/**"},
		[]string{"go/internal/query/**"},
	)
	workflow := "jobs:\n  changes:\n    steps:\n      - uses: dorny/paths-filter@v3\n        with:\n          filters: |\n            other:\n              - 'go/internal/query/**'\n"
	mustWriteFile(t, filepath.Join(root, ".github", "workflows", "static-contract-gates.yml"), workflow)

	if _, err := validateGateTriggerCoverage(root, gateTriggerContract()); err == nil {
		t.Fatal("expected an error for a workflow without an evidence filter")
	}
}

// A workflow that sets `predicate-quantifier: 'every'` on the dorny step
// changes the filter semantics from "ANY pattern selects the file" to "ALL
// patterns must match". oneGlobMatchesAll proves coverage under the ANY
// reading only, so the check must refuse to evaluate under `every` rather
// than report coverage it did not prove. Three sibling workflows already set
// `every`, so this is one copy-paste away from static-contract-gates.yml.
func TestValidatorGateTriggerCoverage_EveryQuantifierFailsLoudly(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/query/**", "specs/evidence-continuity.v1.yaml"},
		[]string{"go/internal/query/**", "specs/evidence-continuity.v1.yaml"},
	)
	workflow := "jobs:\n  changes:\n    steps:\n      - uses: dorny/paths-filter@v3\n        with:\n          predicate-quantifier: 'every'\n          filters: |\n            evidence:\n              - 'go/internal/query/**'\n              - 'specs/evidence-continuity.v1.yaml'\n"
	mustWriteFile(t, filepath.Join(root, ".github", "workflows", "static-contract-gates.yml"), workflow)

	if _, err := validateGateTriggerCoverage(root, gateTriggerContract()); err == nil {
		t.Fatal("expected an error for a workflow whose dorny step sets predicate-quantifier: every")
	}
}

// Gate-scope findings describe the gate's trigger wiring, not a matrix row,
// so RowID must stay empty: every other finding uses RowID as a contract row
// id, and a consumer mapping RowID back to a row would otherwise get the gate
// id in a slot that promises a row id.
func TestValidatorGateTriggerCoverage_GateScopeFindingsLeaveRowIDEmpty(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/collector/**"},
		[]string{"go/internal/collector/**"},
	)

	findings, err := validateGateTriggerCoverage(root, gateTriggerContract())
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("expected gate_trigger_gap findings for an uncovered package")
	}
	for _, finding := range findings {
		if finding.RowID != "" {
			t.Fatalf("gate-scope finding carries RowID %q; want empty (the message names the gap, RowID is reserved for matrix row ids)", finding.RowID)
		}
	}
}

func TestValidatorGateTriggerCoverage_NoGoTestRefsNoReads(t *testing.T) {
	contract := Contract{
		Version: "v1",
		Rows: []Row{{
			ID:         "script-proof-row",
			Domain:     "code_to_cloud",
			Capability: "platform_impact.deployment_chain",
			SourceFact: ProofRef{Ref: "bash scripts/run-remote-e2e-check.sh"},
		}},
	}

	// No fixture files exist under this root; the check must not need them
	// when the contract references no go-test packages.
	findings, err := validateGateTriggerCoverage(t.TempDir(), contract)
	if err != nil {
		t.Fatalf("validateGateTriggerCoverage() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got:\n%s", FormatFindings(findings))
	}
}
