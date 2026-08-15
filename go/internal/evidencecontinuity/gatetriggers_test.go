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

func TestValidatorGateTriggerCoverage_CoveredPackageClean(t *testing.T) {
	root := writeGateTriggerFixture(
		t,
		[]string{"go/internal/query/**", "specs/evidence-continuity.v1.yaml"},
		[]string{"go/internal/query/**", "specs/evidence-continuity.v1.yaml"},
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
	mustContainFinding(t, findings, FindingGateTriggerGap, "specs/evidence-continuity.v1.yaml itself")
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
		[]string{"go/internal/query/*_test.go", "specs/evidence-continuity.v1.yaml"},
		[]string{"go/internal/query/**", "specs/evidence-continuity.v1.yaml"},
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
