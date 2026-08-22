// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
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

func TestFilesystemManagedCopyRejectsTransientTrackedEshuignore(t *testing.T) {
	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".github", "workflows"), 0o750); err != nil {
		t.Fatalf("create workflow directory: %v", err)
	}
	workflowPath := filepath.Join(".github", "workflows", "build.yml")
	retainedPath := filepath.Join(".github", "workflows", "retained.yml")
	writeFile(t, sourcePath, ".eshuignore", "# committed policy\n")
	writeFile(t, sourcePath, workflowPath, workflowImageCommitFixture)
	writeFile(t, sourcePath, retainedPath, strings.ReplaceAll(workflowImageCommitFixture, ":prod", ":retained"))
	writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")
	wantCommit := runGit(t, sourcePath, "rev-parse", "HEAD")

	originalCopy := copyRepositoryTreeFn
	copyRepositoryTreeFn = func(
		ctx context.Context,
		sourceRoot string,
		targetRoot string,
		expectation *managedCopyCommitExpectation,
	) (bool, error) {
		writeFile(t, sourceRoot, ".eshuignore", filepath.ToSlash(workflowPath)+"\n")
		matches, err := originalCopy(ctx, sourceRoot, targetRoot, expectation)
		writeFile(t, sourceRoot, ".eshuignore", "# committed policy\n")
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
		t.Fatalf("transiently excluded workflow stat error = %v, want omitted file", err)
	}
	if _, err := os.Stat(filepath.Join(managedPath, retainedPath)); err != nil {
		t.Fatalf("retained workflow stat error = %v", err)
	}
	if got := synced.SourceCommitSHAByRepoPath[canonicalLocalPath(managedPath)]; got != "" {
		t.Fatalf("copy-bound SourceCommitSHA = %q, want empty for transient policy", got)
	}
	if got := gitCleanWorktreeCommitSHA(context.Background(), sourcePath); got != wantCommit {
		t.Fatalf("restored source commit = %q, want %q", got, wantCommit)
	}

	repositories := buildSelectedRepositories(
		config, synced.SelectedRepoPaths, nil, nil, synced.SourceCommitSHAByRepoPath, nil, nil,
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
		t.Fatalf("HeadCommitSHA = %q, want empty for transient policy", snapshot.HeadCommitSHA)
	}
	assertWorkflowImageFactCommitSHA(t, managedPath, snapshot, "")
}

