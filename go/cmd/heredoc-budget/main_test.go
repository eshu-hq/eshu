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

// --- CLI integration tests ------------------------------------------------

func TestRun_UpdateThenCheckPasses(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	overBody := strings.Repeat("z", 600) + "\n"
	mustWriteFile(t, filepath.Join(scriptsDir, "offender.sh"), "cat <<EOF\n"+overBody+"EOF\n")
	baselinePath := filepath.Join(scriptsDir, "heredoc-budget-baseline.txt")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-baseline", baselinePath, "-update"}, &stdout, &stderr); code != 0 {
		t.Fatalf("update run failed: code=%d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-baseline", baselinePath}, &stdout, &stderr); code != 0 {
		t.Fatalf("expected PASS on freshly generated baseline, got code=%d stderr=%s", code, stderr.String())
	}
}

func TestRun_NewOffenderFailsAfterBaseline(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	baselinePath := filepath.Join(scriptsDir, "heredoc-budget-baseline.txt")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-baseline", baselinePath, "-update"}, &stdout, &stderr); code != 0 {
		t.Fatalf("update run failed: %s", stderr.String())
	}

	overBody := strings.Repeat("z", 600) + "\n"
	mustWriteFile(t, filepath.Join(scriptsDir, "new-offender.sh"), "cat <<EOF\n"+overBody+"EOF\n")

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"-baseline", baselinePath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for a new offending file, got PASS: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "new-offender.sh") {
		t.Fatalf("expected stderr to name the offending file, got: %s", stderr.String())
	}
}

// TestRun_UnquotedMarginViolationReportsRationale guards #5085 end to end
// through the CLI: a fresh baseline followed by a new UNQUOTED heredoc whose
// literal source sits between the unquoted margin and the raw budget must
// fail, and the stderr report must explain the margin rationale (not just
// print the same message as an ordinary over-budget failure) so an operator
// is not confused by a "violation" whose body is under the stated budget.
func TestRun_UnquotedMarginViolationReportsRationale(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	baselinePath := filepath.Join(scriptsDir, "heredoc-budget-baseline.txt")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-baseline", baselinePath, "-update"}, &stdout, &stderr); code != 0 {
		t.Fatalf("update run failed: %s", stderr.String())
	}

	body := strings.Repeat("a", 449) + "\n" // 450 bytes: 384 < 450 <= 512
	mustWriteFile(t, filepath.Join(scriptsDir, "unquoted-risk.sh"), "cat <<EOF\n"+body+"EOF\n")

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"-baseline", baselinePath}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for a new unquoted heredoc over the margin, got PASS: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unquoted-risk.sh") {
		t.Fatalf("expected stderr to name the offending file, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "runtime-expansion margin") {
		t.Fatalf("expected stderr to explain the unquoted margin rationale, got: %s", stderr.String())
	}
}

func TestRun_RequiresBaselineFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit when -baseline is missing")
	}
}
