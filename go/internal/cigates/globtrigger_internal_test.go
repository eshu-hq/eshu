// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// universeOf builds a path universe from an explicit path list, so a case can
// state exactly what the tree contains — including the directory entries
// loadTrackedPaths derives — without needing a git fixture.
func universeOf(paths ...string) *trackedPaths {
	tp := &trackedPaths{byHead: make(map[string][][]string)}
	seen := make(map[string]struct{})
	for _, p := range paths {
		tp.add(p, seen)
	}
	return tp
}

// TestTrackedPaths_MatchesAnyAgreesWithMatchGlob is the equivalence guard the
// first-segment index needs. matchesAny skips whole buckets of the universe
// and reuses matchSegments directly instead of calling MatchGlob per path, so
// it can drift from MatchGlob in exactly the cases the index reasons about: a
// pattern whose first segment is a wildcard, a leading "**", and the anchored
// and directory-style patterns MatchGlob rejects before matching. Select uses
// MatchGlob to pick gates at run time; if these two disagree, this check
// either passes a trigger that can never select or fails one that does.
func TestTrackedPaths_MatchesAnyAgreesWithMatchGlob(t *testing.T) {
	t.Parallel()

	universe := []string{
		"go", "go/internal", "go/internal/cigates", "go/internal/cigates/glob.go",
		"go/internal/query", "go/internal/query/openapi_types.go",
		"scripts", "scripts/run-remote-e2e-x.sh", "scripts/lib", "scripts/lib/live-gate-lock.sh",
		".github", ".github/workflows", ".github/workflows/test.yml",
		"README.md", "Makefile",
	}
	tp := universeOf(universe...)

	patterns := []string{
		"go/**",                          // literal head, matches
		"go/internal/*",                  // literal head, only a directory matches
		"go/internal/cigates/*.go",       // literal head, file match
		"go/internal/nothing/**",         // literal head, no match
		"scripts/**/run-remote-e2e-*.sh", // ** at its zero-segment expansion
		"scripts/**/nothing-*.sh",        // ** expansion, no match
		"*.md",                           // wildcard head — must not use the index
		"*/internal/**",                  // wildcard head, matches deeper
		"*/nothing/**",                   // wildcard head, no match
		"**/openapi*.go",                 // ** head — must not use the index
		"**/nothing*.go",                 // ** head, no match
		".github/workflows/*.yml",        // dotted head
		"../etc/*.conf",                  // escapes the tree: matches nothing
		"/go/**",                         // anchored: MatchGlob rejects outright
		"go/internal/",                   // directory-style: MatchGlob rejects outright
		"go",                             // degenerate single-segment literal
		"Makefile",                       // degenerate single-segment literal
		"go/internal/cigates/glob.go/**", // past a file: no match
		"go/**/cigates/**/glob.go",       // two **, one expanding to zero
	}

	for _, pattern := range patterns {
		want := false
		for _, p := range universe {
			if MatchGlob(pattern, p) {
				want = true
				break
			}
		}
		if got := tp.matchesAny(pattern); got != want {
			t.Errorf("matchesAny(%q) = %v; a MatchGlob scan of the same universe says %v", pattern, got, want)
		}
	}
}

// TestLoadTrackedPaths_DerivesEveryAncestorDirectory pins the universe's shape
// against the real enumerator. git tracks files only, so the directory
// entries a directory-shaped trigger resolves against exist only because this
// derives them — and it stops walking at the first ancestor it has already
// seen, which is sound only while every recorded path brought its own
// ancestors with it. A regression there drops directories silently and turns
// working registry triggers into reported-stale ones.
func TestLoadTrackedPaths_DerivesEveryAncestorDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Two files sharing a prefix: the second reaches an ancestor the first
	// already recorded, which is the case the short-circuit walks into.
	for _, rel := range []string{"go/internal/cigates/glob.go", "go/internal/cigates/validate.go", "README.md"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"-c", "init.defaultBranch=main", "init", "-q"},
		{"add", "-A", "--force"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	tp, err := loadTrackedPaths(root)
	if err != nil {
		t.Fatalf("loadTrackedPaths() error = %v", err)
	}

	var got []string
	for _, segments := range tp.all {
		got = append(got, strings.Join(segments, "/"))
	}
	sort.Strings(got)
	want := []string{
		"README.md",
		"go",
		"go/internal",
		"go/internal/cigates",
		"go/internal/cigates/glob.go",
		"go/internal/cigates/validate.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("universe =\n%s\nwant (every file plus every implied directory, each once) =\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestLoadTrackedPaths_FailsOnATreeGitDoesNotTrack pins the fail-closed rule
// at its source. An empty or unreadable tracked set means no glob trigger was
// verified, and the caller must be told that rather than handed an empty
// universe every trigger would then "fail" against for the wrong reason, or —
// worse — a silent skip.
func TestLoadTrackedPaths_FailsOnATreeGitDoesNotTrack(t *testing.T) {
	t.Parallel()

	tp, err := loadTrackedPaths(t.TempDir())
	if err == nil {
		t.Fatalf("loadTrackedPaths() on a non-work-tree returned %+v and no error; an unverifiable trigger set must not read as enumerable", tp)
	}
	if !strings.Contains(err.Error(), "tracked paths") {
		t.Fatalf("loadTrackedPaths() error = %v, want one naming the tracked path set it could not read", err)
	}
}
