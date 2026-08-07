// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestManagedCopySubmodulePinBindsToCopiedCommitAfterSourceAdvances(t *testing.T) {
	t.Parallel()

	sourcePath := t.TempDir()
	mustInitGitRepo(t, sourcePath)
	submoduleOrigin := t.TempDir()
	mustInitGitRepo(t, submoduleOrigin)
	writeSelectionTestFile(t, filepath.Join(submoduleOrigin, "README.md"), "submodule A\n")
	runGit(t, submoduleOrigin, "add", "README.md")
	runGit(t, submoduleOrigin, "commit", "-m", "submodule A")
	submoduleCommitA := runGit(t, submoduleOrigin, "rev-parse", "HEAD")
	writeSelectionTestFile(t, filepath.Join(submoduleOrigin, "README.md"), "submodule B\n")
	runGit(t, submoduleOrigin, "add", "README.md")
	runGit(t, submoduleOrigin, "commit", "-m", "submodule B")
	submoduleCommitB := runGit(t, submoduleOrigin, "rev-parse", "HEAD")

	writeSelectionTestFile(t, filepath.Join(sourcePath, ".eshuignore"), "lib/foo/\n")
	writeSelectionTestFile(t, filepath.Join(sourcePath, "main.go"), "package main\n")
	runGit(t, sourcePath, "-c", "protocol.file.allow=always", "submodule", "add", submoduleOrigin, "lib/foo")
	runGit(t, filepath.Join(sourcePath, "lib", "foo"), "checkout", submoduleCommitA)
	runGit(t, sourcePath, "add", ".eshuignore", ".gitmodules", "lib/foo", "main.go")
	runGit(t, sourcePath, "commit", "-m", "commit A")
	commitA := runGit(t, sourcePath, "rev-parse", "HEAD")

	selected := selectManagedCopyForCommitReadTest(t, sourcePath)
	if selected.SourceCommitSHA != commitA {
		t.Fatalf("SourceCommitSHA = %q, want copied commit %q", selected.SourceCommitSHA, commitA)
	}

	runGit(t, filepath.Join(sourcePath, "lib", "foo"), "checkout", submoduleCommitB)
	runGit(t, sourcePath, "add", "lib/foo")
	runGit(t, sourcePath, "commit", "-m", "commit B")

	snapshot, err := (NativeRepositorySnapshotter{}).SnapshotRepository(context.Background(), selected)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	repo := testCollectorRepositoryMetadata(selected.RepoPath)
	collected := buildStreamingGeneration(
		selected.RepoPath,
		repo,
		"managed-copy-run",
		time.Date(2026, time.August, 7, 3, 0, 0, 0, time.UTC),
		snapshot,
		false,
		"",
	)
	pinFacts := factsByKind(drainFactChannel(collected.Facts), facts.SubmodulePinFactKind)
	if got, want := len(pinFacts), 1; got != want {
		t.Fatalf("submodule.pin fact count = %d, want %d", got, want)
	}
	if got, want := pinFacts[0].Payload["pinned_sha"], submoduleCommitA; got != want {
		t.Fatalf("pinned_sha = %#v, want copied-commit gitlink %q", got, want)
	}
}

func selectManagedCopyForCommitReadTest(t *testing.T, sourcePath string) SelectedRepository {
	t.Helper()

	config := RepoSyncConfig{
		ReposDir:       t.TempDir(),
		SourceMode:     "filesystem",
		FilesystemRoot: sourcePath,
	}
	synced, err := syncFilesystemRepositories(context.Background(), config, []string{"."})
	if err != nil {
		t.Fatalf("syncFilesystemRepositories() error = %v", err)
	}
	repositories := buildSelectedRepositories(
		config,
		synced.SelectedRepoPaths,
		nil,
		nil,
		synced.SourceCommitSHAByRepoPath,
		nil,
		nil,
	)
	attachFilesystemGitTreePaths(config, []string{"."}, repositories)
	if got, want := len(repositories), 1; got != want {
		t.Fatalf("selected repositories = %d, want %d", got, want)
	}
	return repositories[0]
}
