// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const staleGitLockMinAge = 5 * time.Minute

// newGitCommand builds the process-group command for a git invocation. Every
// git spawn in this package must route through it so cancelled sweeps never
// strand remote-https helpers (enforced by
// TestGitSpawnsRouteThroughConstructor).
func newGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	return runtimecfg.NewProcessGroupCommand(ctx, "git", args...) // #nosec G204 -- git binary is fixed; args are internally constructed, never user text
}

func syncGitRepositoriesWithLogger(
	ctx context.Context,
	config RepoSyncConfig,
	repositoryIDs []string,
	logger *slog.Logger,
	baseline gitDeltaBaseline,
) (GitSyncSelection, error) {
	if err := os.MkdirAll(config.ReposDir, 0o750); err != nil { // #nosec G301 -- internal git repos workspace directory
		return GitSyncSelection{}, fmt.Errorf("create repos dir %q: %w", config.ReposDir, err)
	}
	ctx = deadline.WithSweep(deadline.WithMetrics(ctx, baseline.Instruments), config.GitDeadlines, logger)
	token, err := resolveGitToken(ctx, config)
	if err != nil && config.SourceMode == "githubOrg" {
		return GitSyncSelection{}, err
	}

	selected := make([]string, 0, len(repositoryIDs))
	deltaByRepoPath := make(map[string]GitSyncDelta)
	refsByRepoPath := make(map[string][]GitRef)
	reconcileByRepoPath := make(map[string]bool)
	reconcileReasonByRepoPath := make(map[string]string)
	sourceSHAByRepoPath := make(map[string]string)
	preparseByRepoPath := make(map[string]string)
	refWorktreesByRepoPath := make(map[string][]RefWorktreeEntry)
	fleetRefCount := 0
	fleetCap := config.PinnedRefFleetCap
	reconciledThisCycle := 0
	repositoryIDs = prioritizeRepositoryReindex(config, repositoryIDs, baseline.RepositoryReindexRequestedAt)
	walkOrder, walkTracker := trackReconcileWalk(config, repositoryIDs, baseline)
	for i, repoID := range walkOrder {
		if err := ctx.Err(); err != nil {
			return GitSyncSelection{}, err
		}
		checkoutName, err := repoCheckoutName(repoID)
		if err != nil {
			return GitSyncSelection{}, err
		}
		repoPath := filepath.Join(config.ReposDir, filepath.FromSlash(checkoutName))
		event := gitSyncLogEventFor(repoID, i+1, len(repositoryIDs))
		hasPinnedRefs := len(config.PinnedRefsByRepoID[repoID]) > 0

		if !hasGitMarker(repoPath) {
			cloned, cloneErr := cloneRepository(ctx, config, repoID, repoPath, token, logger, event)
			if cloneErr != nil {
				recordGitRepoSyncFailure(ctx, baseline.Instruments, "clone") // already logged by cloneRepository
			} else if cloned {
				refs, ok, fatalErr := resolveRepoRefsIsolated(ctx, config, repoPath, token, logger, event, baseline.Instruments)
				if fatalErr != nil {
					return GitSyncSelection{}, fatalErr
				}
				if ok {
					selected = append(selected, repoPath)
					refsByRepoPath[repoPath] = refs
				}
			}
		} else {
			// Every repository is evaluated so the due-unvisited gauge sees
			// the whole cycle; only forcing is budget-gated.
			reconcile := baseline.reconcileDue(ctx, config, repoPath, logger)
			forceReconcile := reconcile.Due && reconcileBudgetRemaining(baseline.Reconcile, reconciledThisCycle)
			walkTracker.noteEvaluated(reconcile.Due, reconcile.Reason, forceReconcile)
			// updated==false with a nil updateErr is the legitimate no-new-changes
			// no-op, not a failure — only updateErr != nil is metered below.
			holdIfHead := ""
			if forceReconcile {
				holdIfHead = expiredPendingHoldHead(reconcile.Reason, reconcile.State)
			}
			updated, delta, sourceSHA, updateErr := syncExistingRepository(ctx, config, repoPath, token, logger, event, baseline, forceReconcile, holdIfHead)
			if delta.ReconcileHeld {
				baseline.recordReconcileSuppressed(ctx, reconcileReasonInFlightExpiredHeld)
				logReconcileHeld(ctx, logger, reconcile, holdIfHead)
			}
			if updateErr != nil {
				recordGitRepoSyncFailure(ctx, baseline.Instruments, "fetch") // already logged by updateRepository
			} else if updated {
				refs, ok, fatalErr := resolveRepoRefsIsolated(ctx, config, repoPath, token, logger, event, baseline.Instruments)
				if fatalErr != nil {
					return GitSyncSelection{}, fatalErr
				}
				// knownClean reuses the skip attempt's clean-tree observation
				// for the identity stamp below (#8038 item 2).
				skipped, knownClean := false, (*bool)(nil)
				if ok && forceReconcile {
					skipped, knownClean = baseline.skipParse(ctx, logger, config, token, repoPath, reconcile, sourceSHA, refs)
				}
				if skipped {
					reconciledThisCycle++ // ruling a9-143 D2: the re-projection spends a slot
					walkTracker.noteSpent(i)
				} else if ok {
					selected = append(selected, repoPath)
					refsByRepoPath[repoPath] = refs
					if !delta.IsEmpty() || delta.DefaultBranchChanged {
						deltaByRepoPath[repoPath] = delta
					}
					sourceSHAByRepoPath[repoPath] = sourceSHA
					if delta.IsEmpty() {
						if identity := baseline.snapshotIdentity(ctx, config, token, repoPath, sourceSHA, refs, knownClean); identity != "" {
							preparseByRepoPath[repoPath] = identity
						}
					}
					if forceReconcile {
						reconcileByRepoPath[repoPath] = true
						reconcileReasonByRepoPath[repoPath] = reconcile.Reason
						reconciledThisCycle++
						baseline.recordReconciliation(ctx, logger, reconcile)
						walkTracker.noteSpent(i)
					}
				}
			}
		}

		// Ref worktrees run EVERY cycle when pinned refs exist, independent
		// of whether the default branch moved (N1 fix: the epic #5393
		// motivating scenario is a quiet default branch with an actively
		// deployed pinned branch — without this, the pinned branch never
		// refreshes between reconciliation cycles).
		if hasPinnedRefs {
			entries, newCount, wtErr := createRefWorktrees(ctx, config, repoPath, repoID, token, logger, event, fleetRefCount, fleetCap)
			if wtErr != nil {
				return GitSyncSelection{}, wtErr
			}
			fleetRefCount = newCount
			if len(entries) > 0 {
				refWorktreesByRepoPath[repoPath] = entries
				// N5 fix: when only pinned refs advanced (default branch
				// unchanged), do NOT add the main repoPath to selected.
				// The main-line entry would trigger a full file-tree walk +
				// parse on the default branch every cycle, wasting fleet CPU.
				// buildSelectedRepositories processes ref-worktree-only paths
				// separately, emitting only the ref-scoped entries.
			}
		} else {
			// Unpinning must prune leftover ref worktrees so the reserved
			// .eshu-ref-worktrees checkout does not leak: cleanManaged
			// workspace preserves the .eshu- prefix, so once created these
			// entries only ever go away via reconcileRefWorktrees. When a
			// repo has zero current pins (all pins removed, or it never had
			// any), pass an empty pinned set so any stale entries under its
			// ref dir are pruned; reconcileRefWorktrees already returns
			// cleanly when the dir doesn't exist.
			reconcileRefWorktrees(ctx, config, repoID, nil, logger)
		}
	}
	nextStart := walkTracker.finish(ctx, baseline)
	return GitSyncSelection{
		SelectedRepoPaths:          sortUniqueStrings(selected),
		DeltaByRepoPath:            deltaByRepoPath,
		RefsByRepoPath:             refsByRepoPath,
		ReconcileByRepoPath:        reconcileByRepoPath,
		ReconcileReasonByRepoPath:  reconcileReasonByRepoPath,
		SourceCommitSHAByRepoPath:  sourceSHAByRepoPath,
		PreparseIdentityByRepoPath: preparseByRepoPath,
		RefWorktreesByRepoPath:     refWorktreesByRepoPath,
		NextReconcileStartOffset:   nextStart,
	}, nil
}

