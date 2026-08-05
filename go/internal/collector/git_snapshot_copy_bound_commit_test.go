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

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestFilesystemManagedCopyDoesNotCarryCommitIfSourceCleansAfterCopy(t *testing.T) {
	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".github", "workflows"), 0o750); err != nil {
		t.Fatalf("create workflow directory: %v", err)
	}
	workflowPath := filepath.Join(".github", "workflows", "build.yml")
	writeFile(t, sourcePath, workflowPath, workflowImageCommitFixture)
	writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")

	originalCopy := copyRepositoryTreeFn
	copyRepositoryTreeFn = func(ctx context.Context, sourceRoot, targetRoot string) error {
		writeFile(t, sourceRoot, workflowPath, strings.ReplaceAll(workflowImageCommitFixture, ":prod", ":dirty"))
		if err := originalCopy(ctx, sourceRoot, targetRoot); err != nil {
			return err
		}
		runGit(t, sourceRoot, "reset", "--hard", "HEAD")
		return nil
	}
	t.Cleanup(func() { copyRepositoryTreeFn = originalCopy })

	config := RepoSyncConfig{SourceMode: "filesystem", FilesystemRoot: sourcePath, ReposDir: t.TempDir()}
	synced, err := syncFilesystemRepositories(context.Background(), config, []string{"."})
	if err != nil {
		t.Fatalf("syncFilesystemRepositories() error = %v", err)
	}
	managedPath := synced.SelectedRepoPaths[0]
	managedWorkflow, err := os.ReadFile(filepath.Join(managedPath, workflowPath))
	if err != nil {
		t.Fatalf("read managed workflow: %v", err)
	}
	if !strings.Contains(string(managedWorkflow), ":dirty") {
		t.Fatalf("managed workflow = %q, want bytes copied before source reset", managedWorkflow)
	}
	if got := synced.SourceCommitSHAByRepoPath[canonicalLocalPath(managedPath)]; got != "" {
		t.Fatalf("copy-bound SourceCommitSHA = %q, want empty after dirty-to-clean interleaving", got)
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
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	repository := repositories[0]
	repository.Delta = true
	repository.FileTargets = []string{filepath.Join(managedPath, "main.py")}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(), repository,
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	if snapshot.HeadCommitSHA != "" {
		t.Fatalf("HeadCommitSHA = %q, want empty for bytes copied before source reset", snapshot.HeadCommitSHA)
	}
	assertWorkflowImageFactCommitSHA(t, managedPath, snapshot, "")
}

func TestFilesystemManagedCopyRejectsUntrackedFileCreatedDuringCopy(t *testing.T) {
	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")
	wantCommit := runGit(t, sourcePath, "rev-parse", "HEAD")
	workflowPath := filepath.Join(".github", "workflows", "untracked.yml")

	originalCopy := copyRepositoryTreeFn
	copyRepositoryTreeFn = func(ctx context.Context, sourceRoot, targetRoot string) error {
		if err := os.MkdirAll(filepath.Join(sourceRoot, ".github", "workflows"), 0o750); err != nil {
			return err
		}
		writeFile(t, sourceRoot, workflowPath, workflowImageCommitFixture)
		if err := originalCopy(ctx, sourceRoot, targetRoot); err != nil {
			return err
		}
		return os.Remove(filepath.Join(sourceRoot, workflowPath))
	}
	t.Cleanup(func() { copyRepositoryTreeFn = originalCopy })

	config := RepoSyncConfig{SourceMode: "filesystem", FilesystemRoot: sourcePath, ReposDir: t.TempDir()}
	synced, err := syncFilesystemRepositories(context.Background(), config, []string{"."})
	if err != nil {
		t.Fatalf("syncFilesystemRepositories() error = %v", err)
	}
	if got := gitCleanWorktreeCommitSHA(context.Background(), sourcePath); got != wantCommit {
		t.Fatalf("source commit after copy = %q, want restored clean %q", got, wantCommit)
	}
	managedPath := synced.SelectedRepoPaths[0]
	if _, err := os.Stat(filepath.Join(managedPath, workflowPath)); err != nil {
		t.Fatalf("stat copied untracked workflow: %v", err)
	}
	if got := synced.SourceCommitSHAByRepoPath[canonicalLocalPath(managedPath)]; got != "" {
		t.Fatalf("copy-bound SourceCommitSHA = %q, want empty for extra copied path", got)
	}
}

func BenchmarkFilesystemManagedCopyCommitAttribution(b *testing.B) {
	sourcePath := b.TempDir()
	runGitBenchmark(b, sourcePath, "init")
	runGitBenchmark(b, sourcePath, "config", "user.email", "test@example.com")
	runGitBenchmark(b, sourcePath, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".github", "workflows"), 0o750); err != nil {
		b.Fatalf("create workflow directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(sourcePath, ".github", "workflows", "build.yml"),
		[]byte(workflowImageCommitFixture),
		0o644,
	); err != nil {
		b.Fatalf("write workflow fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourcePath, "main.py"), []byte("def main():\n    pass\n"), 0o644); err != nil {
		b.Fatalf("write source fixture: %v", err)
	}
	runGitBenchmark(b, sourcePath, "add", ".")
	runGitBenchmark(b, sourcePath, "commit", "-m", "initial commit")
	commitSHA := strings.TrimSpace(runGitBenchmark(b, sourcePath, "rev-parse", "HEAD"))
	targetPath := filepath.Join(b.TempDir(), "managed")
	if err := copyRepositoryTree(context.Background(), sourcePath, targetPath); err != nil {
		b.Fatalf("copyRepositoryTree() error = %v", err)
	}

	b.Run("prior-rev-parse-head", func(b *testing.B) {
		for b.Loop() {
			if output := runGitBenchmark(b, sourcePath, "rev-parse", "HEAD"); strings.TrimSpace(output) != commitSHA {
				b.Fatalf("rev-parse HEAD = %q, want %q", output, commitSHA)
			}
		}
	})
	b.Run("immutable-tree-validation", func(b *testing.B) {
		for b.Loop() {
			if !managedCopyMatchesCommit(context.Background(), sourcePath, targetPath, commitSHA) {
				b.Fatal("managedCopyMatchesCommit() = false, want true")
			}
		}
	})
	b.Run("copy-bound-attribution", func(b *testing.B) {
		for b.Loop() {
			observedCommit := gitCleanWorktreeCommitSHA(context.Background(), sourcePath)
			if observedCommit != commitSHA ||
				!managedCopyMatchesCommit(context.Background(), sourcePath, targetPath, observedCommit) {
				b.Fatalf("copy-bound attribution failed for commit %q", observedCommit)
			}
		}
	})
}

func runGitBenchmark(b *testing.B, repoPath string, args ...string) string {
	b.Helper()
	commandArgs := append([]string{"-C", repoPath}, args...)
	output, err := exec.Command("git", commandArgs...).Output() // #nosec G204 -- benchmark helper with controlled arguments
	if err != nil {
		b.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(output)
}
