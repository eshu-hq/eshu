// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemManagedCopyHonorsIgnoredUntrackedEshuignore(t *testing.T) {
	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	writeFile(t, sourcePath, ".gitignore", ".eshuignore\n")
	writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
	writeFile(t, sourcePath, "operator-secret.txt", "must not enter the managed copy\n")
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")
	wantCommit := runGit(t, sourcePath, "rev-parse", "HEAD")
	writeFile(t, sourcePath, ".eshuignore", "operator-secret.txt\n")
	if got := gitCleanWorktreeCommitSHA(context.Background(), sourcePath); got != wantCommit {
		t.Fatalf("clean source commit = %q, want ignored-control commit %q", got, wantCommit)
	}

	config := RepoSyncConfig{SourceMode: "filesystem", FilesystemRoot: sourcePath, ReposDir: t.TempDir()}
	synced, err := syncFilesystemRepositories(context.Background(), config, []string{"."})
	if err != nil {
		t.Fatalf("syncFilesystemRepositories() error = %v", err)
	}
	managedPath := synced.SelectedRepoPaths[0]
	if _, err := os.Stat(filepath.Join(managedPath, "operator-secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("excluded operator secret stat error = %v, want file absent", err)
	}
	if got := synced.SourceCommitSHAByRepoPath[canonicalLocalPath(managedPath)]; got != wantCommit {
		t.Fatalf("copy-bound SourceCommitSHA = %q, want %q with snapshotted local policy", got, wantCommit)
	}
}
