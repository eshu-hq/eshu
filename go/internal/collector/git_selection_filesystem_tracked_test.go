// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestNativeRepositorySelectorSelectRepositoriesFilesystemCopiesTrackedIgnoredFile
// proves the issue #5591 fix at site 2 (the filesystem managed-copy path):
// copyRepositoryTree resolves gitTrackedFiles against sourceRoot (the git
// checkout) BEFORE the tree is copied, so a force-committed
// (`git add -f`) file that matches the source repo's own .gitignore rule
// lands in the managed copy, while a genuinely untracked file matching the
// same rule does not.
func TestNativeRepositorySelectorSelectRepositoriesFilesystemCopiesTrackedIgnoredFile(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	mustInitGitRepo(t, sourceRepo)

	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "terraform.tfstate"), "{}")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "scratch.tfstate"), "{}")
	runGit(t, sourceRepo, "add", "main.go", ".gitignore")
	runGit(t, sourceRepo, "add", "-f", "terraform.tfstate")
	runGit(t, sourceRepo, "commit", "-m", "initial")
	// scratch.tfstate is intentionally never `git add`ed.

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	copiedRoot := filepath.Join(reposDir, "eshu-hq", "service-a")
	if _, err := os.Stat(filepath.Join(copiedRoot, "terraform.tfstate")); err != nil {
		t.Fatalf("managed copy missing tracked terraform.tfstate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(copiedRoot, "scratch.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("managed copy unexpectedly contains untracked scratch.tfstate (stat err = %v, want IsNotExist)", err)
	}
}

// TestNativeRepositorySelectorSelectRepositoriesFilesystemStillCopiesNoEshuIgnoredTrackedFile
// proves .eshuignore remains the operator's own opt-out in the managed-copy
// path too: it still excludes a file git tracks from the copy, unlike
// .gitignore.
func TestNativeRepositorySelectorSelectRepositoriesFilesystemStillSkipsEshuIgnoredTrackedFile(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-b")
	if err := os.MkdirAll(sourceRepo, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	mustInitGitRepo(t, sourceRepo)

	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".eshuignore"), "*.tfstate\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "terraform.tfstate"), "{}")
	runGit(t, sourceRepo, "add", "main.go")
	runGit(t, sourceRepo, "add", "-f", "terraform.tfstate")
	runGit(t, sourceRepo, "commit", "-m", "initial")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: filesystemRoot,
			Component:      "collector-git",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
	}

	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}

	copiedRoot := filepath.Join(reposDir, "eshu-hq", "service-b")
	if _, err := os.Stat(filepath.Join(copiedRoot, "terraform.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("managed copy unexpectedly contains eshuignored terraform.tfstate (stat err = %v, want IsNotExist)", err)
	}
}
