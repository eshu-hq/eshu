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
	"time"
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
	gitOutputIn(t, dir, args...)
}

func gitOutputIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_NOGLOBAL=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return string(out)
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

// staticBaselineResolver reports a fixed projected commit SHA so a sync
// cycle sees the default branch as already-projected (baseline == remote
// head) instead of falling back to a full re-observe on an empty baseline.
type staticBaselineResolver struct {
	sha string
}

func (r staticBaselineResolver) LastProjectedCommitSHA(_ context.Context, _ string) (string, error) {
	return r.sha, nil
}

func (r staticBaselineResolver) LastFullProjectionAt(_ context.Context, _ string) (time.Time, bool, error) {
	return time.Now(), true, nil
}

// TestSyncGitRepositoriesRefreshesPinnedRefWhenDefaultUnmoved proves N1:
// syncGitRepositoriesWithLogger refreshes pinned ref worktrees EVERY cycle,
// even when the default branch did not move (the epic #5393 motivating
// scenario — quiet default, actively deployed pinned branch).
func TestSyncGitRepositoriesRefreshesPinnedRefWhenDefaultUnmoved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Bare repo as "remote".
	bareDir := t.TempDir()
	gitInit(t, bareDir, "--bare", "-b", "main")

	reposDir := t.TempDir()
	repoName := "test-repo-n1"
	repoPath := filepath.Join(reposDir, repoName)

	// Create initial commit on main and a feature branch.
	tmpDir := t.TempDir()
	gitClone(t, ctx, bareDir, tmpDir)
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# initial\n")
	gitAddCommit(t, tmpDir, "initial")
	gitPush(t, tmpDir, bareDir, "main")

	// Create feature branch.
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# feature v1\n")
	gitRunIn(t, tmpDir, "checkout", "-b", "feature-x")
	gitAddCommit(t, tmpDir, "feature v1")
	gitPush(t, tmpDir, bareDir, "feature-x")

	// Pre-clone into the managed repos path so syncExistingRepository is called
	// instead of cloneRepository (which would try to resolve a GitHub remote URL).
	gitClone(t, ctx, bareDir, repoPath)

	cfg := RepoSyncConfig{
		ReposDir:            reposDir,
		SourceMode:          "explicit",
		CloneDepth:          1,
		PinnedRefsByRepoID:  map[string][]string{repoName: {"feature-x"}},
		PinnedRefPerRepoCap: 3,
		GitAuthMethod:       "none",
		GithubOrg:           "test",
		RepoLimit:           1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// First sync cycle: fetch + create ref worktree.
	sel1, err := syncGitRepositoriesWithLogger(ctx, cfg, []string{repoName}, logger, gitDeltaBaseline{})
	if err != nil {
		t.Fatalf("first syncGitRepositoriesWithLogger error = %v", err)
	}
	if len(sel1.SelectedRepoPaths) == 0 {
		t.Fatal("first sync: no repos selected")
	}
	entries1 := sel1.RefWorktreesByRepoPath[repoPath]
	if len(entries1) != 1 {
		t.Fatalf("first sync: ref worktrees = %d, want 1", len(entries1))
	}
	firstSHA := entries1[0].HeadSHA

	// Advance the feature branch WITHOUT touching main.
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# feature v2\n")
	gitRunIn(t, tmpDir, "checkout", "feature-x")
	gitAddCommit(t, tmpDir, "feature v2")
	gitPush(t, tmpDir, bareDir, "feature-x")

	// Second sync cycle: main is unmoved, but feature-x advanced.
	// Before N1 fix: updated=false, createRefWorktrees never called → stale SHA.
	// After  N1 fix: createRefWorktrees runs regardless → SHA advances.
	// The baseline resolver reports main's HEAD as already projected, so the
	// default branch is genuinely unmoved this cycle (an empty baseline would
	// legitimately force a full re-observe and mask the N5 assertion).
	mainSHA := strings.TrimSpace(gitOutputIn(t, repoPath, "rev-parse", "HEAD"))
	sel2, err := syncGitRepositoriesWithLogger(ctx, cfg, []string{repoName}, logger, gitDeltaBaseline{
		Resolver: staticBaselineResolver{sha: mainSHA},
	})
	if err != nil {
		t.Fatalf("second syncGitRepositoriesWithLogger error = %v", err)
	}
	entries2 := sel2.RefWorktreesByRepoPath[repoPath]
	if len(entries2) != 1 {
		t.Fatalf("second sync: ref worktrees = %d, want 1 (N1: should refresh even when default unmoved)", len(entries2))
	}
	secondSHA := entries2[0].HeadSHA
	if firstSHA == secondSHA {
		t.Fatalf("N1: second SHA (%s) == first SHA (%s); pinned ref was NOT refreshed when default branch unmoved", firstSHA[:8], secondSHA[:8])
	}
	t.Logf("N1: pinned ref advanced from %s to %s while default branch unmoved", firstSHA[:8], secondSHA[:8])

	// N5: default branch unmoved → main-line entry NOT selected
	// (no wasteful file-tree walk + parse on unchanged default branch).
	sel2Paths := make(map[string]struct{}, len(sel2.SelectedRepoPaths))
	for _, p := range sel2.SelectedRepoPaths {
		sel2Paths[p] = struct{}{}
	}
	if _, ok := sel2Paths[repoPath]; ok {
		t.Fatal("N5: default branch unmoved, but repoPath is in SelectedRepoPaths (main-line should NOT be re-snapshotted)")
	}
	t.Logf("N5: main-line repoPath correctly excluded from selected when default branch unmoved")
}

