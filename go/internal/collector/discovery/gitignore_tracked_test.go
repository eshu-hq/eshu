// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import (
	"path/filepath"
	"testing"
)

// TestResolveRepositoryFileSetsGitTrackedResolverKeepsGitignoredTrackedFile
// proves the #5591 fix: git only applies .gitignore to UNTRACKED paths, so a
// force-committed file that matches a gitignore rule stays tracked and must
// stay discoverable. The fake GitTrackedResolver below stands in for the real
// `git ls-files` resolver the collector package injects, keeping this
// discovery-package test git-free.
func TestResolveRepositoryFileSetsGitTrackedResolverKeepsGitignoredTrackedFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustMkdirGit(t, repo)
	mustWriteFile(t, filepath.Join(repo, ".gitignore"), "*.tfstate\n")
	mustWriteFile(t, filepath.Join(repo, "terraform.tfstate"), "{}")
	mustWriteFile(t, filepath.Join(repo, "scratch.tfstate"), "{}")

	resolvedRepo := mustResolvePath(t, repo)
	resolver := func(repoRoot string) (map[string]struct{}, bool) {
		if repoRoot != resolvedRepo {
			return nil, false
		}
		return map[string]struct{}{"terraform.tfstate": {}}, true
	}

	stats, got, err := ResolveRepositoryFileSetsWithStats(
		root,
		func(path string) bool { return filepath.Ext(path) == ".tfstate" },
		Options{
			IgnoredDirs:        []string{".git"},
			HonorGitignore:     true,
			GitTrackedResolver: resolver,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() error = %v, want nil", err)
	}

	if repoFileSetsContainSuffix(got, "scratch.tfstate") {
		t.Fatalf("fileSets unexpectedly kept untracked scratch.tfstate; fileSets=%v", got)
	}
	if !repoFileSetsContainSuffix(got, "terraform.tfstate") {
		t.Fatalf("fileSets missing tracked terraform.tfstate despite gitignore match; fileSets=%v", got)
	}
	if got, want := stats.FilesSkippedGitignore, 1; got != want {
		t.Fatalf("FilesSkippedGitignore = %d, want %d (only untracked scratch.tfstate)", got, want)
	}
}

// TestResolveRepositoryFileSetsGitTrackedResolverNotOKFallsBackToPlainGitignore
// proves that when the resolver reports ok=false (non-git directory, or git
// itself unavailable), gitignore filtering behaves exactly as it did before
// #5591 — every matching file is skipped regardless of any hypothetical
// tracked status, since tracked status is unknown.
func TestResolveRepositoryFileSetsGitTrackedResolverNotOKFallsBackToPlainGitignore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustMkdirGit(t, repo)
	mustWriteFile(t, filepath.Join(repo, ".gitignore"), "*.tfstate\n")
	mustWriteFile(t, filepath.Join(repo, "terraform.tfstate"), "{}")

	resolver := func(string) (map[string]struct{}, bool) {
		return nil, false
	}

	stats, got, err := ResolveRepositoryFileSetsWithStats(
		root,
		func(path string) bool { return filepath.Ext(path) == ".tfstate" },
		Options{
			IgnoredDirs:        []string{".git"},
			HonorGitignore:     true,
			GitTrackedResolver: resolver,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() error = %v, want nil", err)
	}

	if repoFileSetsContainSuffix(got, "terraform.tfstate") {
		t.Fatalf("fileSets unexpectedly kept terraform.tfstate when resolver reported ok=false; fileSets=%v", got)
	}
	if got, want := stats.FilesSkippedGitignore, 1; got != want {
		t.Fatalf("FilesSkippedGitignore = %d, want %d", got, want)
	}
}

