// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestCollectorTrackedResolver(walkRoot string, resolve func(string) (map[string]struct{}, bool)) *collectorTrackedResolver {
	return &collectorTrackedResolver{
		resolve:   resolve,
		walkRoot:  walkRoot,
		ctx:       context.Background(),
		rootByDir: make(map[string]string),
		setByRoot: make(map[string]map[string]struct{}),
	}
}

// TestCollectorTrackedResolverIsLazyWhenNoIgnoreMatch proves the issue #5658
// P1b fix: shouldSkipFilesystemEntry checks the gitignore match FIRST and
// calls trackedFile only on a match, so a file that never matches
// .gitignore must never invoke resolve() at all — the fable-measured eager
// call spawned one `git ls-files` subprocess per repository regardless of
// match rate.
func TestCollectorTrackedResolverIsLazyWhenNoIgnoreMatch(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(repoRoot, ".gitignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(repoRoot, "kept.py"), "print('kept')\n")

	var calls int
	resolver := newTestCollectorTrackedResolver(repoRoot, func(string) (map[string]struct{}, bool) {
		calls++
		return map[string]struct{}{}, true
	})
	caches := newCollectorIgnoreCaches()
	caches.tracked = resolver

	keptPath := filepath.Join(repoRoot, "kept.py")
	rel, err := filepath.Rel(repoRoot, keptPath)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if shouldSkipFilesystemEntry(repoRoot, keptPath, rel, "kept.py", false, caches) {
		t.Fatalf("shouldSkipFilesystemEntry(kept.py) = true, want false (no gitignore match)")
	}
	if calls != 0 {
		t.Fatalf("resolve invoked %d times, want 0 (kept.py never matched .gitignore; tracked status was never decision-relevant)", calls)
	}
}

// TestCollectorTrackedResolverInvokesResolveOnceWhenDecisionRelevant proves
// the lazy resolver still fires — exactly once — when a discovered file
// actually matches .gitignore, and multiple ignore-matched files in the same
// git root still cost only one resolve() call (setByRoot memoization).
func TestCollectorTrackedResolverInvokesResolveOnceWhenDecisionRelevant(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(repoRoot, ".gitignore"), "*.tfstate\n")

	var calls int
	resolver := newTestCollectorTrackedResolver(repoRoot, func(string) (map[string]struct{}, bool) {
		calls++
		return map[string]struct{}{"a.tfstate": {}, "b.tfstate": {}}, true
	})
	caches := newCollectorIgnoreCaches()
	caches.tracked = resolver

	for _, name := range []string{"a.tfstate", "b.tfstate"} {
		full := filepath.Join(repoRoot, name)
		rel, err := filepath.Rel(repoRoot, full)
		if err != nil {
			t.Fatalf("filepath.Rel: %v", err)
		}
		if shouldSkipFilesystemEntry(repoRoot, full, rel, name, false, caches) {
			t.Fatalf("shouldSkipFilesystemEntry(%s) = true, want false (tracked despite gitignore match)", name)
		}
	}
	if calls != 1 {
		t.Fatalf("resolve invoked %d times, want exactly 1 (memoized per git root across 2 matched files)", calls)
	}
}

