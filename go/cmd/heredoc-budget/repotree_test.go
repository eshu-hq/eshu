// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRootDir resolves the enclosing eshu checkout root from this test file's
// own path, so the committed-baseline check below always scans the working
// tree it runs in (go/cmd/heredoc-budget/repotree_test.go is four levels
// below the root).
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed: cannot locate the checkout root")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

// TestCommittedBaselineMatchesWorkingTree is the converted heredoc-budget
// gate (#10988, part of #9315): it runs the same check the old
// `go run ./cmd/heredoc-budget -baseline ...` gate cell ran — scanning the
// real scripts/ tree against the committed baseline — under `go test`, so
// test.yml and the re-pointed gate cell execute one runner. A new heredoc
// over the budget (or a widened unquoted body) fails this test until the
// baseline is deliberately regenerated with -update.
func TestCommittedBaselineMatchesWorkingTree(t *testing.T) {
	baseline := filepath.Join(repoRootDir(t), "scripts", "heredoc-budget-baseline.txt")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-baseline", baseline}, &stdout, &stderr); code != 0 {
		t.Fatalf("working tree regressed vs the committed heredoc baseline (exit %d):\nstdout: %s\nstderr: %s",
			code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Fatalf("expected a PASS summary, got stdout: %s", stdout.String())
	}
}