// TestResolveRepositoryFileSetsGitTrackedResolverStillHonorsEshuIgnore proves
// the #5591 rule is scoped to .gitignore only: .eshuignore remains the
// operator opt-out and still skips a tracked file. The skip must stay
// individually visible via the capped TrackedFilesSkippedEshuIgnore list
// rather than disappearing into the aggregate FilesSkippedEshuIgnore count.
func TestResolveRepositoryFileSetsGitTrackedResolverStillHonorsEshuIgnore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	mustMkdirGit(t, repo)
	mustWriteFile(t, filepath.Join(repo, ".eshuignore"), "*.tfstate\n")
	mustWriteFile(t, filepath.Join(repo, "terraform.tfstate"), "{}")

	resolvedRepo := mustResolvePath(t, repo)
	resolver := func(repoRoot string) (map[string]struct{}, bool) {
		if repoRoot != resolvedRepo {
			return nil, false
		}
		return map[string]struct{}{"terraform.tfstate": {}}, true
	}

	stats, got, err := ResolveRepositoryFileSetsWithStats(
		root,
		func(path string) bool { return filepath.Ext(path) == ".tfstate" },
		Options{
			IgnoredDirs:        []string{".git"},
			HonorEshuIgnore:    true,
			GitTrackedResolver: resolver,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() error = %v, want nil", err)
	}

	if repoFileSetsContainSuffix(got, "terraform.tfstate") {
		t.Fatalf("fileSets unexpectedly kept eshuignore-matched terraform.tfstate; fileSets=%v", got)
	}
	if got, want := stats.FilesSkippedEshuIgnore, 1; got != want {
		t.Fatalf("FilesSkippedEshuIgnore = %d, want %d", got, want)
	}
	if got, want := len(stats.TrackedFilesSkippedEshuIgnore), 1; got != want {
		t.Fatalf("len(TrackedFilesSkippedEshuIgnore) = %d, want %d", got, want)
	}
	if got, want := stats.TrackedFilesSkippedEshuIgnore[0], "terraform.tfstate"; got != want {
		t.Fatalf("TrackedFilesSkippedEshuIgnore[0] = %q, want %q", got, want)
	}
}

// TestResolveRepositoryFileSetsGitTrackedResolverInvokedPerRepoRoot proves the
// resolver is invoked once per DISCOVERED repository root, not once for the
// whole scan root — a nested repository (e.g. a submodule with its own
// .git) must get its own tracked-set lookup keyed by its own root.
func TestResolveRepositoryFileSetsGitTrackedResolverInvokedPerRepoRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outerRepo := filepath.Join(root, "outer")
	nestedRepo := filepath.Join(outerRepo, "nested")
	mustMkdirGit(t, outerRepo)
	mustMkdirGit(t, nestedRepo)
	mustWriteFile(t, filepath.Join(outerRepo, ".gitignore"), "*.tfstate\n")
	mustWriteFile(t, filepath.Join(outerRepo, "outer.tfstate"), "{}")
	mustWriteFile(t, filepath.Join(nestedRepo, ".gitignore"), "*.tfstate\n")
	mustWriteFile(t, filepath.Join(nestedRepo, "nested.tfstate"), "{}")

	resolvedOuter := mustResolvePath(t, outerRepo)
	resolvedNested := mustResolvePath(t, nestedRepo)
	invoked := map[string]int{}
	resolver := func(repoRoot string) (map[string]struct{}, bool) {
		invoked[repoRoot]++
		switch repoRoot {
		case resolvedOuter:
			return map[string]struct{}{"outer.tfstate": {}}, true
		case resolvedNested:
			// The nested repo's own tracked set does NOT include
			// nested.tfstate, so it must still be skipped there.
			return map[string]struct{}{}, true
		default:
			return nil, false
		}
	}

	stats, got, err := ResolveRepositoryFileSetsWithStats(
		root,
		func(path string) bool { return filepath.Ext(path) == ".tfstate" },
		Options{
			IgnoredDirs:        []string{".git"},
			HonorGitignore:     true,
			GitTrackedResolver: resolver,
		},
	)
	if err != nil {
		t.Fatalf("ResolveRepositoryFileSetsWithStats() error = %v, want nil", err)
	}

	if invoked[resolvedOuter] != 1 {
		t.Fatalf("resolver invoked %d times for outer repo root, want 1", invoked[resolvedOuter])
	}
	if invoked[resolvedNested] != 1 {
		t.Fatalf("resolver invoked %d times for nested repo root, want 1", invoked[resolvedNested])
	}
	if !repoFileSetsContainSuffix(got, "outer/outer.tfstate") {
		t.Fatalf("fileSets missing tracked outer/outer.tfstate; fileSets=%v", got)
	}
	if repoFileSetsContainSuffix(got, "nested/nested.tfstate") {
		t.Fatalf("fileSets unexpectedly kept nested.tfstate not tracked in the nested repo's own resolver result; fileSets=%v", got)
	}
	if got, want := stats.FilesSkippedGitignore, 1; got != want {
		t.Fatalf("FilesSkippedGitignore = %d, want %d", got, want)
	}
}