// updateRepository fetches the remote branch and decides how to project the
// new state. The delta baseline is baselineSHA, the commit of the most recent
// generation that reached a projected state (epic #2340) — NOT the local
// working-copy HEAD. Diffing from local HEAD is unsafe: if a prior projection
// failed after its checkout advanced HEAD, the next sync would skip the
// unprojected changes. When baselineSHA is empty (no projected generation yet)
// or unreachable in the local checkout (shallow-clone prune or divergence), or
// when the remote default branch changed (#7678, see fetchDefaultBranch), the
// sync falls back to a full snapshot — an empty delta — and reports the reason
// through onFallback so operators can watch the delta-skip rate. onFallback may
// be nil.
//
// When updated is true, sourceCommitSHA is the remote HEAD SHA resolved during
// this sync. After checkoutRemoteBranch completes, HEAD equals sourceCommitSHA,
// so the snapshot can carry it rather than shelling out to git rev-parse HEAD
// again. sourceCommitSHA is empty when updated is false (no snapshot).
func updateRepository(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
	baselineSHA string,
	onFallback func(reason string),
	holdIfHead string,
) (updated bool, delta GitSyncDelta, sourceCommitSHA string, _ error) {
	event = event.withOperation("fetch")
	branch, err := resolveDefaultBranch(ctx, config, repoPath, token)
	if err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, GitSyncDelta{}, "", err
	}
	if branch == "" {
		logGitSyncCompleted(ctx, logger, event, false)
		return false, GitSyncDelta{}, "", nil
	}

	event.Branch = branch
	logGitSyncStarted(ctx, logger, event)
	previousBranch := branch
	branch, remoteSHA, detection, err := fetchDefaultBranch(ctx, config, repoPath, token, branch, logger, event)
	if err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, GitSyncDelta{}, "", err
	}
	remoteRef := "refs/remotes/origin/" + branch
	event.Branch = branch
	if holdIfHead != "" && detection == "" && remoteSHA == holdIfHead {
		// Ruling a9-126: the pending full at this head still has live work,
		// so a forced snapshot would only be skipped by the store.
		logGitSyncCompleted(ctx, logger, event, false)
		return false, GitSyncDelta{ReconcileHeld: true}, "", nil
	}

	baseline := strings.TrimSpace(baselineSHA)
	switch {
	case detection != "":
		// The projected commit belongs to the previous default branch's
		// history; re-observe fully rather than diff across branches.
		logDefaultBranchChanged(ctx, logger, event, previousBranch, detection)
		notifyDeltaFallback(onFallback, deltaFallbackDefaultBranchChanged)
	case baseline == "":
		// No projected generation yet: there is no trustworthy baseline, so the
		// whole repository must be re-observed.
		notifyDeltaFallback(onFallback, deltaFallbackNoProjectedBaseline)
	case baseline == remoteSHA:
		// The last projected commit already equals the remote head; nothing new
		// has been observed since the last successful projection.
		logGitSyncCompleted(ctx, logger, event, false)
		return false, GitSyncDelta{}, "", nil
	case !isGitCommitReachable(ctx, config, repoPath, token, baseline):
		// The baseline is known but absent from local history (shallow-clone
		// prune or divergence); a delta diff would be wrong, so re-observe fully.
		notifyDeltaFallback(onFallback, deltaFallbackBaselineUnreachable)
	default:
		delta, err := gitDiffDelta(ctx, config, repoPath, token, baseline, remoteRef)
		if err != nil {
			logGitSyncFailed(ctx, logger, event, err)
			return false, GitSyncDelta{}, "", err
		}
		if err := checkoutRemoteBranch(ctx, config, repoPath, token, branch); err != nil {
			logGitSyncFailed(ctx, logger, event, err)
			return false, GitSyncDelta{}, "", err
		}
		delta.BaselineCommitSHA, event.DeltaBaselineCommitSHA = baseline, baseline
		logGitSyncCompleted(ctx, logger, event, true)
		return true, delta, remoteSHA, nil
	}

	if err := checkoutRemoteBranch(ctx, config, repoPath, token, branch); err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, GitSyncDelta{}, "", err
	}
	logGitSyncCompleted(ctx, logger, event, true)
	return true, GitSyncDelta{DefaultBranchChanged: detection != ""}, remoteSHA, nil
}

func checkoutRemoteBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	branch string,
) error {
	_, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"checkout",
		"-B",
		branch,
		"refs/remotes/origin/"+branch,
	)
	return err
}

func resolveDefaultBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
) (string, error) {
	output, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"symbolic-ref",
		"refs/remotes/origin/HEAD",
	)
	if err == nil {
		branch, branchErr := normalizeGitBranchName(strings.TrimPrefix(strings.TrimSpace(output), "refs/remotes/origin/"))
		if branchErr != nil {
			return "", branchErr
		}
		if branch != "" {
			return branch, nil
		}
	}
	return remoteDefaultBranch(ctx, config, repoPath, token)
}

// gitFetchBranch fetches one remote branch into refs/remotes/origin. A
// non-empty probeRef also stores the remote HEAD commit there in the same
// fetch, at no extra network round trip.
func gitFetchBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	branch string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
	probeRef string,
) error {
	branch, err := normalizeGitBranchName(branch)
	if err != nil {
		return err
	}
	args := append(config.GitDeadlines.LowSpeedArgs(), "fetch", "--progress", "origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch))
	if probeRef != "" {
		args = append(args, "+HEAD:"+probeRef)
	}
	args = append(args, fmt.Sprintf("--depth=%d", config.CloneDepth))
	fetch := func() error {
		_, err := gitRunWithStderrWriter(ctx, repoPath, config, token,
			newGitProgressWriter(ctx, logger, event, nil), deadline.Fetch, args...)
		return err
	}
	if err = fetch(); err == nil || !recoverStaleGitShallowLock(repoPath, err) {
		return err
	}
	return fetch()
}

func normalizeGitBranchName(branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if branch == "" {
		return "", nil
	}
	if branch == "ref:" || strings.HasPrefix(branch, "-") ||
		strings.Contains(branch, ":") ||
		strings.Contains(branch, "..") ||
		strings.Contains(branch, "\\") ||
		strings.ContainsAny(branch, " \t\r\n") {
		return "", fmt.Errorf("invalid git branch name %q", branch)
	}
	return branch, nil
}

func recoverStaleGitShallowLock(repoPath string, fetchErr error) bool {
	if fetchErr == nil {
		return false
	}
	msg := fetchErr.Error()
	if !strings.Contains(msg, "shallow.lock") || !strings.Contains(msg, "File exists") {
		return false
	}
	removed, err := removeGitShallowLock(repoPath, func(modTime time.Time) bool {
		return time.Since(modTime) >= staleGitLockMinAge
	})
	return removed && err == nil
}

// gitLockClockSlack is how far before an attempt's start a lock's mtime may
// fall and still count as that attempt's: filesystems with coarse timestamps
// round a lock created just after the start down.
const gitLockClockSlack = 2 * time.Second

// removeKilledAttemptShallowLock is the owner-dead path of the lock recovery
// (#8091). The retry loop calls it between a failed fetch attempt and its
// retry: the attempt's process group is gone (a deadline kill is SIGKILL, so
// git cannot clean up its own lock), and a shallow.lock created since the
// attempt started is its leftover, so waiting out the stale age would only turn
// the retry into the same "File exists" failure. A lock older than the attempt
// is left alone: it belongs to some other process and keeps the stale-age rule.
func removeKilledAttemptShallowLock(repoPath string, attemptStarted time.Time) error {
	_, err := removeGitShallowLock(repoPath, func(modTime time.Time) bool {
		return !modTime.Before(attemptStarted.Add(-gitLockClockSlack))
	})
	return err
}

// removeGitShallowLock is the one place the shallow lock is removed. It
// removes <repo>/.git/shallow.lock when the lock exists and ownedByDeadProcess
// accepts its modification time. A missing lock is not an error.
func removeGitShallowLock(repoPath string, ownedByDeadProcess func(modTime time.Time) bool) (bool, error) {
	lockPath := filepath.Join(repoPath, ".git", "shallow.lock")
	info, err := os.Stat(lockPath)
	if err != nil {
		return false, nil //nolint:nilerr // no readable lock: nothing to remove
	}
	if !ownedByDeadProcess(info.ModTime()) {
		return false, nil
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove %s: %w", lockPath, err)
	}
	return true, nil
}

func gitRevParse(
	ctx context.Context,
	repoPath string,
	ref string,
	config RepoSyncConfig,
	token string,
) (string, error) {
	output, err := gitRun(ctx, repoPath, config, token, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// skipParse and snapshotIdentity live in selection_skip.go: the file owns the
// forced-reconcile skip attempt, the recorded-drop probe (#8185), and the
// identity stamp.
