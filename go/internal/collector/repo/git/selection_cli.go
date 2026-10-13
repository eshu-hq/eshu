// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/preparse"
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
	ctx = deadline.WithMetrics(ctx, baseline.Instruments)
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
				if ok && forceReconcile && baseline.skipParse(ctx, logger, config, token, repoPath, reconcile, sourceSHA, refs) {
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
						if identity := baseline.snapshotIdentity(ctx, config, token, repoPath, sourceSHA, refs); identity != "" {
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

func cloneRepository(
	ctx context.Context,
	config RepoSyncConfig,
	repoID string,
	repoPath string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) (bool, error) {
	event = event.withOperation("clone")
	logGitSyncStarted(ctx, logger, event)
	remoteURL := repoRemoteURL(config, repoID)
	if remoteURL == "" {
		err := fmt.Errorf("build remote URL for %q", repoID)
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o750); err != nil { // #nosec G301 -- internal git checkout parent directory
		logGitSyncFailed(ctx, logger, event, err)
		return false, err
	}
	var stderr bytes.Buffer
	progress := newGitProgressWriter(ctx, logger, event, &stderr)
	if err := deadline.Run(ctx, config.GitDeadlines, "clone", func(deadlineCtx context.Context) *exec.Cmd {
		command := newGitCommand(
			deadlineCtx,
			"clone",
			"--progress",
			fmt.Sprintf("--depth=%d", config.CloneDepth),
			"--single-branch",
			remoteURL,
			repoPath,
		)
		command.Env = gitCommandEnv(config, token, repoPath)
		command.Stderr = progress
		return command
	}); err != nil {
		progress.Flush()
		_ = os.RemoveAll(repoPath)
		wrapped := fmt.Errorf("clone %q: %w: %s", repoID, err, sanitizeGitProgressMessage(strings.TrimSpace(stderr.String())))
		logGitSyncFailed(ctx, logger, event, wrapped)
		return false, wrapped
	}
	progress.Flush()
	logGitSyncCompleted(ctx, logger, event, true)
	return true, nil
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
	args := []string{"fetch", "--progress", "origin", fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)}
	if probeRef != "" {
		args = append(args, "+HEAD:"+probeRef)
	}
	args = append(args, fmt.Sprintf("--depth=%d", config.CloneDepth))
	fetch := func() error {
		_, err := gitRunWithStderrWriter(ctx, repoPath, config, token,
			newGitProgressWriter(ctx, logger, event, nil), args...)
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
	lockPath := filepath.Join(repoPath, ".git", "shallow.lock")
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) < staleGitLockMinAge {
		return false
	}
	return os.Remove(lockPath) == nil
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

func gitRun(
	ctx context.Context,
	repoPath string,
	config RepoSyncConfig,
	token string,
	args ...string,
) (string, error) {
	return gitRunWithStderrWriter(ctx, repoPath, config, token, nil, args...)
}

// skipParse runs the pre-parse decision after the fetch (#7978 follow-up,
// ruling a9-143). It reports true when the store re-projected the active
// generation, so the caller selects nothing for repoPath. A resolver without
// preparse.Reprojector, a refusal, or a store error reports false and the
// repository is parsed as before.
func (b gitDeltaBaseline) skipParse(
	ctx context.Context, logger *slog.Logger, config RepoSyncConfig, token, repoPath string,
	decision reconcileDecision, sourceSHA string, refs []GitRef,
) bool {
	reprojector, ok := b.Resolver.(preparse.Reprojector)
	if b.Preparse == nil || !ok {
		return false
	}
	return preparse.Run(ctx, preparse.Attempt{
		ScopeID: decision.ScopeID, Reason: decision.Reason, State: decision.State, RemoteCommit: sourceSHA,
		Identity:      preparse.Identity(GenerationContentVersion(), sourceSHA, repositoryDefaultBranch(refs), *b.Preparse),
		HasSubmodules: regularFileExists(filepath.Join(repoPath, ".gitmodules")),
		MaxSkips:      b.ParseMaxSkips, ConfigDigest: preparse.ConfigDigest(*b.Preparse),
	}, func() (bool, error) { return gitTreeClean(ctx, config, token, repoPath) }, reprojector,
		preparse.Recorder{Instruments: b.Instruments, Logger: logger})
}

// snapshotIdentity returns the pre-parse identity a full snapshot of repoPath
// at sourceSHA will carry, or an empty string when the feature is off or the
// checkout is not clean (the identity describes the commit's tree only).
func (b gitDeltaBaseline) snapshotIdentity(ctx context.Context, config RepoSyncConfig, token, repoPath, sourceSHA string, refs []GitRef) string {
	if b.Preparse == nil {
		return ""
	}
	if clean, err := gitTreeClean(ctx, config, token, repoPath); err != nil || !clean {
		return ""
	}
	return preparse.Identity(GenerationContentVersion(), sourceSHA, repositoryDefaultBranch(refs), *b.Preparse)
}
