// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// createRefWorktrees fetches pinned refs (branches or tags) and creates
// isolated git worktrees for each one. Each worktree is a separate checkout
// so snapshotting runs against the pinned ref without disturbing the
// default-branch checkout. Returns empty slice (not error) when no pinned
// refs are configured. Enabler for #5417.
func createRefWorktrees(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	repoID string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
	fleetRefCount int,
	fleetCap int,
) ([]RefWorktreeEntry, int, error) {
	pinnedRefs := config.PinnedRefsByRepoID[repoID]
	if len(pinnedRefs) == 0 {
		return nil, fleetRefCount, nil
	}
	// Fleet-wide cap: if we've already reached the total limit, stop creating
	// new worktrees for this cycle.
	if fleetCap > 0 && fleetRefCount >= fleetCap {
		if logger != nil {
			logger.WarnContext(ctx, "pinned ref fleet cap reached; refs on remaining repos will be skipped this cycle",
				slog.Int("fleet_cap", fleetCap),
				slog.Int("current_count", fleetRefCount),
			)
		}
		return nil, fleetRefCount, nil
	}
	cap := config.PinnedRefPerRepoCap
	if cap <= 0 {
		cap = 3
	}
	if len(pinnedRefs) > cap {
		if logger != nil {
			logger.WarnContext(
				ctx, "pinned ref count exceeds per-repo cap; truncating",
				slog.String("repo_id", repoID),
				slog.Int("pinned_ref_count", len(pinnedRefs)),
				slog.Int("cap", cap),
			)
		}
		pinnedRefs = pinnedRefs[:cap]
	}

	// Resolve the default branch so we can reject/warn when an operator
	// accidentally pins the default branch (which would duplicate snapshots).
	defaultBranch, _ := resolveDefaultBranch(ctx, config, repoPath, token)

	// Reconcile stale worktrees: remove any ref worktrees for refs that are
	// no longer pinned (unpin, repo removal, config change). Also clean up
	// partial creates (dir exists without .git).
	reconcileRefWorktrees(ctx, config, repoID, pinnedRefs, logger)

	entries := make([]RefWorktreeEntry, 0, len(pinnedRefs))
	for _, ref := range pinnedRefs {
		// Fleet cap: stop when we've hit the cycle limit.
		if fleetCap > 0 && fleetRefCount >= fleetCap {
			break
		}
		// Reject pinning the default branch: it's already indexed via the
		// canonical checkout. Pinning it would produce duplicate snapshots
		// every cycle (waste + potential stale-collection noise).
		if defaultBranch != "" && ref == defaultBranch {
			if logger != nil {
				logger.WarnContext(
					ctx, "pinned ref matches the default branch; skipping (already indexed)",
					slog.String("repo_id", repoID),
					slog.String("ref", ref),
				)
			}
			continue
		}

		// Namespace ref worktrees under a reserved .eshu- prefix to prevent
		// path collision: a ref name must not alias another repo's canonical
		// checkout (e.g. an explicit-mode repo ID containing '@').
		// cleanManagedWorkspace already preserves .eshu- entries, so these
		// survive cleanup cycles (P1-3 fix).
		safeRepo := filepath.FromSlash(normalizeRepositoryID(repoID))
		safeRef := filepath.FromSlash(ref)
		refWorktreePath := filepath.Join(config.ReposDir, ".eshu-ref-worktrees", safeRepo, safeRef)
		if err := os.MkdirAll(filepath.Dir(refWorktreePath), 0o750); err != nil { // #nosec G301
			if logger != nil {
				logger.WarnContext(ctx, "failed to create ref worktree parent dir; skipping",
					slog.String("repo_id", repoID),
					slog.String("ref", ref),
					slog.String("error", err.Error()),
				)
			}
			continue
		}

		// Fetch and determine ref type: branch (heads) first, then tag.
		refKind, remoteRef, fetchErr := gitFetchPinnedRef(ctx, config, repoPath, ref, token, logger, event)
		if fetchErr != nil {
			if logger != nil {
				logger.WarnContext(
					ctx, "failed to fetch pinned ref; skipping",
					slog.String("repo_id", repoID),
					slog.String("ref", ref),
					slog.String("error", fetchErr.Error()),
				)
			}
			continue
		}

		if hasGitMarker(refWorktreePath) {
			// Existing worktree: reset to the fetched ref so it picks up
			// upstream advances (P1-1 fix: was frozen at first HEAD).
			if _, resetErr := gitRun(ctx, refWorktreePath, config, token,
				"reset", "--hard", remoteRef,
			); resetErr != nil {
				if logger != nil {
					logger.WarnContext(ctx, "failed to reset existing pinned ref worktree; skipping",
						slog.String("repo_id", repoID),
						slog.String("ref", ref),
						slog.String("error", resetErr.Error()),
					)
				}
				continue
			}
		} else {
			// Recover partial creates: a non-empty dir without .git (e.g.
			// from a previous interrupted sync cycle) will cause
			// git worktree add to fail with "already exists". Remove it
			// before creating the fresh worktree (N4 fix).
			if partialCreateRecoverable(refWorktreePath) {
				if remErr := os.RemoveAll(refWorktreePath); remErr != nil && logger != nil {
					logger.WarnContext(ctx, "failed to remove partial-create dir for pinned ref worktree; skipping",
						slog.String("repo_id", repoID),
						slog.String("ref", ref),
						slog.String("path", refWorktreePath),
						slog.String("error", remErr.Error()),
					)
					continue
				}
			}
			// Create a linked worktree at refWorktreePath pointing at the fetched ref.
			if _, worktreeErr := gitRun(ctx, repoPath, config, token,
				"worktree", "add", "--detach", refWorktreePath, remoteRef,
			); worktreeErr != nil {
				if logger != nil {
					logger.WarnContext(ctx, "failed to create worktree for pinned ref; skipping",
						slog.String("repo_id", repoID),
						slog.String("ref", ref),
						slog.String("error", worktreeErr.Error()),
					)
				}
				continue
			}
		}
		sha, shaErr := gitRevParse(ctx, refWorktreePath, "HEAD", config, token)
		if shaErr != nil {
			sha = ""
		}
		entries = append(entries, RefWorktreeEntry{
			WorktreePath: refWorktreePath,
			Ref:          ref,
			RefKind:      refKind,
			HeadSHA:      sha,
		})
		fleetRefCount++
	}
	return entries, fleetRefCount, nil
}

