// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
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

// writeRemoteValidationSpecsMulti writes a scratch capability matrix citing one
// remote_validation ref per entry in refs (each under its own capability), so a
// test can vary the dangling set across regenerations.
func writeRemoteValidationSpecsMulti(t *testing.T, dir string, refs ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	var b strings.Builder
	b.WriteString("capabilities:\n")
	for i, ref := range refs {
		fmt.Fprintf(&b, "  - capability: cap.%d\n", i)
		b.WriteString("    tools: [find_code]\n")
		b.WriteString("    profiles:\n")
		fmt.Fprintf(&b, "      production: {status: supported, max_truth_level: exact, verification: [{remote_validation: %s}]}\n", ref)
	}
	if err := os.WriteFile(filepath.Join(dir, "capability-matrix.v1.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write matrix: %v", err)
	}
}

// TestRemoteValidationModeFailsOnCeilingGrowth proves the anti-append-smuggling
// guard at the CLI level: a baseline whose entry count exceeds the committed
// FROZEN_MAX ceiling fails -mode remote-validation even when every dangling ref
// is otherwise listed.
func TestRemoteValidationModeFailsOnCeilingGrowth(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	specsDir := filepath.Join(tmp, "specs")
	writeRemoteValidationSpecs(t, specsDir, "prod-dangling-example")
	baseline := filepath.Join(specsDir, "remote-validation-baseline.txt")
	// The cited ref IS baselined (no artifact finding), but a second entry
	// pushes the count to 2 over a frozen ceiling of 1: growth is rejected.
	if err := os.WriteFile(baseline, []byte("# FROZEN_MAX: 1\nprod-dangling-example\nprod-smuggled-extra\n"), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-mode", "remote-validation",
		"-specs", specsDir,
		"-root", tmp,
		"-remote-validation-baseline", baseline,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("expected ceiling growth to fail the gate, stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "EXCEEDS frozen ceiling") {
		t.Fatalf("expected output to explain the ceiling violation, got:\n%s", stdout.String())
	}
}

// TestRemoteValidationModeRatchetsCeilingDownThenRejectsRegrowth proves the
// full ratchet: -update on a shrunk dangling set lowers FROZEN_MAX, and after
// that a newly-added unbacked ref regenerated via -update cannot re-grow the
// set past the lowered ceiling.
func TestRemoteValidationModeRatchetsCeilingDownThenRejectsRegrowth(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	specsDir := filepath.Join(tmp, "specs")
	baseline := filepath.Join(specsDir, "remote-validation-baseline.txt")
	baseArgs := func(extra ...string) []string {
		return append([]string{
			"-mode", "remote-validation",
			"-specs", specsDir,
			"-root", tmp,
			"-remote-validation-baseline", baseline,
		}, extra...)
	}

	// Step 1: two dangling refs, -update freezes the ceiling at 2.
	writeRemoteValidationSpecsMulti(t, specsDir, "prod-ref-a", "prod-ref-b")
	var out bytes.Buffer
	if err := run(baseArgs("-update"), &out, &out); err != nil {
		t.Fatalf("step1 -update: %v\n%s", err, out.String())
	}
	if c, _ := capabilitycatalogCeiling(t, baseline); c != 2 {
		t.Fatalf("step1 ceiling = %d, want 2", c)
	}

	// Step 2: burn down to one dangling ref, -update ratchets the ceiling to 1.
	writeRemoteValidationSpecsMulti(t, specsDir, "prod-ref-a")
	out.Reset()
	if err := run(baseArgs("-update"), &out, &out); err != nil {
		t.Fatalf("step2 -update: %v\n%s", err, out.String())
	}
	if c, _ := capabilitycatalogCeiling(t, baseline); c != 1 {
		t.Fatalf("step2 ceiling = %d, want 1 (ratcheted down)", c)
	}

	// Step 3: attacker swaps in a NEW unbacked ref (count back to 2) and runs
	// -update. The ceiling stays at 1, so the regenerated baseline holds 2
	// entries over a ceiling of 1 and the subsequent check fails.
	writeRemoteValidationSpecsMulti(t, specsDir, "prod-ref-a", "prod-ref-c")
	out.Reset()
	if err := run(baseArgs("-update"), &out, &out); err != nil {
		t.Fatalf("step3 -update: %v\n%s", err, out.String())
	}
	if c, _ := capabilitycatalogCeiling(t, baseline); c != 1 {
		t.Fatalf("step3 ceiling = %d, want 1 (-update must never raise it)", c)
	}
	out.Reset()
	if err := run(baseArgs(), &out, &out); err == nil {
		t.Fatalf("step3 check: expected re-growth past the ratcheted ceiling to fail, got:\n%s", out.String())
	}
}

// capabilitycatalogCeiling reads the FROZEN_MAX ceiling from a baseline file via
// the package's own best-effort reader, so the test asserts on the real parse.
func capabilitycatalogCeiling(t *testing.T, path string) (int, bool) {
	t.Helper()
	return capabilitycatalog.ReadRemoteValidationCeiling(path)
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
	if err := os.WriteFile(baseline, []byte("# FROZEN_MAX: 1\nprod-dangling-example\n"), 0o644); err != nil {
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