// TestBuildSelectedRepositoriesRefOnlyEmitsNoMainline proves N5: when
// ref worktrees exist but the main repo path is NOT in selectedPaths
// (default branch unchanged), buildSelectedRepositories emits ONLY the
// ref-scoped SelectedRepository entries — no main-line entry.
func TestBuildSelectedRepositoriesRefOnlyEmitsNoMainline(t *testing.T) {
	t.Parallel()

	reposDir := t.TempDir()
	mainRepoPath := filepath.Join(reposDir, "org", "repo")
	if err := os.MkdirAll(mainRepoPath, 0o750); err != nil {
		t.Fatal(err)
	}

	refWorktreesByRepoPath := map[string][]RefWorktreeEntry{
		mainRepoPath: {
			{WorktreePath: filepath.Join(reposDir, ".eshu-ref-worktrees", "org", "repo", "feature-x"), Ref: "feature-x", HeadSHA: "abc123", RefKind: "branch"},
		},
	}

	// selectedPaths is EMPTY (default branch did not move).
	got := buildSelectedRepositories(
		RepoSyncConfig{ReposDir: reposDir, SourceMode: "githubOrg", GithubOrg: "test", CloneDepth: 1},
		[]string{}, // no main-line paths selected
		nil, nil, nil,
		map[string][]GitRef{
			mainRepoPath: {{Name: "main", Kind: "branch", HeadSHA: "def456"}},
		},
		nil,
		refWorktreesByRepoPath,
	)

	// Must produce exactly one ref-scoped entry, zero main-line entries.
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (ref-only, no main-line)", len(got))
	}
	if got[0].Ref != "feature-x" {
		t.Fatalf("entry Ref = %q, want feature-x", got[0].Ref)
	}
	if got[0].RepoPath != refWorktreesByRepoPath[mainRepoPath][0].WorktreePath {
		t.Fatalf("entry RepoPath = %q, want ref worktree path", got[0].RepoPath)
	}
	if len(got[0].GitRefs) != 1 || got[0].GitRefs[0].Name != "main" {
		t.Fatalf("entry GitRefs = %+v, want the refs map entry carried over (N5a alignment)", got[0].GitRefs)
	}
	t.Logf("N5: %d entries produced (ref-only, zero main-line)", len(got))
}

// TestCreateRefWorktreesSlashRefSurvivesReconcile proves N2: a pinned
// ref containing a slash (e.g. "feature/x") survives reconcile across
// cycles without being deleted and recreated — the intermediate directory
// is recognized as a parent of the pinned ref.
func TestCreateRefWorktreesSlashRefSurvivesReconcile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	bareDir := t.TempDir()
	gitInit(t, bareDir, "--bare", "-b", "main")

	reposDir := t.TempDir()
	repoName := "test-repo-n2"
	repoPath := filepath.Join(reposDir, repoName)

	tmpDir := t.TempDir()
	gitClone(t, ctx, bareDir, tmpDir)
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# initial\n")
	gitAddCommit(t, tmpDir, "initial")
	gitPush(t, tmpDir, bareDir, "main")

	// Create a slash branch.
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# slash v1\n")
	gitRunIn(t, tmpDir, "checkout", "-b", "feature/x")
	gitAddCommit(t, tmpDir, "slash v1")
	gitPush(t, tmpDir, bareDir, "feature/x")

	// Pre-clone into managed path.
	gitClone(t, ctx, bareDir, repoPath)

	cfg := RepoSyncConfig{
		ReposDir:            reposDir,
		SourceMode:          "explicit",
		CloneDepth:          1,
		PinnedRefsByRepoID:  map[string][]string{repoName: {"feature/x"}},
		PinnedRefPerRepoCap: 3,
		GitAuthMethod:       "none",
		GithubOrg:           "test",
		RepoLimit:           1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// First wave: create the slash worktree.
	sel1, err := syncGitRepositoriesWithLogger(ctx, cfg, []string{repoName}, logger, gitDeltaBaseline{})
	if err != nil {
		t.Fatalf("first syncGitRepositoriesWithLogger error = %v", err)
	}
	entries1 := sel1.RefWorktreesByRepoPath[repoPath]
	if len(entries1) != 1 {
		t.Fatalf("first sync: ref worktrees = %d, want 1 (slash ref)", len(entries1))
	}
	firstSHA := entries1[0].HeadSHA

	// Second wave (default unmoved, pinned ref unchanged): reconcile should
	// NOT delete the slash ref's worktree.
	sel2, err := syncGitRepositoriesWithLogger(ctx, cfg, []string{repoName}, logger, gitDeltaBaseline{})
	if err != nil {
		t.Fatalf("second syncGitRepositoriesWithLogger error = %v", err)
	}
	entries2 := sel2.RefWorktreesByRepoPath[repoPath]
	if len(entries2) != 1 {
		t.Fatalf("second sync: ref worktrees = %d, want 1 (N2: slash ref should survive reconcile)", len(entries2))
	}
	// SHA should be the SAME (nothing advanced) — the key assertion is that
	// the worktree EXISTS across cycles, not that it changed.
	if entries2[0].HeadSHA != firstSHA {
		t.Fatalf("slash ref SHA changed unexpectedly: %s → %s (ref didn't advance)", firstSHA[:8], entries2[0].HeadSHA[:8])
	}
	t.Logf("N2: slash ref %s survived reconcile, SHA stable at %s", entries1[0].Ref, firstSHA[:8])
}

