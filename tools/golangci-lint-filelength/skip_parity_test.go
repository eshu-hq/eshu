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
	relative := []string{
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

	// Production calls skip() with an absolute path -- golangci-lint passes
	// pass.Fset.Position(...).Filename -- while the pre-commit hook passes
	// repo-relative paths. Both forms have to agree across the two
	// implementations, so every path is checked twice.
	var paths []string
	for _, rel := range relative {
		paths = append(paths, rel, filepath.Join(repoRoot, rel))
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

// TestBashSkipsAtLeastAsMuchAtRepoRoot pins the one place the two
// implementations deliberately differ, and the direction of the difference.
//
// The plugin's generated/vendor/testdata rules require a separator on BOTH
// sides, so a repo-root "testdata/sample.go" does not match. The bash side
// also carries leading-segment alternatives ("testdata/*" as well as
// "*/testdata/*"), so it skips those.
//
// This is safe, and the direction is what makes it safe. Bash skipping MORE
// can only mean it declines to reject a file; bash skipping LESS would mean a
// local rejection CI does not make, which is the exact failure this gate was
// changed to remove (#6104). It is also unreachable for anything CI lints:
// the plugin only ever sees files under go/, whose paths always carry a
// leading "go/" segment, and the equality test above covers those in both
// relative and absolute form.
//
// Pinned rather than "fixed" so that a change to either side has to come here
// and state its case.
func TestBashSkipsAtLeastAsMuchAtRepoRoot(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	script := filepath.Join(repoRoot, "scripts", "dev", "precommit-go.sh")

	for _, path := range []string{"testdata/sample.go", "vendor/lib.go", "generated/api.go"} {
		if skip(path) {
			t.Errorf("skip(%q) = true; the plugin requires a separator on both "+
				"sides, so a repo-root segment should not match. If this rule "+
				"changed, the bash mirror must change with it", path)
		}

		cmd := exec.Command("bash", script, "filecap-skip", path)
		cmd.Dir = repoRoot
		if err := cmd.Run(); err != nil {
			t.Errorf("bash filecap-skip(%q) did not skip; bash must skip at "+
				"least as much as the plugin, never less -- skipping less "+
				"produces a local rejection CI does not make: %v", path, err)
		}
	}
}
