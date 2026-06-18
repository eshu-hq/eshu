package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// supplyChainDemoRoot is the canonical, human-facing demo corpus shipped under
// examples/supply-chain-demo. These tests treat that directory as the single
// source of truth so the runbook a reader follows and the fixtures the CLI
// exercises cannot drift apart.
const supplyChainDemoRoot = "../../../examples/supply-chain-demo"

const supplyChainDemoVulnerablePackage = "synthetic-vulnerable-npm"

// TestRunVulnScanRepoSupplyChainDemoVulnerableAppReachesReadyWithFindings proves
// the demo's vulnerable repository reaches ready_with_findings with the single
// synthetic CVE finding and the supply-chain exit code 3.
//
// The impact envelope is served by a stub because promoting an advisory to a
// finding requires owned advisory facts that only the Compose stack carries, so
// it cannot run in a unit test. To stop the stub from making the assertion a
// tautology, the finding's package and version are read FROM the fixture
// manifest and lockfile: if the demo repo ever stops declaring the synthetic
// vulnerable dependency, requireNpmFixtureDependency fails the test.
func TestRunVulnScanRepoSupplyChainDemoVulnerableAppReachesReadyWithFindings(t *testing.T) {
	appDir := filepath.Join(supplyChainDemoRoot, "app")
	lockedVersion := requireNpmFixtureDependency(t, appDir, supplyChainDemoVulnerablePackage)

	repoID := "repo-supply-chain-demo-app"
	finding := map[string]any{
		"finding_id":        "finding-supply-chain-demo-app",
		"cve_id":            "CVE-2026-SYNTHETIC-NPM",
		"advisory_id":       "GHSA-synthetic-npm-0001",
		"package_id":        "npm:" + supplyChainDemoVulnerablePackage,
		"package_name":      supplyChainDemoVulnerablePackage,
		"ecosystem":         "npm",
		"package_manager":   "npm",
		"impact_status":     "affected_exact",
		"observed_version":  lockedVersion,
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

	out, err := runSupplyChainDemoScan(t, appDir, tc)
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
	// Couple the finding back to the fixture: the package name and the version
	// reported must be the ones the demo manifest/lockfile actually declare.
	pkg := requireMapField(t, first, "package")
	if got := pkg["package_name"]; got != supplyChainDemoVulnerablePackage {
		t.Fatalf("finding package_name = %#v, want %q", got, supplyChainDemoVulnerablePackage)
	}
	affected := requireMapField(t, first, "affected")
	if got := affected["observed_version"]; got != lockedVersion {
		t.Fatalf("finding observed_version = %#v, want fixture-locked %q", got, lockedVersion)
	}
}

// TestRunVulnScanRepoSupplyChainDemoMissingEvidenceRefuses proves the demo's
// missing-evidence repository reaches evidence_incomplete with
// missing_evidence=[advisory_sources] and exit code 4, demonstrating the
// refusal path the runbook walks. The fixture's dependency declaration is
// asserted directly so the "dependency present, no advisory" premise stays real.
func TestRunVulnScanRepoSupplyChainDemoMissingEvidenceRefuses(t *testing.T) {
	missingDir := filepath.Join(supplyChainDemoRoot, "missing-evidence")
	requireNpmFixtureDependency(t, missingDir, "synthetic-missing-evidence-npm")

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

	out, err := runSupplyChainDemoScan(t, missingDir, tc)
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

// TestSupplyChainDemoDockerfileProvidesRuntimeDependency proves the demo image
// includes a local runtime module for the synthetic package. The Dockerfile
// intentionally avoids npm install because the package is not on a real
// registry; without this stub, server.js can parse in the repository fixture
// but the built image exits on require().
func TestSupplyChainDemoDockerfileProvidesRuntimeDependency(t *testing.T) {
	requireNpmFixtureDependency(t, filepath.Join(supplyChainDemoRoot, "app"), supplyChainDemoVulnerablePackage)
	stubRoot := filepath.Join(supplyChainDemoRoot, "docker-stubs", supplyChainDemoVulnerablePackage)
	stubPackage := readDemoJSON(t, filepath.Join(stubRoot, "package.json"))
	if got := stubPackage["name"]; got != supplyChainDemoVulnerablePackage {
		t.Fatalf("runtime stub package name = %#v, want %q", got, supplyChainDemoVulnerablePackage)
	}
	stubIndexRaw, err := os.ReadFile(filepath.Join(stubRoot, "index.js"))
	if err != nil {
		t.Fatalf("read runtime stub index.js: %v", err)
	}
	if !strings.Contains(string(stubIndexRaw), "module.exports.render") {
		t.Fatalf("runtime stub index.js does not export render")
	}
	dockerfileRaw, err := os.ReadFile(filepath.Join(supplyChainDemoRoot, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(dockerfileRaw)
	for _, want := range []string{
		"docker-stubs/" + supplyChainDemoVulnerablePackage,
		"node_modules/" + supplyChainDemoVulnerablePackage,
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile does not include %q; runtime dependency stub is not copied", want)
		}
	}
}

// requireNpmFixtureDependency asserts that the demo repo at repoDir declares pkg
// in both package.json dependencies and the package-lock.json locked package
// set, and returns the locked version. This is what ties these tests to the
// committed fixtures: if the manifest or lockfile stops declaring the synthetic
// dependency, the test fails instead of silently passing on the stubbed
// envelope.
func requireNpmFixtureDependency(t *testing.T, repoDir, pkg string) string {
	t.Helper()
	manifest := readDemoJSON(t, filepath.Join(repoDir, "package.json"))
	deps, ok := manifest["dependencies"].(map[string]any)
	if !ok {
		t.Fatalf("package.json in %s has no dependencies map; demo fixture drifted", repoDir)
	}
	if _, ok := deps[pkg].(string); !ok {
		t.Fatalf("package.json in %s does not declare dependency %q; demo fixture drifted", repoDir, pkg)
	}
	lock := readDemoJSON(t, filepath.Join(repoDir, "package-lock.json"))
	packages, ok := lock["packages"].(map[string]any)
	if !ok {
		t.Fatalf("package-lock.json in %s has no packages map; demo fixture drifted", repoDir)
	}
	entry, ok := packages["node_modules/"+pkg].(map[string]any)
	if !ok {
		t.Fatalf("package-lock.json in %s has no locked entry for %q; demo fixture drifted", repoDir, pkg)
	}
	version, ok := entry["version"].(string)
	if !ok || version == "" {
		t.Fatalf("package-lock.json entry for %q in %s has no version", pkg, repoDir)
	}
	return version
}

// readDemoJSON reads and unmarshals a JSON object from the demo corpus.
func readDemoJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read demo json %s: %v", path, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal demo json %s: %v", path, err)
	}
	return parsed
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
