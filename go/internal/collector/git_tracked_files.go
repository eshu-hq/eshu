// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitTrackedFiles resolves the set of repo-relative, slash-separated paths
// git tracks in the repository rooted at gitDir (issue #5591): git applies
// .gitignore only to UNTRACKED paths, so a file `git add -f`d past a
// .gitignore rule stays tracked (`git ls-files` still lists it) even though
// eshu's discovery previously applied .gitignore as a pure pattern filter
// with no knowledge of git's tracked set, silently dropping a force-committed
// file such as a checked-in terraform.tfstate that matches the repo's own
// `*.tfstate` rule.
//
// It runs `git -C gitDir ls-files -z` — a purely local, read-only index
// query on an already-checked-out repository — so it shells out directly the
// same way gitSubmoduleGitlinkSHA does (see git_submodule_pinned_sha.go),
// rather than through gitRun/gitCommandEnv, which exist to thread
// RepoSyncConfig/token auth material into commands that talk to a remote
// (fetch/clone). The NUL-separated (-z) form is parsed instead of the default
// newline-separated form so a tracked path containing a literal newline
// (rare, but valid in git) is never mis-split.
//
// It returns (nil, false) — never guesses — when the invocation fails for
// any reason: gitDir is not a git repository, git is not installed, or the
// repository has no commits yet (an unborn HEAD still has an empty-but-valid
// index, so this case is unlikely in practice but is not special-cased).
// Callers MUST treat ok=false as "tracked status unknown" and fall back to
// their pre-#5591 behavior — gitignore filtering with no tracked-file
// exception — never as "nothing is tracked."
func gitTrackedFiles(ctx context.Context, gitDir string) (map[string]struct{}, bool) {
	command := exec.CommandContext(ctx, "git", "-C", gitDir, "ls-files", "-z") // #nosec G204 -- runs git with fixed internally-constructed arguments over an already-resolved local repo path
	output, err := command.Output()
	if err != nil {
		return nil, false
	}

	tracked := make(map[string]struct{})
	for _, rel := range strings.Split(string(output), "\x00") {
		if rel == "" {
			continue
		}
		tracked[filepath.ToSlash(rel)] = struct{}{}
	}
	return tracked, true
}

// hasGitDirMarker reports whether dir has its own ".git" entry — a
// directory, a regular file (a linked worktree or submodule gitlink
// pointer), or a symlink — mirroring discovery's own repository-root
// definition. The collector package cannot import that unexported check, and
// buildGitTrackedResolver needs an equivalent test to decide whether dir IS a
// git checkout (safe to run ls-files there directly) or a plain-filesystem
// managed copy that carries no .git of its own (issue #5649; see
// buildGitTrackedResolver).
func hasGitDirMarker(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0
}

// buildGitTrackedResolver builds a discovery.Options.GitTrackedResolver
// closure for one repository snapshot (issue #5591). scanRoot is the root
// discovery.ResolveRepositoryFileSetsWithStats walks — SnapshotRepository's
// repoPath; gitTreePath is the git checkout backing that scan root: equal to
// scanRoot in ordinary git-sync mode, but the SOURCE checkout in filesystem
// managed-copy mode, where the copy under scanRoot has no .git of its own
// (issue #5649) — attachFilesystemGitTreePaths is what populates
// SelectedRepository.GitTreePath with that source path.
//
// discovery groups discovered files by the nearest repository root under
// scanRoot, so the returned resolver may be asked about scanRoot itself, or
// about a nested repository root found further down the tree (e.g. a
// submodule checked out with its own .git). Resolution order per call:
//
//  1. repoRoot has its own ".git" marker: it IS a git checkout — a nested or
//     submodule repo in either mode, or scanRoot itself in ordinary git-sync
//     mode (where gitTreePath == scanRoot, so this also covers the common
//     case). Run ls-files there directly.
//  2. repoRoot == scanRoot and gitTreePath is set: the managed-copy root
//     itself has no .git — run ls-files at gitTreePath instead.
//  3. repoRoot is a descendant of scanRoot and gitTreePath is set: mirror the
//     same scanRoot-relative path under gitTreePath (copyRepositoryTree
//     mirrors the source tree 1:1) and run ls-files there.
//  4. Anything else — gitTreePath empty/blank, or repoRoot outside scanRoot —
//     reports ok=false: tracked status unknown, caller keeps its pre-#5591
//     behavior.
func buildGitTrackedResolver(
	ctx context.Context,
	scanRoot string,
	gitTreePath string,
) func(repoRoot string) (map[string]struct{}, bool) {
	return func(repoRoot string) (map[string]struct{}, bool) {
		if hasGitDirMarker(repoRoot) {
			return gitTrackedFiles(ctx, repoRoot)
		}
		gitTreePath = strings.TrimSpace(gitTreePath)
		if gitTreePath == "" {
			return nil, false
		}
		if repoRoot == scanRoot {
			return gitTrackedFiles(ctx, gitTreePath)
		}

		rel, err := filepath.Rel(scanRoot, repoRoot)
		if err != nil {
			return nil, false
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." {
			return gitTrackedFiles(ctx, gitTreePath)
		}
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return nil, false
		}
		return gitTrackedFiles(ctx, filepath.Join(gitTreePath, filepath.FromSlash(rel)))
	}
}
