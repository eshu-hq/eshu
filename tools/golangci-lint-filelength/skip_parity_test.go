// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSkipMatchesTheBashMirror runs this plugin's skip() and the bash
// implementation in scripts/dev/precommit-go.sh over the same paths and
// requires them to agree.
//
// This plugin is the authority: CI enforces the cap through it, while the
// local pre-commit hook enforces the same cap in bash. Nothing used to compare
// the two. The ci-gates trigger on tools/golangci-lint-filelength/** claimed
// that changing skip() here would be caught by re-running the bash mirror
// test, but that test only ever checked bash against bash -- it never consulted
// this file. So the drift it was supposed to prevent is exactly what happened:
// the bash side rejected long _test.go files that CI accepted (#6104).
//
// This test is what makes that claim true. Change skip() below without making
// the same change in bash and it fails here.
func TestSkipMatchesTheBashMirror(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	script := filepath.Join(repoRoot, "scripts", "dev", "precommit-go.sh")

	// Each rule skip() implements, plus the paths that must NOT be skipped --
	// a mirror that skipped everything would pass a one-sided table.
	paths := []string{
		"go/internal/cli/admin/admin.go",
		"go/internal/cli/admin/admin_test.go",
		"go/cmd/eshu/main.go",
		"go/cmd/eshu/graph_lifecycle_test.go",
		"go/internal/generated/api.go",
		"go/internal/vendor/lib.go",
		"go/internal/parser/testdata/sample.go",
		"go/internal/generatedthing/notgenerated.go",
		"go/internal/x/vendored.go",
		"go/internal/x/testdatafile.go",
		"tools/golangci-lint-filelength/filelength.go",
	}

	for _, path := range paths {
		got := skip(path)

		cmd := exec.Command("bash", script, "filecap-skip", path)
		cmd.Dir = repoRoot
		runErr := cmd.Run()

		var bashSkipped bool
		switch {
		case runErr == nil:
			bashSkipped = true
		default:
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				t.Fatalf("running the bash mirror for %q: %v", path, runErr)
			}
			if code := exitErr.ExitCode(); code != 1 {
				t.Fatalf("bash mirror for %q exited %d; want 0 (skipped) or 1 (checked)",
					path, code)
			}
			bashSkipped = false
		}

		if got != bashSkipped {
			t.Errorf("skip(%q): plugin=%v bash=%v -- the local hook and CI now "+
				"disagree about this path", path, got, bashSkipped)
		}
	}
}
