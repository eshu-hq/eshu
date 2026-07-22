// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func mustInitGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
}

func TestGitTrackedFilesListsForceAddedTrackedPath(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	mustInitGitRepo(t, repoRoot)
	writeFile(t, repoRoot, ".gitignore", "*.tfstate\n")
	writeFile(t, repoRoot, "terraform.tfstate", "{}")
	writeFile(t, repoRoot, "kept.py", "print('kept')\n")
	runGit(t, repoRoot, "add", ".gitignore", "kept.py")
	runGit(t, repoRoot, "add", "-f", "terraform.tfstate")
	runGit(t, repoRoot, "commit", "-m", "initial")
	// scratch.tfstate is written but never `git add`ed: it must be absent
	// from the tracked set.
	writeFile(t, repoRoot, "scratch.tfstate", "{}")

	tracked, ok := gitTrackedFiles(context.Background(), repoRoot)
	if !ok {
		t.Fatal("gitTrackedFiles() ok = false, want true for a real git checkout")
	}
	for _, want := range []string{"terraform.tfstate", "kept.py", ".gitignore"} {
		if _, present := tracked[want]; !present {
			t.Errorf("tracked[%q] missing, want present; tracked=%v", want, tracked)
		}
	}
	if _, present := tracked["scratch.tfstate"]; present {
		t.Error("tracked[scratch.tfstate] present, want absent (never git add'ed)")
	}
}

func TestGitTrackedFilesReturnsNotOKForNonGitDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracked, ok := gitTrackedFiles(context.Background(), dir)
	if ok {
		t.Fatalf("gitTrackedFiles() ok = true, want false for a non-git directory; tracked=%v", tracked)
	}
	if tracked != nil {
		t.Fatalf("gitTrackedFiles() tracked = %v, want nil when ok=false", tracked)
	}
}

func TestHasGitDirMarker(t *testing.T) {
	t.Parallel()

	gitRepo := t.TempDir()
	mustInitGitRepo(t, gitRepo)
	if !hasGitDirMarker(gitRepo) {
		t.Errorf("hasGitDirMarker(%q) = false, want true", gitRepo)
	}

	plainDir := t.TempDir()
	if hasGitDirMarker(plainDir) {
		t.Errorf("hasGitDirMarker(%q) = true, want false", plainDir)
	}
}

// TestBuildGitTrackedResolverGitModeUsesRepoRootDirectly proves the ordinary
// git-sync case: repoRoot has its own .git, so the resolver runs ls-files
// there directly regardless of gitTreePath (which equals repoRoot in this
// mode, per SnapshotRepository).
func TestBuildGitTrackedResolverGitModeUsesRepoRootDirectly(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	mustInitGitRepo(t, repoRoot)
	writeFile(t, repoRoot, "tracked.tfstate", "{}")
	runGit(t, repoRoot, "add", "-f", "tracked.tfstate")
	runGit(t, repoRoot, "commit", "-m", "initial")

	resolver := buildGitTrackedResolver(context.Background(), repoRoot, repoRoot)
	tracked, ok := resolver(repoRoot)
	if !ok {
		t.Fatal("resolver() ok = false, want true")
	}
	if _, present := tracked["tracked.tfstate"]; !present {
		t.Errorf("tracked[tracked.tfstate] missing, want present; tracked=%v", tracked)
	}
}

// TestBuildGitTrackedResolverManagedCopyModeUsesGitTreePath proves the
// filesystem managed-copy case (issue #5649): the scanRoot itself (the
// managed copy) has no .git, so the resolver must defer to gitTreePath (the
// SOURCE checkout) when asked about scanRoot.
func TestBuildGitTrackedResolverManagedCopyModeUsesGitTreePath(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	mustInitGitRepo(t, sourceRoot)
	writeFile(t, sourceRoot, "tracked.tfstate", "{}")
	runGit(t, sourceRoot, "add", "-f", "tracked.tfstate")
	runGit(t, sourceRoot, "commit", "-m", "initial")

	managedCopyRoot := t.TempDir() // no .git here — mirrors a filesystem-mode managed copy

	resolver := buildGitTrackedResolver(context.Background(), managedCopyRoot, sourceRoot)
	tracked, ok := resolver(managedCopyRoot)
	if !ok {
		t.Fatal("resolver() ok = false, want true (must defer to gitTreePath)")
	}
	if _, present := tracked["tracked.tfstate"]; !present {
		t.Errorf("tracked[tracked.tfstate] missing, want present; tracked=%v", tracked)
	}
}

