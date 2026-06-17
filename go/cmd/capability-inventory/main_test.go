package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoSpecsDir resolves the repository specs directory from this test file.
func repoSpecsDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "specs"))
}

// TestVerifyAgainstRealSpecs is the drift gate: the committed, embedded catalog
// artifact must reconcile with zero findings and match a fresh regeneration from
// the real specs and live MCP registry.
func TestVerifyAgainstRealSpecs(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	err := run([]string{"-mode", "verify", "-specs", repoSpecsDir(t)}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("verify failed: %v\nstdout:\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "capability catalog verified") {
		t.Fatalf("verify output missing confirmation:\n%s", stdout.String())
	}
}

// TestReportListsEntries exercises report mode.
func TestReportListsEntries(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-mode", "report", "-specs", repoSpecsDir(t)}, &stdout, &stderr); err != nil {
		t.Fatalf("report failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "no reconciliation findings") {
		t.Fatalf("report findings not clean:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "catalog entries:") {
		t.Fatalf("report missing entry count:\n%s", stdout.String())
	}
}

// TestUnsupportedMode rejects unknown modes.
func TestUnsupportedMode(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-mode", "bogus", "-specs", repoSpecsDir(t)}, &stdout, &stderr); err == nil {
		t.Fatal("run() error = nil, want unsupported mode error")
	}
}
