// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRootDir resolves the repository root relative to this test file
// (go/cmd/ifa), so these tests can exercise the real committed specs and B-12
// snapshot the same way `ifa coverage` runs against them in CI.
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func TestRunCoverageAgainstRealSpecsIsAdvisoryAndWritesReport(t *testing.T) {
	repoRoot := repoRootDir(t)
	reportOut := filepath.Join(t.TempDir(), "report.json")

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"coverage",
		"-specs-dir", filepath.Join(repoRoot, "specs"),
		"-snapshot", filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json"),
		"-report-out", reportOut,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run(coverage) against real specs = %v, stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "coverage (advisory)") {
		t.Errorf("stdout missing advisory summary header:\n%s", stdout.String())
	}
	data, readErr := os.ReadFile(reportOut)
	if readErr != nil {
		t.Fatalf("report not written: %v", readErr)
	}
	if !strings.Contains(string(data), "\"schema_version\"") {
		t.Errorf("report payload missing schema_version:\n%s", data)
	}
}

func TestRunCoverageBlockingFailsOnStaleManifestRow(t *testing.T) {
	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	// A manifest naming a fact kind that does not exist in the real registry
	// can never be enumerated as a supported surface, so it is stale drift —
	// exactly the kind of broken manifest -blocking must fail on.
	brokenManifest := filepath.Join(t.TempDir(), "ifa-coverage-manifest.v1.yaml")
	if err := os.WriteFile(brokenManifest, []byte(`version: "v1"
coverage:
  - {surface: "fact_kind:ghost_kind_that_does_not_exist", scenario: odu, scenario_type: baseline, ref: "odu:kustomize-deploys-from", proof_gate: ifa-contract-layer}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"coverage",
		"-specs-dir", specsDir,
		"-snapshot", filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json"),
		"-manifest", brokenManifest,
		"-blocking",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(coverage -blocking) with a stale manifest row must return an error")
	}
	if !strings.Contains(err.Error(), "blocking") {
		t.Errorf("error = %v, want it to mention blocking", err)
	}
}
