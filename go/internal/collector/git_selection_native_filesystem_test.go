// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeRepositorySelectorSelectRepositoriesFilesystemPreservesGitHubWorkflows(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(
		t,
		filepath.Join(sourceRepo, ".github", "workflows", "deploy.yml"),
		"name: deploy\non:\n  push:\n",
	)

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

	wantWorkflowPath := filepath.Join(reposDir, "eshu-hq", "service-a", ".github", "workflows", "deploy.yml")
	if _, err := os.Stat(wantWorkflowPath); err != nil {
		t.Fatalf("copied repository missing GitHub workflow %q: %v", wantWorkflowPath, err)
	}
}

// TestNativeRepositorySelectorSelectRepositoriesFilesystemPreservesGitlabCI proves
// the filesystem selector copies a root-level hidden .gitlab-ci.yml into the
// managed workspace instead of pruning it as a dotfile. The managed-workspace copy
// drops dot-prefixed entries unless preserveFilesystemHiddenPath allows them; the
// GitLab CI pipeline file is a root-level hidden file (not a directory prefix like
// .github/workflows), so the allowlist must match it by exact relative path or the
// file never reaches discovery, the parser, or the graph.
func TestNativeRepositorySelectorSelectRepositoriesFilesystemPreservesGitlabCI(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(
		t,
		filepath.Join(sourceRepo, ".gitlab-ci.yml"),
		"stages:\n  - build\nbuild-app:\n  stage: build\n  script: [\"go build ./...\"]\n",
	)
	writeSelectionTestFile(
		t,
		filepath.Join(sourceRepo, "nested", ".gitlab-ci.yaml"),
		"stages: [test]\nunit:\n  stage: test\n  script: [\"go test ./...\"]\n",
	)
	writeSelectionTestFile(
		t,
		filepath.Join(sourceRepo, ".gitmodules"),
		"[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n",
	)

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

	for _, rel := range []string{".gitlab-ci.yml", filepath.Join("nested", ".gitlab-ci.yaml"), ".gitmodules"} {
		wantPath := filepath.Join(reposDir, "eshu-hq", "service-a", rel)
		if _, err := os.Stat(wantPath); err != nil {
			t.Fatalf("copied repository missing GitLab CI config %q: %v", wantPath, err)
		}
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemReturnsEmptyBatchWhenManifestUnchanged(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

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
		Now: func() time.Time {
			return time.Date(2026, time.April, 13, 20, 30, 0, 0, time.UTC)
		},
	}

	firstBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("first SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(firstBatch.Repositories), 1; got != want {
		t.Fatalf("len(first.Repositories) = %d, want %d", got, want)
	}

	secondBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("second SelectRepositories() error = %v, want nil", err)
	}
	if got := len(secondBatch.Repositories); got != 0 {
		t.Fatalf("len(second.Repositories) = %d, want 0 when filesystem manifest is unchanged", got)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemIgnoresGitignoredManifestChurn(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "generated.log\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "generated.log"), "first\n")

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

	firstBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("first SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(firstBatch.Repositories), 1; got != want {
		t.Fatalf("len(first.Repositories) = %d, want %d", got, want)
	}

	writeSelectionTestFile(t, filepath.Join(sourceRepo, "generated.log"), "second\n")
	secondBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("second SelectRepositories() error = %v, want nil", err)
	}
	if got := len(secondBatch.Repositories); got != 0 {
		t.Fatalf("len(second.Repositories) = %d, want 0 when only gitignored files changed", got)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemIgnoresEshuignoredManifestChurn(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".eshuignore"), "scratch.json\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "scratch.json"), "{\"version\":1}\n")

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

	firstBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("first SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(firstBatch.Repositories), 1; got != want {
		t.Fatalf("len(first.Repositories) = %d, want %d", got, want)
	}

	writeSelectionTestFile(t, filepath.Join(sourceRepo, "scratch.json"), "{\"version\":2}\n")
	secondBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("second SelectRepositories() error = %v, want nil", err)
	}
	if got := len(secondBatch.Repositories); got != 0 {
		t.Fatalf("len(second.Repositories) = %d, want 0 when only eshuignored files changed", got)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemReselectsWhenIgnoreRulesChange(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "generated.log\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "generated.log"), "first\n")

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

	firstBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("first SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(firstBatch.Repositories), 1; got != want {
		t.Fatalf("len(first.Repositories) = %d, want %d", got, want)
	}

	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".gitignore"), "other.log\n")
	secondBatch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("second SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(secondBatch.Repositories), 1; got != want {
		t.Fatalf("len(second.Repositories) = %d, want %d when ignore rules changed", got, want)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemRootRepository(t *testing.T) {
	t.Parallel()

	sourceRepo := t.TempDir()
	reposDir := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	observedAt := time.Date(2026, time.April, 13, 21, 15, 0, 0, time.UTC)
	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:       reposDir,
			SourceMode:     "filesystem",
			FilesystemRoot: sourceRepo,
			Component:      "bootstrap-index",
			CloneDepth:     1,
			RepoLimit:      4000,
			GitAuthMethod:  "none",
		},
		Now: func() time.Time {
			return observedAt
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}

	selectedRepo := batch.Repositories[0]
	wantRepoPath := filepath.Join(reposDir, filepath.Base(sourceRepo))
	if got, want := selectedRepo.RepoPath, resolveRepoPathForAssertion(t, wantRepoPath); got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
	if got, want := selectedRepo.GitTreePath, resolveRepoPathForAssertion(t, sourceRepo); got != want {
		t.Fatalf("GitTreePath = %q, want source repository %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(wantRepoPath, "main.go")); err != nil {
		t.Fatalf("copied repository missing main.go: %v", err)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemDirectRootRepository(t *testing.T) {
	t.Parallel()

	sourceRepo := t.TempDir()
	reposDir := t.TempDir()
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:         reposDir,
			SourceMode:       "filesystem",
			FilesystemRoot:   sourceRepo,
			FilesystemDirect: true,
			Component:        "bootstrap-index",
			CloneDepth:       1,
			RepoLimit:        4000,
			GitAuthMethod:    "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}
	if got, want := batch.Repositories[0].RepoPath, resolveRepoPathForAssertion(t, sourceRepo); got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
}

func TestNativeRepositorySelectorSelectRepositoriesFilesystemDirectWorkspaceRepositories(t *testing.T) {
	t.Parallel()

	filesystemRoot := t.TempDir()
	reposDir := t.TempDir()
	sourceRepo := filepath.Join(filesystemRoot, "eshu-hq", "service-a")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeSelectionTestFile(t, filepath.Join(sourceRepo, "main.go"), "package main\n")

	selector := NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:         reposDir,
			SourceMode:       "filesystem",
			FilesystemRoot:   filesystemRoot,
			FilesystemDirect: true,
			Component:        "workspace-index",
			CloneDepth:       1,
			RepoLimit:        4000,
			GitAuthMethod:    "none",
		},
	}

	batch, err := selector.SelectRepositories(context.Background())
	if err != nil {
		t.Fatalf("SelectRepositories() error = %v, want nil", err)
	}
	if got, want := len(batch.Repositories), 1; got != want {
		t.Fatalf("len(Repositories) = %d, want %d", got, want)
	}
	if got, want := batch.Repositories[0].RepoPath, resolveRepoPathForAssertion(t, sourceRepo); got != want {
		t.Fatalf("RepoPath = %q, want %q", got, want)
	}
}
