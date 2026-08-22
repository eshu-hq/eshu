// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

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
	copyRepositoryTreeFn = func(
		ctx context.Context,
		sourceRoot string,
		targetRoot string,
		expectation *managedCopyCommitExpectation,
	) (bool, error) {
		writeFile(t, sourceRoot, workflowPath, strings.ReplaceAll(workflowImageCommitFixture, ":prod", ":dirty"))
		matches, err := originalCopy(ctx, sourceRoot, targetRoot, expectation)
		if err != nil {
			return false, err
		}
		runGit(t, sourceRoot, "reset", "--hard", "HEAD")
		return matches, nil
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

func TestFilesystemManagedCopyDoesNotCarryCommitIfTrackedFileRestoredAfterCopy(t *testing.T) {
	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".github", "workflows"), 0o750); err != nil {
		t.Fatalf("create workflow directory: %v", err)
	}
	workflowPath := filepath.Join(".github", "workflows", "build.yml")
	retainedWorkflowPath := filepath.Join(".github", "workflows", "retained.yml")
	writeFile(t, sourcePath, workflowPath, workflowImageCommitFixture)
	writeFile(t, sourcePath, retainedWorkflowPath, strings.ReplaceAll(workflowImageCommitFixture, ":prod", ":retained"))
	writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")

	originalCopy := copyRepositoryTreeFn
	copyRepositoryTreeFn = func(
		ctx context.Context,
		sourceRoot string,
		targetRoot string,
		expectation *managedCopyCommitExpectation,
	) (bool, error) {
		if err := os.Remove(filepath.Join(sourceRoot, workflowPath)); err != nil {
			return false, err
		}
		matches, err := originalCopy(ctx, sourceRoot, targetRoot, expectation)
		writeFile(t, sourceRoot, workflowPath, workflowImageCommitFixture)
		return matches, err
	}
	t.Cleanup(func() { copyRepositoryTreeFn = originalCopy })

	config := RepoSyncConfig{SourceMode: "filesystem", FilesystemRoot: sourcePath, ReposDir: t.TempDir()}
	synced, err := syncFilesystemRepositories(context.Background(), config, []string{"."})
	if err != nil {
		t.Fatalf("syncFilesystemRepositories() error = %v", err)
	}
	managedPath := synced.SelectedRepoPaths[0]
	if _, err := os.Stat(filepath.Join(managedPath, workflowPath)); !os.IsNotExist(err) {
		t.Fatalf("managed workflow stat error = %v, want omitted file", err)
	}
	if got := synced.SourceCommitSHAByRepoPath[canonicalLocalPath(managedPath)]; got != "" {
		t.Fatalf("copy-bound SourceCommitSHA = %q, want empty after tracked-file omission", got)
	}
	if got := gitCleanWorktreeCommitSHA(context.Background(), sourcePath); got == "" {
		t.Fatal("source commit after copy is empty, want restored clean checkout")
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
		t.Fatalf("HeadCommitSHA = %q, want empty for omitted tracked workflow", snapshot.HeadCommitSHA)
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
	copyRepositoryTreeFn = func(
		ctx context.Context,
		sourceRoot string,
		targetRoot string,
		expectation *managedCopyCommitExpectation,
	) (bool, error) {
		if err := os.MkdirAll(filepath.Join(sourceRoot, ".github", "workflows"), 0o750); err != nil {
			return false, err
		}
		writeFile(t, sourceRoot, workflowPath, workflowImageCommitFixture)
		matches, err := originalCopy(ctx, sourceRoot, targetRoot, expectation)
		if err != nil {
			return false, err
		}
		if err := os.Remove(filepath.Join(sourceRoot, workflowPath)); err != nil {
			return false, err
		}
		return matches, nil
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

func TestFilesystemManagedCopyRejectsGitTextTransform(t *testing.T) {
	sourcePath, workflowPath, wantCommit := prepareCRLFManagedCopyRepository(t)
	config := RepoSyncConfig{SourceMode: "filesystem", FilesystemRoot: sourcePath, ReposDir: t.TempDir()}

	synced, err := syncFilesystemRepositories(context.Background(), config, []string{"."})
	if err != nil {
		t.Fatalf("syncFilesystemRepositories() error = %v", err)
	}
	managedPath := synced.SelectedRepoPaths[0]
	if got := synced.SourceCommitSHAByRepoPath[canonicalLocalPath(managedPath)]; got != "" {
		t.Fatalf("copy-bound SourceCommitSHA = %q, want empty for transformed bytes", got)
	}
	managedWorkflow, err := os.ReadFile(filepath.Join(managedPath, workflowPath))
	if err != nil {
		t.Fatalf("read managed workflow: %v", err)
	}
	if !strings.Contains(string(managedWorkflow), "\r\n") {
		t.Fatalf("managed workflow bytes = %q, want copied CRLF working-tree bytes", managedWorkflow)
	}
	if got := gitCleanWorktreeCommitSHA(context.Background(), sourcePath); got != wantCommit {
		t.Fatalf("source commit after copy = %q, want clean transformed commit %q", got, wantCommit)
	}
}

func TestFilesystemManagedCopyDoesNotExecuteGitCleanFilter(t *testing.T) {
	sourcePath, _, wantCommit := prepareCRLFManagedCopyRepository(t)
	sentinelPath := filepath.Join(t.TempDir(), "filter-ran")
	runGit(t, sourcePath, "config", "filter.hostile.clean", "touch "+sentinelPath)
	expectation := loadManagedCopyCommitExpectation(context.Background(), sourcePath, wantCommit)
	if expectation == nil {
		t.Fatal("loadManagedCopyCommitExpectation() = nil, want immutable commit expectation")
	}
	matches, err := copyRepositoryTreeWithExpectation(
		context.Background(), sourcePath, t.TempDir(), expectation,
	)
	if err != nil {
		t.Fatalf("copyRepositoryTreeWithExpectation() error = %v", err)
	}
	if matches {
		t.Fatal("copyRepositoryTreeWithExpectation() matches = true, want transformed bytes to fail closed")
	}
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Fatalf("hostile clean filter sentinel stat error = %v, want filter command not executed", err)
	}
}

func TestManagedCopyDistinguishesCommittedSymlinkFromRegularFile(t *testing.T) {
	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
	writeFile(t, sourcePath, "expected.py", "VALUE = 'committed'\n")
	if err := os.Symlink("main.py", filepath.Join(sourcePath, "link.py")); err != nil {
		t.Fatalf("create committed symlink: %v", err)
	}
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")
	commitSHA := runGit(t, sourcePath, "rev-parse", "HEAD")

	cleanExpectation := loadManagedCopyCommitExpectation(context.Background(), sourcePath, commitSHA)
	cleanMatches, err := copyRepositoryTreeWithExpectation(
		context.Background(), sourcePath, t.TempDir(), cleanExpectation,
	)
	if err != nil || !cleanMatches {
		t.Fatalf("clean managed copy matches=%t error=%v, want committed symlink accepted", cleanMatches, err)
	}

	if err := os.Remove(filepath.Join(sourcePath, "expected.py")); err != nil {
		t.Fatalf("remove committed regular file: %v", err)
	}
	if err := os.Symlink("main.py", filepath.Join(sourcePath, "expected.py")); err != nil {
		t.Fatalf("replace regular file with symlink: %v", err)
	}
	mutatedExpectation := loadManagedCopyCommitExpectation(context.Background(), sourcePath, commitSHA)
	mutatedMatches, err := copyRepositoryTreeWithExpectation(
		context.Background(), sourcePath, t.TempDir(), mutatedExpectation,
	)
	if err != nil {
		t.Fatalf("mutated copy error = %v", err)
	}
	if mutatedMatches {
		t.Fatal("mutated managed copy matches = true, want regular-to-symlink replacement rejected")
	}
}

func prepareCRLFManagedCopyRepository(t *testing.T) (string, string, string) {
	t.Helper()
	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	workflowPath := filepath.Join(".github", "workflows", "build.yml")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".github", "workflows"), 0o750); err != nil {
		t.Fatalf("create workflow directory: %v", err)
	}
	writeFile(t, sourcePath, ".gitattributes", "*.yml text eol=crlf filter=hostile\n")
	writeFile(t, sourcePath, workflowPath, workflowImageCommitFixture)
	writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")
	wantCommit := runGit(t, sourcePath, "rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(sourcePath, workflowPath)); err != nil {
		t.Fatalf("remove workflow before checkout: %v", err)
	}
	runGit(t, sourcePath, "checkout", "--", workflowPath)
	workflowBytes, err := os.ReadFile(filepath.Join(sourcePath, workflowPath))
	if err != nil {
		t.Fatalf("read transformed workflow: %v", err)
	}
	if !strings.Contains(string(workflowBytes), "\r\n") {
		t.Fatalf("transformed workflow bytes = %q, want CRLF", workflowBytes)
	}
	if got := gitCleanWorktreeCommitSHA(context.Background(), sourcePath); got != wantCommit {
		t.Fatalf("clean transformed source commit = %q, want %q", got, wantCommit)
	}
	return sourcePath, workflowPath, wantCommit
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
	fileCount, byteCount, err := managedCopyBenchmarkTreeSize(targetPath)
	if err != nil {
		b.Fatalf("measure managed copy: %v", err)
	}
	b.Logf("synthetic managed copy commit=%s files=%d bytes=%d", commitSHA, fileCount, byteCount)

	b.Run("prior-full-managed-copy", func(b *testing.B) {
		fullTargetPath := filepath.Join(b.TempDir(), "managed")
		for b.Loop() {
			if err := copyRepositoryTree(context.Background(), sourcePath, fullTargetPath); err != nil {
				b.Fatalf("copyRepositoryTree() error = %v", err)
			}
			if output := runGitBenchmark(b, sourcePath, "rev-parse", "HEAD"); strings.TrimSpace(output) != commitSHA {
				b.Fatalf("rev-parse HEAD = %q, want %q", output, commitSHA)
			}
		}
	})
	b.Run("copy-bound-full-managed-copy", func(b *testing.B) {
		fullTargetPath := filepath.Join(b.TempDir(), "managed")
		for b.Loop() {
			observedCommit := gitCleanWorktreeCommitSHA(context.Background(), sourcePath)
			if observedCommit != commitSHA {
				b.Fatalf("clean source commit = %q, want %q", observedCommit, commitSHA)
			}
			expectation := loadManagedCopyCommitExpectation(context.Background(), sourcePath, observedCommit)
			matches, err := copyRepositoryTreeFn(
				context.Background(), sourcePath, fullTargetPath, expectation,
			)
			if err != nil || !matches {
				b.Fatalf("copy-bound managed copy matches=%t error=%v", matches, err)
			}
		}
	})
}

