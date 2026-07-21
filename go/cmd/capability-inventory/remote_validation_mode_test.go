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

func writeRemoteValidationSpecs(t *testing.T, dir, ref string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	body := "capabilities:\n" +
		"  - capability: code_search.exact_symbol\n" +
		"    tools: [find_code]\n" +
		"    profiles:\n" +
		"      production: {status: supported, max_truth_level: exact, verification: [{remote_validation: " + ref + "}]}\n"
	if err := os.WriteFile(filepath.Join(dir, "capability-matrix.v1.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write matrix: %v", err)
	}
}

// TestRemoteValidationModeFailsOnUnbaselinedDanglingRef is the RED case: a
// remote_validation ref with no committed artifact and no baseline entry
// must fail -mode remote-validation.
func TestRemoteValidationModeFailsOnUnbaselinedDanglingRef(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	specsDir := filepath.Join(tmp, "specs")
	writeRemoteValidationSpecs(t, specsDir, "prod-dangling-example")
	baseline := filepath.Join(specsDir, "remote-validation-baseline.txt")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-mode", "remote-validation",
		"-specs", specsDir,
		"-root", tmp,
		"-remote-validation-baseline", baseline,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected remote-validation mode to fail on a dangling unbaselined ref, stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "prod-dangling-example") {
		t.Fatalf("expected finding output to name the dangling ref, got:\n%s", stdout.String())
	}
}

// TestRemoteValidationModePassesWhenBaselined is the GREEN case: the same
// dangling ref passes once it is listed in the baseline file.
func TestRemoteValidationModePassesWhenBaselined(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	specsDir := filepath.Join(tmp, "specs")
	writeRemoteValidationSpecs(t, specsDir, "prod-dangling-example")
	baseline := filepath.Join(specsDir, "remote-validation-baseline.txt")
	if err := os.WriteFile(baseline, []byte("prod-dangling-example\n"), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-mode", "remote-validation",
		"-specs", specsDir,
		"-root", tmp,
		"-remote-validation-baseline", baseline,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected remote-validation mode to pass a baselined ref: %v\nstdout:\n%s", err, stdout.String())
	}
}

// TestRemoteValidationModePassesWhenArtifactCommitted proves committing the
// evidence file itself (not baselining) also clears the gate.
func TestRemoteValidationModePassesWhenArtifactCommitted(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	specsDir := filepath.Join(tmp, "specs")
	writeRemoteValidationSpecs(t, specsDir, "prod-has-artifact-example")
	baseline := filepath.Join(specsDir, "remote-validation-baseline.txt")
	artifactDir := filepath.Join(tmp, "docs", "internal", "remote-validation")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "prod-has-artifact-example.md"), []byte("# evidence\n"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-mode", "remote-validation",
		"-specs", specsDir,
		"-root", tmp,
		"-remote-validation-baseline", baseline,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected remote-validation mode to pass a ref with a committed artifact: %v\nstdout:\n%s", err, stdout.String())
	}
}

// TestRemoteValidationModeUpdateRegeneratesBaseline proves -update writes the
// current dangling set to baselinePath.
func TestRemoteValidationModeUpdateRegeneratesBaseline(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	specsDir := filepath.Join(tmp, "specs")
	writeRemoteValidationSpecs(t, specsDir, "prod-regen-example")
	baseline := filepath.Join(specsDir, "remote-validation-baseline.txt")

	var stdout, stderr bytes.Buffer
	if err := run([]string{
		"-mode", "remote-validation",
		"-specs", specsDir,
		"-root", tmp,
		"-remote-validation-baseline", baseline,
		"-update",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("-update failed: %v\nstdout:\n%s", err, stdout.String())
	}

	written, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatalf("read regenerated baseline: %v", err)
	}
	if !strings.Contains(string(written), "prod-regen-example") {
		t.Fatalf("regenerated baseline missing dangling ref, got:\n%s", string(written))
	}

	// A second run with no -update must now pass: -update baselined exactly
	// the current dangling set.
	var checkStdout, checkStderr bytes.Buffer
	if err := run([]string{
		"-mode", "remote-validation",
		"-specs", specsDir,
		"-root", tmp,
		"-remote-validation-baseline", baseline,
	}, &checkStdout, &checkStderr); err != nil {
		t.Fatalf("check after -update failed: %v\nstdout:\n%s", err, checkStdout.String())
	}
}

// TestRemoteValidationModeFailsClosedOnMalformedBaseline proves a malformed
// baseline line aborts the check rather than being silently skipped.
func TestRemoteValidationModeFailsClosedOnMalformedBaseline(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	specsDir := filepath.Join(tmp, "specs")
	writeRemoteValidationSpecs(t, specsDir, "prod-dangling-example")
	baseline := filepath.Join(specsDir, "remote-validation-baseline.txt")
	if err := os.WriteFile(baseline, []byte("Not A Valid Slug\n"), 0o644); err != nil {
		t.Fatalf("write malformed baseline: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-mode", "remote-validation",
		"-specs", specsDir,
		"-root", tmp,
		"-remote-validation-baseline", baseline,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected remote-validation mode to fail closed on a malformed baseline")
	}
}

// TestRemoteValidationModeRealSpecs is the real-repo gate: the committed
// specs/remote-validation-baseline.txt must cover every currently dangling
// remote_validation ref.
func TestRemoteValidationModeRealSpecs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-mode", "remote-validation",
		"-specs", repoSpecsDir(t),
		"-root", repoRootDir(t),
		"-remote-validation-baseline", filepath.Join(repoSpecsDir(t), "remote-validation-baseline.txt"),
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("remote-validation mode against real specs: %v\nstdout:\n%s", err, stdout.String())
	}
}