// TestBuildGitTrackedResolverManagedCopyModeMirrorsNestedRepoRoot proves a
// nested repository root discovered somewhere under a managed-copy scanRoot
// (itself not a git checkout) resolves against the SAME relative path
// mirrored under gitTreePath, matching how copyRepositoryTree mirrors the
// source tree into the managed copy 1:1.
func TestBuildGitTrackedResolverManagedCopyModeMirrorsNestedRepoRoot(t *testing.T) {
	t.Parallel()

	sourceRoot := t.TempDir()
	nestedSourceRepo := filepath.Join(sourceRoot, "modules", "nested")
	if err := os.MkdirAll(nestedSourceRepo, 0o755); err != nil {
		t.Fatalf("mkdir nested source repo: %v", err)
	}
	mustInitGitRepo(t, nestedSourceRepo)
	writeFile(t, nestedSourceRepo, "tracked.tfstate", "{}")
	runGit(t, nestedSourceRepo, "add", "-f", "tracked.tfstate")
	runGit(t, nestedSourceRepo, "commit", "-m", "initial")

	managedCopyRoot := t.TempDir()
	nestedCopyRepo := filepath.Join(managedCopyRoot, "modules", "nested")
	if err := os.MkdirAll(nestedCopyRepo, 0o755); err != nil {
		t.Fatalf("mkdir nested copy repo: %v", err)
	}

	resolver := buildGitTrackedResolver(context.Background(), managedCopyRoot, sourceRoot)
	tracked, ok := resolver(nestedCopyRepo)
	if !ok {
		t.Fatal("resolver() ok = false, want true (must mirror rel path under gitTreePath)")
	}
	if _, present := tracked["tracked.tfstate"]; !present {
		t.Errorf("tracked[tracked.tfstate] missing, want present; tracked=%v", tracked)
	}
}

// TestBuildGitTrackedResolverInvokedPerNestedGitRepoRoot proves a nested
// repository that HAS its own .git (a real submodule checkout, in either
// git-sync or filesystem-direct mode) resolves independently of the outer
// repo, keyed by its own root — not by falling through to gitTreePath.
func TestBuildGitTrackedResolverInvokedPerNestedGitRepoRoot(t *testing.T) {
	t.Parallel()

	outerRoot := t.TempDir()
	mustInitGitRepo(t, outerRoot)
	writeFile(t, outerRoot, "outer.tfstate", "{}")
	runGit(t, outerRoot, "add", "-f", "outer.tfstate")
	runGit(t, outerRoot, "commit", "-m", "outer initial")

	nestedRoot := filepath.Join(outerRoot, "modules", "nested")
	if err := os.MkdirAll(nestedRoot, 0o755); err != nil {
		t.Fatalf("mkdir nested repo: %v", err)
	}
	mustInitGitRepo(t, nestedRoot)
	writeFile(t, nestedRoot, "nested.tfstate", "{}")
	runGit(t, nestedRoot, "add", "-f", "nested.tfstate")
	runGit(t, nestedRoot, "commit", "-m", "nested initial")

	resolver := buildGitTrackedResolver(context.Background(), outerRoot, outerRoot)

	outerTracked, ok := resolver(outerRoot)
	if !ok {
		t.Fatal("resolver(outerRoot) ok = false, want true")
	}
	if _, present := outerTracked["outer.tfstate"]; !present {
		t.Errorf("outerTracked[outer.tfstate] missing; tracked=%v", outerTracked)
	}
	if _, present := outerTracked["modules/nested/nested.tfstate"]; present {
		t.Errorf("outerTracked unexpectedly contains the nested repo's own file; tracked=%v", outerTracked)
	}

	nestedTracked, ok := resolver(nestedRoot)
	if !ok {
		t.Fatal("resolver(nestedRoot) ok = false, want true")
	}
	if _, present := nestedTracked["nested.tfstate"]; !present {
		t.Errorf("nestedTracked[nested.tfstate] missing; tracked=%v", nestedTracked)
	}
	if _, present := nestedTracked["outer.tfstate"]; present {
		t.Errorf("nestedTracked unexpectedly contains the outer repo's own file; tracked=%v", nestedTracked)
	}
}

// TestBuildGitTrackedResolverReportsNotOKOutsideScanRootOrWithoutGitTreePath
// proves the degenerate-safety branches: an empty gitTreePath, or a repoRoot
// outside scanRoot, both report ok=false rather than guessing.
func TestBuildGitTrackedResolverReportsNotOKOutsideScanRootOrWithoutGitTreePath(t *testing.T) {
	t.Parallel()

	scanRoot := t.TempDir()
	outsideRoot := t.TempDir()

	resolver := buildGitTrackedResolver(context.Background(), scanRoot, "")
	if _, ok := resolver(scanRoot); ok {
		t.Error("resolver(scanRoot) ok = true, want false when gitTreePath is empty and scanRoot has no .git")
	}

	resolverWithTreePath := buildGitTrackedResolver(context.Background(), scanRoot, scanRoot)
	if _, ok := resolverWithTreePath(outsideRoot); ok {
		t.Error("resolver(outsideRoot) ok = true, want false for a repoRoot outside scanRoot")
	}
}