func BenchmarkFilesystemManagedCopyCommitAttributionLargeRepository(b *testing.B) {
	sourcePath := strings.TrimSpace(os.Getenv("ESHU_BENCHMARK_REPOSITORY"))
	if sourcePath == "" {
		b.Skip("set ESHU_BENCHMARK_REPOSITORY to a clean representative checkout")
	}
	commitSHA := gitCleanWorktreeCommitSHA(context.Background(), sourcePath)
	if commitSHA == "" {
		b.Fatal("ESHU_BENCHMARK_REPOSITORY must be a clean Git checkout")
	}
	targetPath := filepath.Join(b.TempDir(), "managed")
	if err := copyRepositoryTree(context.Background(), sourcePath, targetPath); err != nil {
		b.Fatalf("copyRepositoryTree() error = %v", err)
	}
	fileCount, byteCount, err := managedCopyBenchmarkTreeSize(targetPath)
	if err != nil {
		b.Fatalf("measure managed copy: %v", err)
	}
	b.Logf("representative managed copy commit=%s files=%d bytes=%d", commitSHA, fileCount, byteCount)

	b.Run("prior-full-managed-copy", func(b *testing.B) {
		fullTargetPath := filepath.Join(b.TempDir(), "managed")
		for b.Loop() {
			if err := copyRepositoryTree(context.Background(), sourcePath, fullTargetPath); err != nil {
				b.Fatalf("copyRepositoryTree() error = %v", err)
			}
			if output := runGitBenchmark(b, sourcePath, "rev-parse", "HEAD"); strings.TrimSpace(output) != commitSHA {
				b.Fatalf("rev-parse HEAD = %q, want %q", output, commitSHA)
			}
		}
		b.ReportMetric(float64(fileCount), "files")
		b.ReportMetric(float64(byteCount), "bytes")
	})
	b.Run("copy-bound-full-managed-copy", func(b *testing.B) {
		fullTargetPath := filepath.Join(b.TempDir(), "managed")
		for b.Loop() {
			observedCommit := gitCleanWorktreeCommitSHA(context.Background(), sourcePath)
			if observedCommit != commitSHA {
				b.Fatalf("clean source commit = %q, want %q", observedCommit, commitSHA)
			}
			expectation := loadManagedCopyCommitExpectation(context.Background(), sourcePath, observedCommit)
			matches, err := copyRepositoryTreeFn(
				context.Background(), sourcePath, fullTargetPath, expectation,
			)
			if err != nil || !matches {
				b.Fatalf("copy-bound managed copy matches=%t error=%v", matches, err)
			}
		}
		b.ReportMetric(float64(fileCount), "files")
		b.ReportMetric(float64(byteCount), "bytes")
	})
}

func managedCopyBenchmarkTreeSize(root string) (int, int64, error) {
	fileCount := 0
	byteCount := int64(0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fileCount++
		byteCount += info.Size()
		return nil
	})
	return fileCount, byteCount, err
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