// TestCollectorTrackedResolverInvokesResolveOncePerNearestGitRoot proves the
// issue #5658 P1a+P1b combination: an outer repo and a nested repo (its own
// ".git" marker) each get their own resolve() call, and each call fires at
// most once regardless of how many ignore-matched files land in that root.
func TestCollectorTrackedResolverInvokesResolveOncePerNearestGitRoot(t *testing.T) {
	t.Parallel()

	outerRoot := t.TempDir()
	nestedRoot := filepath.Join(outerRoot, "modules", "nested")
	if err := os.MkdirAll(filepath.Join(nestedRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir nested .git marker: %v", err)
	}
	writeSelectionTestFile(t, filepath.Join(outerRoot, ".gitignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(nestedRoot, ".gitignore"), "*.tfstate\n")

	calls := map[string]int{}
	resolver := newTestCollectorTrackedResolver(outerRoot, func(root string) (map[string]struct{}, bool) {
		calls[root]++
		switch root {
		case outerRoot:
			return map[string]struct{}{"outer.tfstate": {}}, true
		case nestedRoot:
			return map[string]struct{}{"terraform.tfstate": {}}, true
		default:
			return nil, false
		}
	})
	caches := newCollectorIgnoreCaches()
	caches.tracked = resolver

	outerFile := filepath.Join(outerRoot, "outer.tfstate")
	outerRel, err := filepath.Rel(outerRoot, outerFile)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if shouldSkipFilesystemEntry(outerRoot, outerFile, outerRel, "outer.tfstate", false, caches) {
		t.Fatal("outer.tfstate skipped, want kept (tracked in the outer repo's own root)")
	}

	nestedFile := filepath.Join(nestedRoot, "terraform.tfstate")
	// repoRoot and rel mirror copyRepositoryTree's real call convention:
	// repoRoot is always sourceRoot (outerRoot), rel is fullPath relative to
	// sourceRoot — tracked-status resolution computes its OWN rel against
	// the nearest root internally.
	nestedRel, err := filepath.Rel(outerRoot, nestedFile)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if shouldSkipFilesystemEntry(outerRoot, nestedFile, nestedRel, "terraform.tfstate", false, caches) {
		t.Fatal("nested terraform.tfstate skipped, want kept (tracked in its own nested repo's root)")
	}

	if calls[outerRoot] != 1 {
		t.Fatalf("resolve invoked %d times for outer root, want 1", calls[outerRoot])
	}
	if calls[nestedRoot] != 1 {
		t.Fatalf("resolve invoked %d times for nested root, want 1", calls[nestedRoot])
	}
}

// TestCollectorTrackedResolverEshuIgnoreLogGateIsLazy proves the issue #5658
// P1b fix to copyRepositoryTree's eshuignore log gate: it PATH-shims a
// counting wrapper in front of the real `git` binary so gitTrackedFiles'
// actual subprocess spawns are observable, then runs copyRepositoryTree
// end-to-end against a real repo with one eshuignore-matched tracked file
// and one plain file. The log gate must spawn `git ls-files` only for the
// eshuignore-matched file's repo root — never merely because the walk
// visited a non-matching file.
//
// This test does not call t.Parallel(): Go's test runner defers every
// t.Parallel() test in the package until all serial tests (this one
// included) have finished, so mutating PATH via t.Setenv here cannot race
// with another test's concurrently running `git` subprocess.
func TestCollectorTrackedResolverEshuIgnoreLogGateIsLazy(t *testing.T) {
	repoRoot := t.TempDir()
	mustInitGitRepo(t, repoRoot)
	writeSelectionTestFile(t, filepath.Join(repoRoot, ".eshuignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(repoRoot, "terraform.tfstate"), "{}")
	writeSelectionTestFile(t, filepath.Join(repoRoot, "kept.py"), "print('kept')\n")
	runGit(t, repoRoot, "add", "-f", "terraform.tfstate", "kept.py", ".eshuignore")
	runGit(t, repoRoot, "commit", "-m", "initial")

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("LookPath(git): %v", err)
	}
	countFile := filepath.Join(t.TempDir(), "git-invocations.log")
	shimDir := t.TempDir()
	shimScript := "#!/bin/sh\n" +
		"echo x >> " + shellQuote(countFile) + "\n" +
		"exec " + shellQuote(realGit) + " \"$@\"\n"
	shimPath := filepath.Join(shimDir, "git")
	if err := os.WriteFile(shimPath, []byte(shimScript), 0o755); err != nil { // #nosec G306 -- test-only executable shim script
		t.Fatalf("write git shim: %v", err)
	}

	targetRoot := t.TempDir()
	// PATH is mutated only for this call, restored by t.Cleanup when the
	// (non-parallel) test returns.
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := copyRepositoryTree(context.Background(), repoRoot, targetRoot); err != nil {
		t.Fatalf("copyRepositoryTree() error = %v, want nil", err)
	}

	if _, err := os.Stat(filepath.Join(targetRoot, "kept.py")); err != nil {
		t.Fatalf("managed copy missing kept.py: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "terraform.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("managed copy unexpectedly contains eshuignored terraform.tfstate (stat err = %v, want IsNotExist)", err)
	}

	invocations, err := os.ReadFile(countFile) // #nosec G304 -- reads the test's own scratch count file
	if err != nil {
		t.Fatalf("read count file: %v", err)
	}
	gitCalls := len(strings.Split(strings.TrimRight(string(invocations), "\n"), "\n"))
	// Exactly one `git ls-files` call: the single eshuignore-matched
	// terraform.tfstate resolves (and memoizes) the repo root's tracked set
	// once; kept.py never matches .eshuignore, so the log gate's
	// isCollectorEshuignoredInRepo(...) short-circuit must never call
	// trackedFile for it, and the resolver is never invoked a second time.
	if gitCalls != 1 {
		t.Fatalf("git invocation count = %d, want exactly 1 (only the eshuignore-matched file may trigger resolve()); log contents: %q", gitCalls, string(invocations))
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
