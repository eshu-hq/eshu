// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv writes a minimal specs dir, snapshot, and manifest so run() exercises
// the real loaders (embedded surface inventory + fact-kind registry, on-disk
// parser ledger + matrix + manifest + snapshot).
func testEnv(t *testing.T, manifestBody string) (specsDir, snapshot, manifest, reportOut string) {
	t.Helper()
	specsDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(specsDir, "parser-backing-ledger.v1.yaml"),
		[]byte("version: 1\nparser_backing:\n  - parser: hcl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "capability-matrix.v1.yaml"),
		[]byte("capabilities:\n  - capability: cap.demo\n    profiles:\n      local: {status: supported}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "product-claims.v1.yaml"),
		[]byte("version: v1\nclaims: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "authorization-catalog.v1.yaml"),
		[]byte("version: v1\npermission_families: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specsDir, "ci-gates.v1.yaml"), []byte(`version: v1
gates:
  - id: golden-corpus-gate
    name: Golden Corpus Gate
    category: exactness
    tier: ci-heavy
    blocking: true
    triggers: ["testdata/cassettes/**"]
    local:
      command: "bash scripts/verify-golden-corpus-gate.sh"
      test_command: "bash scripts/test-verify-golden-corpus-gate.sh"
    ci:
      workflow: golden-corpus-gate.yml
      job: "Golden corpus gate"
    requirements: [go, docker, nornicdb]
    ci_only_reason: ""
    local_only_reason: ""
`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot = filepath.Join(specsDir, "snapshot.json")
	if err := os.WriteFile(snapshot, []byte(`{"schema_version":"1","graph":{"required_correlations":[]},"query_shapes":{"http":{},"mcp":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest = filepath.Join(specsDir, "manifest.yaml")
	if err := os.WriteFile(manifest, []byte(manifestBody), 0o600); err != nil {
		t.Fatal(err)
	}
	reportOut = filepath.Join(t.TempDir(), "report.json")
	return specsDir, snapshot, manifest, reportOut
}

func TestRunAdvisoryReportsGapsWithoutFailing(t *testing.T) {
	specsDir, snapshot, manifest, reportOut := testEnv(t, "version: \"v1\"\n")
	var out, errOut bytes.Buffer
	err := run([]string{
		"-specs-dir", specsDir,
		"-snapshot", snapshot,
		"-manifest", manifest,
		"-repo-root", t.TempDir(),
		"-report-out", reportOut,
	}, &out, &errOut)
	if err != nil {
		t.Fatalf("advisory run returned error: %v\nstderr: %s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "coverage (advisory)") {
		t.Errorf("missing advisory summary header:\n%s", out.String())
	}
	data, readErr := os.ReadFile(reportOut)
	if readErr != nil {
		t.Fatalf("report not written: %v", readErr)
	}
	if !strings.Contains(string(data), "\"schema_version\": \"replay-coverage-report.v2\"") {
		t.Errorf("report payload missing schema version:\n%s", data)
	}
	// The real embedded registries enumerate uncovered surfaces -> non-empty gaps.
	if !strings.Contains(string(data), "\"gaps\"") {
		t.Error("report missing gaps field")
	}
}

func TestRunBlockingFailsOnUncoveredSurfaces(t *testing.T) {
	specsDir, snapshot, manifest, reportOut := testEnv(t, "version: \"v1\"\n")
	var out, errOut bytes.Buffer
	err := run([]string{
		"-specs-dir", specsDir,
		"-snapshot", snapshot,
		"-manifest", manifest,
		"-repo-root", t.TempDir(),
		"-report-out", reportOut,
		"-blocking",
	}, &out, &errOut)
	if err == nil {
		t.Fatal("blocking run with uncovered surfaces must return an error")
	}
	if !strings.Contains(err.Error(), "blocking") {
		t.Errorf("error should mention blocking: %v", err)
	}
	// The report is still written before the gate fails, so C-7 always has data.
	if _, statErr := os.Stat(reportOut); statErr != nil {
		t.Errorf("report should be written even when blocking gate fails: %v", statErr)
	}
}

func TestRunRejectsBadManifest(t *testing.T) {
	specsDir, snapshot, manifest, _ := testEnv(t, "version: \"v1\"\ncoverage:\n  - surface: collector:aws\n    scenario: bogus\n    ref: x\n")
	var out, errOut bytes.Buffer
	if err := run([]string{
		"-specs-dir", specsDir, "-snapshot", snapshot, "-manifest", manifest, "-repo-root", t.TempDir(),
	}, &out, &errOut); err == nil {
		t.Fatal("run must surface a malformed manifest as an error")
	}
}

func TestRunRejectsUnknownProofGate(t *testing.T) {
	manifestBody := `version: "v1"
coverage:
  - surface: collector:aws
    scenario: cassette
    scenario_type: baseline
    ref: testdata/cassettes/awscloud/supply-chain-demo.json
    proof_gate: stale-proof-gate
`
	specsDir, snapshot, manifest, reportOut := testEnv(t, manifestBody)
	var out, errOut bytes.Buffer
	err := run([]string{
		"-specs-dir", specsDir,
		"-snapshot", snapshot,
		"-manifest", manifest,
		"-repo-root", t.TempDir(),
		"-report-out", reportOut,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("run must reject unknown proof_gate")
	}
	if !strings.Contains(err.Error(), `unknown proof_gate "stale-proof-gate"`) {
		t.Fatalf("error = %v, want unknown proof_gate", err)
	}
	data, readErr := os.ReadFile(reportOut)
	if readErr != nil {
		t.Fatalf("report should be written before returning proof_gate validation error: %v", readErr)
	}
	if !strings.Contains(string(data), `"status": "unresolved"`) {
		t.Fatalf("report should mark stale proof gate unresolved:\n%s", data)
	}
}