func TestCopyRepositoryFileRejectsSymlinkSwapBeforeRead(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.txt")
	outsidePath := filepath.Join(t.TempDir(), "outside-secret.txt")
	targetPath := filepath.Join(t.TempDir(), "managed", "source.txt")
	if err := os.WriteFile(sourcePath, []byte("safe source\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(outsidePath, []byte("outside secret\n"), 0o600); err != nil {
		t.Fatalf("write outside sentinel: %v", err)
	}
	source := openManagedCopyTestRoot(t, filepath.Dir(sourcePath))
	installManagedCopySymlinkSwap(t, sourcePath, outsidePath)

	if _, err := copyRepositoryFile(
		source, filepath.Dir(sourcePath), sourcePath, targetPath, "source.txt", nil,
	); err == nil {
		t.Fatal("copyRepositoryFile() error = nil, want swapped path rejected")
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("managed target stat error = %v, want no outside bytes copied", err)
	}
}

func TestCopyRepositoryFileRejectsSymlinkSwapAtOpen(t *testing.T) {
	sourceRoot := t.TempDir()
	sourcePath := filepath.Join(sourceRoot, "source.txt")
	outsidePath := filepath.Join(t.TempDir(), "outside-secret.txt")
	targetPath := filepath.Join(t.TempDir(), "managed", "source.txt")
	if err := os.WriteFile(sourcePath, []byte("safe source\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(outsidePath, []byte("outside secret\n"), 0o600); err != nil {
		t.Fatalf("write outside sentinel: %v", err)
	}
	source := openManagedCopyTestRoot(t, sourceRoot)
	originalOpen := openManagedCopySourceFileFn
	openManagedCopySourceFileFn = func(root *managedCopySourceRoot, relativePath string) (*os.File, error) {
		openPath := filepath.Join(sourceRoot, filepath.FromSlash(relativePath))
		if err := os.Rename(openPath, openPath+".original"); err != nil {
			return nil, err
		}
		if err := os.Symlink(outsidePath, openPath); err != nil {
			return nil, err
		}
		return originalOpen(root, relativePath)
	}
	t.Cleanup(func() { openManagedCopySourceFileFn = originalOpen })

	if _, err := copyRepositoryFile(
		source, sourceRoot, sourcePath, targetPath, "source.txt", nil,
	); err == nil {
		t.Fatal("copyRepositoryFile() error = nil, want at-open symlink swap rejected")
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("managed target stat error = %v, want no outside bytes copied", err)
	}
}

func TestBindEshuignoreControlRejectsSymlinkSwapBeforeRead(t *testing.T) {
	sourceRoot := t.TempDir()
	controlPath := filepath.Join(sourceRoot, ".eshuignore")
	outsidePath := filepath.Join(t.TempDir(), "outside-policy")
	if err := os.WriteFile(controlPath, []byte("# safe policy\n"), 0o600); err != nil {
		t.Fatalf("write control: %v", err)
	}
	if err := os.WriteFile(outsidePath, []byte("operator-secret.txt\n"), 0o600); err != nil {
		t.Fatalf("write outside policy: %v", err)
	}
	source := openManagedCopyTestRoot(t, sourceRoot)
	installManagedCopySymlinkSwap(t, controlPath, outsidePath)

	cache := make(map[string]*collectorGitignoreSpec)
	if _, err := (*managedCopyCommitExpectation)(nil).bindEshuignoreControl(
		source, sourceRoot, sourceRoot, cache,
	); err == nil {
		t.Fatal("bindEshuignoreControl() error = nil, want swapped control rejected")
	}
	if spec := cache[controlPath]; spec != nil {
		t.Fatal("swapped outside policy entered the ignore cache")
	}
}

func TestOpenUnchangedManagedCopyFileRejectsStaticSymlinkBeforeOpen(t *testing.T) {
	outsidePath := filepath.Join(t.TempDir(), "outside-secret.txt")
	linkPath := filepath.Join(t.TempDir(), "source-link.txt")
	if err := os.WriteFile(outsidePath, []byte("outside secret\n"), 0o600); err != nil {
		t.Fatalf("write outside sentinel: %v", err)
	}
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		t.Fatalf("create source symlink: %v", err)
	}
	originalOpen := openManagedCopySourceFileFn
	openCalled := false
	openManagedCopySourceFileFn = func(source *managedCopySourceRoot, relativePath string) (*os.File, error) {
		openCalled = true
		return originalOpen(source, relativePath)
	}
	t.Cleanup(func() { openManagedCopySourceFileFn = originalOpen })

	source := openManagedCopyTestRoot(t, filepath.Dir(linkPath))
	if _, _, err := openUnchangedManagedCopyFile(
		source, filepath.Dir(linkPath), filepath.Base(linkPath),
	); err == nil {
		t.Fatal("openUnchangedManagedCopyFile() error = nil, want static symlink rejected")
	}
	if openCalled {
		t.Fatal("static symlink reached open, want rejection before open-time effects")
	}
}

func TestManagedCopyDischargesOnlySkippedDirectoryPrefix(t *testing.T) {
	expectation := newSkippedDirectoryExpectation(3, 2)
	expectation.dischargeSkippedPath("generated/dir-0001", true)
	if _, exists := expectation.remaining["generated/dir-0001/file-0000.txt"]; exists {
		t.Fatal("skipped directory descendant remains")
	}
	if _, exists := expectation.remaining["generated/dir-0002/file-0000.txt"]; !exists {
		t.Fatal("sibling directory descendant was incorrectly discharged")
	}
}

func TestManagedCopyPinsOnlyCurrentDirectoryAncestry(t *testing.T) {
	sourceRoot := t.TempDir()
	const siblingCount = 100
	for index := 0; index < siblingCount; index++ {
		relativePath := fmt.Sprintf("sibling-%03d", index)
		if err := os.Mkdir(filepath.Join(sourceRoot, relativePath), 0o750); err != nil {
			t.Fatalf("create sibling directory: %v", err)
		}
	}
	source := openManagedCopyTestRoot(t, sourceRoot)
	maxPinned := 0
	for index := 0; index < siblingCount; index++ {
		relativePath := fmt.Sprintf("sibling-%03d", index)
		if err := source.TrimToParent(relativePath); err != nil {
			t.Fatalf("trim pinned directories: %v", err)
		}
		if err := source.PinDirectory(relativePath); err != nil {
			t.Fatalf("pin sibling directory: %v", err)
		}
		maxPinned = max(maxPinned, source.pinnedDirectoryCount())
	}
	if maxPinned > 2 {
		t.Fatalf("max pinned directory descriptors = %d, want root plus one sibling", maxPinned)
	}
}

func BenchmarkManagedCopyDischargeSkippedDirectories(b *testing.B) {
	const directoryCount = 1000
	const filesPerDirectory = 100
	b.Run("prior-map-scan", func(b *testing.B) {
		for b.Loop() {
			expectation := newSkippedDirectoryExpectation(directoryCount, filesPerDirectory)
			for directory := 0; directory < directoryCount; directory++ {
				dischargeSkippedPathMapScan(
					expectation, fmt.Sprintf("generated/dir-%04d", directory),
				)
			}
			if len(expectation.remaining) != 0 {
				b.Fatalf("remaining paths = %d, want 0", len(expectation.remaining))
			}
		}
		b.ReportMetric(float64(directoryCount), "skipped_dirs")
		b.ReportMetric(float64(directoryCount*filesPerDirectory), "expected_paths")
	})
	b.Run("sorted-prefix", func(b *testing.B) {
		for b.Loop() {
			expectation := newSkippedDirectoryExpectation(directoryCount, filesPerDirectory)
			for directory := 0; directory < directoryCount; directory++ {
				expectation.dischargeSkippedPath(fmt.Sprintf("generated/dir-%04d", directory), true)
			}
			if len(expectation.remaining) != 0 {
				b.Fatalf("remaining paths = %d, want 0", len(expectation.remaining))
			}
		}
		b.ReportMetric(float64(directoryCount), "skipped_dirs")
		b.ReportMetric(float64(directoryCount*filesPerDirectory), "expected_paths")
	})
}

func dischargeSkippedPathMapScan(
	expectation *managedCopyCommitExpectation,
	relativePath string,
) {
	prefix := relativePath + "/"
	for expectedPath := range expectation.remaining {
		if expectedPath == relativePath || strings.HasPrefix(expectedPath, prefix) {
			delete(expectation.remaining, expectedPath)
		}
	}
}

func newSkippedDirectoryExpectation(
	directoryCount int,
	filesPerDirectory int,
) *managedCopyCommitExpectation {
	pathCount := directoryCount * filesPerDirectory
	paths := make([]string, 0, pathCount)
	remaining := make(map[string]struct{}, pathCount)
	for directory := 0; directory < directoryCount; directory++ {
		for file := 0; file < filesPerDirectory; file++ {
			path := fmt.Sprintf("generated/dir-%04d/file-%04d.txt", directory, file)
			paths = append(paths, path)
			remaining[path] = struct{}{}
		}
	}
	return &managedCopyCommitExpectation{paths: paths, remaining: remaining}
}

func installManagedCopySymlinkSwap(t *testing.T, path string, outsidePath string) {
	t.Helper()
	originalOpen := openManagedCopySourceFileFn
	openManagedCopySourceFileFn = func(source *managedCopySourceRoot, relativePath string) (*os.File, error) {
		openPath := path
		file, err := originalOpen(source, relativePath)
		if err != nil {
			return nil, err
		}
		originalPath := openPath + ".original"
		if err := os.Rename(openPath, originalPath); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := os.Symlink(outsidePath, openPath); err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	t.Cleanup(func() { openManagedCopySourceFileFn = originalOpen })
}

func openManagedCopyTestRoot(t *testing.T, sourceRoot string) *managedCopySourceRoot {
	t.Helper()
	source, err := openManagedCopySourceRoot(sourceRoot)
	if err != nil {
		t.Fatalf("open managed-copy test root: %v", err)
	}
	t.Cleanup(func() { _ = source.Close() })
	return source
}
