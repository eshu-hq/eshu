package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// supplyChainDemoRoot is the canonical, human-facing demo corpus shipped under
// examples/supply-chain-demo. These tests treat that directory as the single
// source of truth so the runbook a reader follows and the fixtures the CLI
// exercises cannot drift apart.
const supplyChainDemoRoot = "../../../examples/supply-chain-demo"

// TestRunVulnScanRepoSupplyChainDemoVulnerableAppReachesReadyWithFindings proves
// the demo's vulnerable repository reaches ready_with_findings with the single
// synthetic CVE finding and the supply-chain exit code 3. The impact envelope is
// served by a stub so the assertion is fully offline and deterministic.
func TestRunVulnScanRepoSupplyChainDemoVulnerableAppReachesReadyWithFindings(t *testing.T) {
	repoID := "repo-supply-chain-demo-app"
	finding := map[string]any{
		"finding_id":        "finding-supply-chain-demo-app",
		"cve_id":            "CVE-2026-SYNTHETIC-NPM",
		"advisory_id":       "GHSA-synthetic-npm-0001",
		"package_id":        "npm:synthetic-vulnerable-npm",
		"package_name":      "synthetic-vulnerable-npm",
		"ecosystem":         "npm",
		"package_manager":   "npm",
		"impact_status":     "affected_exact",
		"observed_version":  "1.0.0",
		"fixed_version":     "1.0.1",
		"repository_id":     repoID,
		"dependency_scope":  "runtime",
		"direct_dependency": true,
		"dependency_depth":  1,
		"evidence_fact_ids": []any{"fact-supply-chain-demo-package", "fact-supply-chain-demo-advisory"},
	}
	tc := vulnScanFixtureMatrixCase{
		fixture:        "supply-chain-demo-app",
		repositoryID:   repoID,
		readinessState: "ready_with_findings",
		freshness:      "fresh",
		exitCode:       3,
		findings:       []map[string]any{finding},
		evidenceSources: []map[string]any{
			{"family": "package.consumption", "fact_count": 2, "freshness": "fresh"},
			{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
			{"family": "vulnerability.advisory", "fact_count": 4, "freshness": "fresh"},
		},
		sourceSnapshots: []map[string]any{
			{"source": "osv", "ecosystem": "npm", "freshness": "fresh", "complete": true},
		},
	}

	out, err := runSupplyChainDemoScan(t, filepath.Join(supplyChainDemoRoot, "app"), tc)
	requireVulnScanExitCode(t, err, tc.exitCode)
	data := requireMapField(t, decodeVulnScanPayload(t, out), "data")
	if got, want := data["repository_id"], repoID; got != want {
		t.Fatalf("data[repository_id] = %#v, want %#v", got, want)
	}
	if got := data["readiness_state"]; got != "ready_with_findings" {
		t.Fatalf("data[readiness_state] = %#v, want %q", got, "ready_with_findings")
	}
	report := requireMapField(t, data, "report")
	findings := requireSliceField(t, report, "findings")
	if len(findings) != 1 {
		t.Fatalf("report findings count = %d, want 1", len(findings))
	}
	first := findings[0].(map[string]any)
	if got := first["cve_id"]; got != "CVE-2026-SYNTHETIC-NPM" {
		t.Fatalf("finding cve_id = %#v, want %q", got, "CVE-2026-SYNTHETIC-NPM")
	}
}

// TestRunVulnScanRepoSupplyChainDemoMissingEvidenceRefuses proves the demo's
// missing-evidence repository reaches evidence_incomplete with
// missing_evidence=[advisory_sources] and exit code 4, demonstrating the
// refusal path the runbook walks.
func TestRunVulnScanRepoSupplyChainDemoMissingEvidenceRefuses(t *testing.T) {
	repoID := "repo-supply-chain-demo-missing-evidence"
	tc := vulnScanFixtureMatrixCase{
		fixture:        "supply-chain-demo-missing-evidence",
		repositoryID:   repoID,
		readinessState: "evidence_incomplete",
		freshness:      "fresh",
		exitCode:       4,
		evidenceSources: []map[string]any{
			{"family": "package.consumption", "fact_count": 1, "freshness": "fresh"},
			{"family": "package.registry", "fact_count": 1, "freshness": "fresh"},
		},
		missingEvidence:     []any{"advisory_sources"},
		wantMissingEvidence: "advisory_sources",
	}

	out, err := runSupplyChainDemoScan(t, filepath.Join(supplyChainDemoRoot, "missing-evidence"), tc)
	requireVulnScanExitCode(t, err, tc.exitCode)
	data := requireMapField(t, decodeVulnScanPayload(t, out), "data")
	if got := data["readiness_state"]; got != "evidence_incomplete" {
		t.Fatalf("data[readiness_state] = %#v, want %q", got, "evidence_incomplete")
	}
	report := requireMapField(t, data, "report")
	readiness := requireMapField(t, report, "readiness")
	if !sliceContainsString(requireSliceField(t, readiness, "missing_evidence"), "advisory_sources") {
		t.Fatalf("readiness missing_evidence = %#v, want to contain %q", readiness["missing_evidence"], "advisory_sources")
	}
}

// runSupplyChainDemoScan copies the demo repository at srcDir into a temporary
// git working tree, serves the matrix case's impact envelope from a stub, and
// runs the vuln-scan repo command with --json. It returns the captured stdout
// and the command error so callers can assert exit codes.
func runSupplyChainDemoScan(t *testing.T, srcDir string, tc vulnScanFixtureMatrixCase) (out *bytes.Buffer, err error) {
	t.Helper()
	if _, statErr := os.Stat(srcDir); statErr != nil {
		t.Fatalf("demo fixture %q missing: %v", srcDir, statErr)
	}
	reset := stubScanRuntime(t)
	defer reset()
	repoPath := copySupplyChainDemoTree(t, srcDir)
	server := vulnScanFixtureMatrixServer(t, repoPath, tc)
	defer server.Close()

	buf := &bytes.Buffer{}
	cmd := newTestVulnScanRepoCommand(t)
	cmd.SetOut(buf)
	cmd.SetErr(io.Discard)
	if setErr := cmd.Flags().Set("service-url", server.URL); setErr != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", setErr)
	}
	if setErr := cmd.Flags().Set("json", "true"); setErr != nil {
		t.Fatalf("Set(json) error = %v, want nil", setErr)
	}
	return buf, runVulnScanRepo(cmd, []string{repoPath})
}

// copySupplyChainDemoTree mirrors the demo repository into a temp directory and
// adds a .git marker so repository discovery treats it as a clone, matching the
// corpus harness behaviour.
func copySupplyChainDemoTree(t *testing.T, srcDir string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), filepath.Base(srcDir))
	if err := filepath.WalkDir(srcDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	}); err != nil {
		t.Fatalf("copy demo tree %q error = %v, want nil", srcDir, err)
	}
	if err := os.MkdirAll(filepath.Join(dst, ".git"), 0o755); err != nil {
		t.Fatalf("create demo .git error = %v, want nil", err)
	}
	return dst
}
