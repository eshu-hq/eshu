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
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const workflowImageCommitFixture = `name: Build image
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: docker build -t ghcr.io/acme/fixture:prod .
`

// TestCheckoutRemoteBranchEquivalence proves the theory that after
// checkoutRemoteBranch runs (git checkout -B <branch> refs/remotes/origin/<branch>),
// HEAD equals the checked-out ref's SHA, so the sync-resolved remoteSHA can be
// carried to skip a redundant git rev-parse HEAD subprocess in the snapshot.
func TestCheckoutRemoteBranchEquivalence(t *testing.T) {
	repoPath := t.TempDir()

	// Initialize a real git repository.
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")

	// Create a commit.
	writeFile(t, repoPath, "README.md", "# Test repo")
	runGit(t, repoPath, "add", "README.md")
	runGit(t, repoPath, "commit", "-m", "initial commit")

	// Get the commit SHA.
	commitSHA := strings.TrimSpace(runGit(t, repoPath, "rev-parse", "HEAD"))
	if commitSHA == "" {
		t.Fatal("rev-parse HEAD returned empty SHA")
	}

	// Simulate checkoutRemoteBranch: git checkout -B main <commit>.
	// This is what checkoutRemoteBranch does with refs/remotes/origin/<branch>.
	runGit(t, repoPath, "checkout", "-B", "main", commitSHA)

	// After checkout, git rev-parse HEAD must equal the commit we checked out.
	headAfterCheckout := strings.TrimSpace(runGit(t, repoPath, "rev-parse", "HEAD"))
	if headAfterCheckout != commitSHA {
		t.Fatalf("after checkout -B main %s, HEAD = %s, want %s", commitSHA, headAfterCheckout, commitSHA)
	}

	// gitCommitSHA must also return the same SHA.
	got := gitCommitSHA(context.Background(), repoPath)
	if got != commitSHA {
		t.Fatalf("gitCommitSHA() = %q, want %q (carried SHA equivalence)", got, commitSHA)
	}
}

// TestSnapshotUsesSourceCommitSHA verifies that when SelectedRepository.SourceCommitSHA
// is populated, SnapshotRepository uses it instead of shelling out to git rev-parse HEAD.
func TestSnapshotUsesSourceCommitSHA(t *testing.T) {
	repoPath := t.TempDir()

	// Initialize a real git repository with a commit.
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")
	writeFile(t, repoPath, "main.py", "def hello():\n    pass\n")
	runGit(t, repoPath, "add", "main.py")
	runGit(t, repoPath, "commit", "-m", "initial commit")

	realHEAD := strings.TrimSpace(runGit(t, repoPath, "rev-parse", "HEAD"))

	// Use a deliberately different SHA to prove the snapshot uses SourceCommitSHA,
	// not the real HEAD.
	carriedSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	snapshotter := NativeRepositorySnapshotter{
		Engine: engine,
		Now: func() time.Time {
			return time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC)
		},
	}

	snapshot, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:        repoPath,
			RemoteURL:       "https://github.com/example/service",
			SourceCommitSHA: carriedSHA,
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	// SourceCommitSHA is non-empty, so the snapshot MUST use it.
	if snapshot.HeadCommitSHA != carriedSHA {
		t.Fatalf("HeadCommitSHA = %q, want %q (SourceCommitSHA was set, must be used)", snapshot.HeadCommitSHA, carriedSHA)
	}

	// Confirm the real HEAD is different, proving we didn't fall back to gitCommitSHA.
	if realHEAD == carriedSHA {
		t.Fatalf("test setup error: real HEAD %q equals fake carried SHA %q", realHEAD, carriedSHA)
	}
}

// TestSnapshotFallsBackToGitCommitSHA verifies that when SelectedRepository.SourceCommitSHA
// is empty, SnapshotRepository falls back to the existing gitCommitSHA behavior.
func TestSnapshotFallsBackToGitCommitSHA(t *testing.T) {
	repoPath := t.TempDir()

	// Initialize a real git repository with a commit.
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")
	writeFile(t, repoPath, "main.py", "def hello():\n    pass\n")
	runGit(t, repoPath, "add", "main.py")
	runGit(t, repoPath, "commit", "-m", "initial commit")

	realHEAD := strings.TrimSpace(runGit(t, repoPath, "rev-parse", "HEAD"))

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	snapshotter := NativeRepositorySnapshotter{
		Engine: engine,
		Now: func() time.Time {
			return time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC)
		},
	}

	// SourceCommitSHA is empty (non-sync mode), so the snapshot must fall back
	// to gitCommitSHA and use the real HEAD.
	snapshot, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:  repoPath,
			RemoteURL: "https://github.com/example/service",
			// SourceCommitSHA intentionally left empty.
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	if snapshot.HeadCommitSHA != realHEAD {
		t.Fatalf("HeadCommitSHA = %q, want %q (SourceCommitSHA empty, must fall back to gitCommitSHA)", snapshot.HeadCommitSHA, realHEAD)
	}
}