// gitFetchPinnedRef fetches a named ref (branch or tag) from the remote.
// It tries heads first (branches), then tags. If both resolve ambiguously
// (same name as branch AND tag), it prefers the branch and logs a warning.
// Returns the ref kind, the remote ref path for checkout/reset, and any error.
func gitFetchPinnedRef(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	ref string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) (refKind string, remoteRef string, err error) {
	ref, err = normalizeGitBranchName(ref)
	if err != nil {
		return "", "", err
	}
	if ref == "" {
		return "", "", fmt.Errorf("empty ref name after normalization")
	}

	// Try branch fetch first (the common case).
	branchRefspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", ref, ref)
	branchRemoteRef := "refs/remotes/origin/" + ref
	_, branchErr := gitRunWithStderrWriter(
		ctx, repoPath, config, token,
		newGitProgressWriter(ctx, logger, event, nil),
		"fetch", "--progress", "origin", branchRefspec,
		fmt.Sprintf("--depth=%d", config.CloneDepth),
	)

	// Try tag fetch.
	tagRefspec := fmt.Sprintf("+refs/tags/%s:refs/tags/%s", ref, ref)
	tagRemoteRef := "refs/tags/" + ref
	_, tagErr := gitRunWithStderrWriter(
		ctx, repoPath, config, token,
		newGitProgressWriter(ctx, logger, event, nil),
		"fetch", "--progress", "origin", tagRefspec,
		fmt.Sprintf("--depth=%d", config.CloneDepth),
	)

	switch {
	case branchErr == nil && tagErr == nil:
		// Ambiguous: same name exists as both branch and tag. Prefer branch,
		// log a warning so the operator knows.
		if logger != nil {
			logger.WarnContext(ctx, "pinned ref name is ambiguous (exists as both branch and tag); using branch",
				slog.String("ref", ref),
			)
		}
		return "branch", branchRemoteRef, nil
	case branchErr == nil:
		return "branch", branchRemoteRef, nil
	case tagErr == nil:
		return "tag", tagRemoteRef, nil
	default:
		return "", "", fmt.Errorf("ref %q not found as branch or tag: branch-err=%w, tag-err=%w", ref, branchErr, tagErr)
	}
}

// reconcileRefWorktrees removes stale ref worktrees (refs no longer pinned)
// and cleans up partial creates (dir exists without .git). This prevents
// leaked disk usage from unpins, repo removals, or interrupted sync cycles.
// Slash refs (e.g. "feature/x") create intermediate directories — an entry
// is active if it matches a pinned ref exactly OR is a parent directory of
// a slash-pinned ref (N2 fix: was comparing top-level names against full
// ref paths, deleting active worktrees for slash refs every cycle).
func reconcileRefWorktrees(
	ctx context.Context,
	config RepoSyncConfig,
	repoID string,
	pinnedRefs []string,
	logger *slog.Logger,
) {
	safeRepo := filepath.FromSlash(normalizeRepositoryID(repoID))
	if safeRepo == "" {
		return
	}
	refsDir := filepath.Join(config.ReposDir, ".eshu-ref-worktrees", safeRepo)
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		return // dir doesn't exist yet — nothing to prune
	}

	pinned := make(map[string]struct{}, len(pinnedRefs))
	for _, ref := range pinnedRefs {
		pinned[ref] = struct{}{}
	}

	for _, entry := range entries {
		name := entry.Name()
		if isActiveOrParent(name, pinned) {
			continue
		}
		// Stale: ref no longer pinned, or partial create (dir without .git).
		refPath := filepath.Join(refsDir, name)
		if err := os.RemoveAll(refPath); err != nil && logger != nil {
			logger.WarnContext(ctx, "failed to remove stale ref worktree",
				slog.String("path", refPath),
				slog.String("error", err.Error()),
			)
		}
	}
	// If the repo directory is now empty, clean it up.
	if remaining, readErr := os.ReadDir(refsDir); readErr == nil && len(remaining) == 0 {
		_ = os.Remove(refsDir)
	}
}

// isActiveOrParent reports whether name is a pinned ref exactly, or a parent
// directory of a pinned slash ref (e.g. "feature" is a parent of "feature/x").
func isActiveOrParent(name string, pinned map[string]struct{}) bool {
	if _, ok := pinned[name]; ok {
		return true
	}
	prefix := name + "/"
	for ref := range pinned {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}

// partialCreateRecoverable reports whether a path exists as a non-git directory
// that should be removed before retrying git worktree add. This recovers from
// interrupted sync cycles that leave a markerless dir at the target path (N4).
func partialCreateRecoverable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false // doesn't exist, no recovery needed
	}
	if !info.IsDir() {
		return false // a file at this path is unexpected; don't touch it
	}
	// Check for git marker — if present, the worktree is valid.
	gitDir := filepath.Join(path, ".git")
	gi, gitErr := os.Stat(gitDir)
	if gitErr != nil {
		return true // dir exists but has no .git — partial create
	}
	return !gi.IsDir() && !gi.Mode().IsRegular() && gi.Mode()&os.ModeSymlink == 0
}