// TestSyncGitRepositoriesPrunesRefWorktreesAfterLastPinRemoved proves the P2
// finding: when an operator removes ALL pins for a repo that remains in the
// selection, syncGitRepositoriesWithLogger must still prune any leftover
// .eshu-ref-worktrees/<repo>/<ref> checkouts. Before the fix, hasPinnedRefs
// is false for an unpinned repo, so the `if hasPinnedRefs` block — the only
// caller of reconcileRefWorktrees — never runs, and the reserved-prefix
// worktree leaks forever (cleanManagedWorkspace preserves .eshu- entries).
//
// This drives the real sync loop (syncGitRepositoriesWithLogger) rather than
// calling reconcileRefWorktrees directly: reconcileRefWorktrees already prunes
// correctly in isolation (git_selection_ref_worktrees.go), so the bug is only
// observable by proving the loop's else-branch actually invokes it for a repo
// with zero current pins.
func TestSyncGitRepositoriesPrunesRefWorktreesAfterLastPinRemoved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Bare repo as "remote".
	bareDir := t.TempDir()
	gitInit(t, bareDir, "--bare", "-b", "main")

	reposDir := t.TempDir()
	repoName := "test-repo-unpin"
	repoPath := filepath.Join(reposDir, repoName)

	tmpDir := t.TempDir()
	gitClone(t, ctx, bareDir, tmpDir)
	writeGitFile(t, filepath.Join(tmpDir, "README.md"), "# initial\n")
	gitAddCommit(t, tmpDir, "initial")
	gitPush(t, tmpDir, bareDir, "main")

	// Pre-clone into the managed repos path so syncExistingRepository is used
	// instead of cloneRepository.
	gitClone(t, ctx, bareDir, repoPath)

	// Seed a leftover ref-worktree dir + marker as if a ref was pinned and
	// checked out in a prior cycle, then the pin was removed.
	staleRefDir := filepath.Join(reposDir, ".eshu-ref-worktrees", repoName, "stale-ref")
	if err := os.MkdirAll(staleRefDir, 0o750); err != nil {
		t.Fatalf("MkdirAll(staleRefDir): %v", err)
	}
	writeGitFile(t, filepath.Join(staleRefDir, "marker.txt"), "leftover from a removed pin\n")

	// Repo is in the selection, but has NO pins — the last pin was removed.
	cfg := RepoSyncConfig{
		ReposDir:            reposDir,
		SourceMode:          "explicit",
		CloneDepth:          1,
		PinnedRefsByRepoID:  map[string][]string{},
		PinnedRefPerRepoCap: 3,
		GitAuthMethod:       "none",
		GithubOrg:           "test",
		RepoLimit:           1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	if _, err := syncGitRepositoriesWithLogger(ctx, cfg, []string{repoName}, logger, gitDeltaBaseline{}); err != nil {
		t.Fatalf("syncGitRepositoriesWithLogger error = %v", err)
	}

	if _, statErr := os.Stat(staleRefDir); !os.IsNotExist(statErr) {
		t.Fatalf("stale ref worktree %q still exists after sync with zero pins (stat err = %v); want pruned", staleRefDir, statErr)
	}
	t.Logf("unpin: leftover ref worktree %q pruned after last pin removed", staleRefDir)
}