func TestSnapshotFallsBackToFilesystemSourceGitTreeCommitSHA(t *testing.T) {
	t.Parallel()

	sourcePath := t.TempDir()
	runGit(t, sourcePath, "init")
	runGit(t, sourcePath, "config", "user.email", "test@example.com")
	runGit(t, sourcePath, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".github", "workflows"), 0o750); err != nil {
		t.Fatalf("create workflow directory: %v", err)
	}
	writeFile(t, sourcePath, filepath.Join(".github", "workflows", "build.yml"), workflowImageCommitFixture)
	writeFile(t, sourcePath, "main.py", "def hello():\n    pass\n")
	runGit(t, sourcePath, "add", ".")
	runGit(t, sourcePath, "commit", "-m", "initial commit")
	wantCommit := runGit(t, sourcePath, "rev-parse", "HEAD")

	managedPath := filepath.Join(t.TempDir(), "managed")
	if err := copyRepositoryTree(context.Background(), sourcePath, managedPath); err != nil {
		t.Fatalf("copyRepositoryTree() error = %v", err)
	}
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	snapshot, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:    managedPath,
			GitTreePath: sourcePath,
			Delta:       true,
			FileTargets: []string{filepath.Join(managedPath, "main.py")},
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	if snapshot.HeadCommitSHA != wantCommit {
		t.Fatalf("HeadCommitSHA = %q, want source GitTreePath HEAD %q", snapshot.HeadCommitSHA, wantCommit)
	}
	if len(snapshot.WorkflowImageFileMetas) != 1 {
		t.Fatalf("len(WorkflowImageFileMetas) = %d, want 1", len(snapshot.WorkflowImageFileMetas))
	}
	if got := snapshot.WorkflowImageFileMetas[0].CommitSHA; got != wantCommit {
		t.Fatalf("workflow CommitSHA = %q, want %q", got, wantCommit)
	}
	assertWorkflowImageFactCommitSHA(t, managedPath, snapshot, wantCommit)
}

func TestSnapshotManagedCopyOmitsCommitSHAForDivergentSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		diverge func(*testing.T, string)
	}{
		{
			name: "dirty tracked workflow",
			diverge: func(t *testing.T, sourcePath string) {
				t.Helper()
				writeFile(t, sourcePath, filepath.Join(".github", "workflows", "build.yml"), strings.ReplaceAll(workflowImageCommitFixture, ":prod", ":dirty"))
			},
		},
		{
			name: "eligible untracked workflow",
			diverge: func(t *testing.T, sourcePath string) {
				t.Helper()
				writeFile(t, sourcePath, filepath.Join(".github", "workflows", "untracked.yml"), strings.ReplaceAll(workflowImageCommitFixture, ":prod", ":untracked"))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sourcePath := t.TempDir()
			runGit(t, sourcePath, "init")
			runGit(t, sourcePath, "config", "user.email", "test@example.com")
			runGit(t, sourcePath, "config", "user.name", "Test")
			if err := os.MkdirAll(filepath.Join(sourcePath, ".github", "workflows"), 0o750); err != nil {
				t.Fatalf("create workflow directory: %v", err)
			}
			writeFile(t, sourcePath, filepath.Join(".github", "workflows", "build.yml"), workflowImageCommitFixture)
			writeFile(t, sourcePath, "main.py", "def main():\n    pass\n")
			runGit(t, sourcePath, "add", ".")
			runGit(t, sourcePath, "commit", "-m", "initial commit")
			test.diverge(t, sourcePath)

			managedPath := filepath.Join(t.TempDir(), "managed")
			if err := copyRepositoryTree(context.Background(), sourcePath, managedPath); err != nil {
				t.Fatalf("copyRepositoryTree() error = %v", err)
			}
			engine, err := parser.DefaultEngine()
			if err != nil {
				t.Fatalf("DefaultEngine() error = %v", err)
			}
			snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
				context.Background(),
				SelectedRepository{
					RepoPath:    managedPath,
					GitTreePath: sourcePath,
					Delta:       true,
					FileTargets: []string{filepath.Join(managedPath, "main.py")},
				},
			)
			if err != nil {
				t.Fatalf("SnapshotRepository() error = %v", err)
			}
			if snapshot.HeadCommitSHA != "" {
				t.Fatalf("HeadCommitSHA = %q, want empty for divergent managed copy", snapshot.HeadCommitSHA)
			}
			if len(snapshot.WorkflowImageFileMetas) == 0 {
				t.Fatal("WorkflowImageFileMetas is empty, want divergent workflow to be discovered")
			}
			for _, meta := range snapshot.WorkflowImageFileMetas {
				if meta.CommitSHA != "" {
					t.Errorf("workflow meta %q CommitSHA = %q, want empty", meta.RelativePath, meta.CommitSHA)
				}
			}
			assertWorkflowImageFactCommitSHA(t, managedPath, snapshot, "")
		})
	}
}

