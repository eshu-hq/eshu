// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreateRefWorktreesReusesExistingWorktree proves P1-1: an existing
// worktree is NOT frozen — it's reset to the latest fetched ref on every
// cycle so a pinned branch advancing upstream re-indexes.
func TestCreateRefWorktreesReusesExistingWorktree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Bare repo as "remote".
	bareDir := t.TempDir()
	gitInit(t, bareDir, "--bare", "-b", "main")

	// Local clone of bare repo.
	reposDir := t.TempDir()
	repoName := "test-repo"
	clonePath := filepath.Join(reposDir, repoName)
	gitClone(t, ctx, bareDir, clonePath)

	// Create initial commit on main.
	tmpDir := t.TempDir()
	gitClone(t, ctx, bareDir, tmpDir)
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# initial\n")
	gitAddCommit(t, tmpDir, "initial")
	gitPush(t, tmpDir, bareDir, "main")

	// Create feature branch ahead of main.
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# feature v1\n")
	gitRunIn(t, tmpDir, "checkout", "-b", "feature-x")
	gitAddCommit(t, tmpDir, "feature v1")
	gitPush(t, tmpDir, bareDir, "feature-x")

	// Fetch feature-x ref into local clone.
	cfg := RepoSyncConfig{
		ReposDir:            reposDir,
		SourceMode:          "explicit",
		CloneDepth:          1,
		PinnedRefsByRepoID:  map[string][]string{repoName: {"feature-x"}},
		PinnedRefPerRepoCap: 3,
		GitAuthMethod:       "none",
		GithubOrg:           "test",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// First wave: create the worktree.
	entries, _, err := createRefWorktrees(ctx, cfg, clonePath, repoName, "", logger, gitSyncLogEvent{}, 0, 0)
	if err != nil {
		t.Fatalf("first createRefWorktrees error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("first createRefWorktrees entries = %d, want 1", len(entries))
	}
	firstSHA := entries[0].HeadSHA
	if firstSHA == "" {
		t.Fatal("first HeadSHA is empty")
	}

	// Advance the feature branch on remote.
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# feature v2\n")
	gitRunIn(t, tmpDir, "checkout", "feature-x")
	gitAddCommit(t, tmpDir, "feature v2")
	gitPush(t, tmpDir, bareDir, "feature-x")

	// Second wave: re-fetch should update the existing worktree.
	entries2, _, err := createRefWorktrees(ctx, cfg, clonePath, repoName, "", logger, gitSyncLogEvent{}, 0, 0)
	if err != nil {
		t.Fatalf("second createRefWorktrees error = %v", err)
	}
	if len(entries2) != 1 {
		t.Fatalf("second createRefWorktrees entries = %d, want 1", len(entries2))
	}
	secondSHA := entries2[0].HeadSHA
	if secondSHA == "" {
		t.Fatal("second HeadSHA is empty")
	}
	if firstSHA == secondSHA {
		t.Fatalf("second HeadSHA = first HeadSHA (%s); worktree was frozen — expected reset to advanced commit", firstSHA)
	}
	t.Logf("P1-1: advanced from %s to %s", firstSHA[:8], secondSHA[:8])
}

func gitInit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"init"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_NOGLOBAL=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init %v: %v\n%s", args, err, out)
	}
}

func gitClone(t *testing.T, ctx context.Context, srcURL, dest string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"clone"}, args...)
	cmdArgs = append(cmdArgs, srcURL, dest)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_NOGLOBAL=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone %s -> %s: %v\n%s", srcURL, dest, err, out)
	}
}

func gitAddCommit(t *testing.T, dir string, msg string) {
	t.Helper()
	gitRunIn(t, dir, "add", "-A")
	cmd := exec.Command("git", "-C", dir, "commit", "-m", msg, "--allow-empty")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_NOGLOBAL=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func gitPush(t *testing.T, srcDir, destDir string, branch string) {
	t.Helper()
	cmd := exec.Command("git", "-C", srcDir, "push", destDir, branch)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_NOGLOBAL=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git push %s to %s: %v\n%s", branch, destDir, err, out)
	}
}

func gitRunIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_NOGLOBAL=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
}

func writeGitFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
