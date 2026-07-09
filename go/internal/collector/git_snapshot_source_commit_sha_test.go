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

	"github.com/eshu-hq/eshu/go/internal/parser"
)

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