func assertWorkflowImageFactCommitSHA(
	t *testing.T,
	repoPath string,
	snapshot RepositorySnapshot,
	wantCommit string,
) {
	t.Helper()
	collected := buildStreamingGeneration(
		repoPath,
		repositoryidentity.Metadata{ID: "repository:managed-copy", Name: "managed-copy"},
		"managed-copy-source-run",
		time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
		snapshot,
		false,
		"",
	)
	if gotCommit := collected.Generation.SourceCommitSHA; gotCommit != wantCommit {
		t.Errorf("generation SourceCommitSHA = %q, want %q", gotCommit, wantCommit)
	}
	count := 0
	for _, envelope := range drainCollectorFacts(t, collected) {
		if envelope.FactKind != facts.CICDWorkflowImageEvidenceFactKind {
			continue
		}
		count++
		gotCommit, _ := envelope.Payload["commit_sha"].(string)
		if gotCommit != wantCommit {
			t.Errorf("workflow image fact %q commit_sha = %q, want %q", envelope.FactID, gotCommit, wantCommit)
		}
	}
	if count == 0 {
		t.Fatal("no ci.workflow_image_evidence fact emitted")
	}
}

// TestSnapshotHeadCommitSubprocessCount is the measured before/after for #4880:
// it counts git rev-parse HEAD subprocess invocations in the snapshot path via
// the gitCommitSHAFn seam. When the sync-resolved SourceCommitSHA is carried,
// the snapshot runs 0 such subprocesses; when empty (fallback), exactly 1.
// It must not run in parallel: it swaps the package-level gitCommitSHAFn seam.
func TestSnapshotHeadCommitSubprocessCount(t *testing.T) {
	repoPath := t.TempDir()
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")
	writeFile(t, repoPath, "main.py", "def hello():\n    pass\n")
	runGit(t, repoPath, "add", "main.py")
	runGit(t, repoPath, "commit", "-m", "initial commit")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	snapshotter := NativeRepositorySnapshotter{
		Engine: engine,
		Now: func() time.Time {
			return time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC)
		},
	}

	original := gitCommitSHAFn
	var calls int
	gitCommitSHAFn = func(ctx context.Context, p string) string {
		calls++
		return original(ctx, p)
	}
	defer func() { gitCommitSHAFn = original }()

	// Carried sync-resolved SHA: the snapshot must run zero git rev-parse HEAD.
	calls = 0
	if _, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath:        repoPath,
		RemoteURL:       "https://github.com/example/service",
		SourceCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}); err != nil {
		t.Fatalf("SnapshotRepository(carried) error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("carried SourceCommitSHA: git rev-parse HEAD invocations = %d, want 0", calls)
	}

	// Empty SHA (non-sync fallback): exactly one git rev-parse HEAD.
	calls = 0
	if _, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath:  repoPath,
		RemoteURL: "https://github.com/example/service",
	}); err != nil {
		t.Fatalf("SnapshotRepository(fallback) error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("empty SourceCommitSHA: git rev-parse HEAD invocations = %d, want 1", calls)
	}
}

func runGit(t *testing.T, repoPath string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", cmdArgs...) // #nosec G204 -- test helper with controlled args
	output, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("git %s: %v\nstderr: %s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
