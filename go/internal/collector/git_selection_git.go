// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const staleGitLockMinAge = 5 * time.Minute

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
	token, err := resolveGitToken(ctx, config)
	if err != nil && config.SourceMode == "githubOrg" {
		return GitSyncSelection{}, err
	}

	selected := make([]string, 0, len(repositoryIDs))
	deltaByRepoPath := make(map[string]GitSyncDelta)
	refsByRepoPath := make(map[string][]GitRef)
	reconcileByRepoPath := make(map[string]bool)
	sourceSHAByRepoPath := make(map[string]string)
	refWorktreesByRepoPath := make(map[string][]RefWorktreeEntry)
	fleetRefCount := 0
	fleetCap := config.PinnedRefFleetCap
	reconciledThisCycle := 0
	for i, repoID := range repositoryIDs {
		if err := ctx.Err(); err != nil {
			return GitSyncSelection{}, err
		}
		checkoutName, err := repoCheckoutName(repoID)
		if err != nil {
			return GitSyncSelection{}, err
		}
		repoPath := filepath.Join(config.ReposDir, filepath.FromSlash(checkoutName))
		event := gitSyncLogEventFor(repoID, i+1, len(repositoryIDs))
		if !hasGitMarker(repoPath) {
			cloned, cloneErr := cloneRepository(ctx, config, repoID, repoPath, token, logger, event)
			if cloneErr == nil && cloned {
				selected = append(selected, repoPath)
				refs, refsErr := remoteGitRefs(ctx, config, repoPath, token)
				if refsErr != nil {
					logGitSyncFailed(ctx, logger, event.withOperation("list_refs"), refsErr)
					return GitSyncSelection{}, refsErr
				}
				refsByRepoPath[repoPath] = refs
				// Create isolated worktrees for pinned refs (enabler #5417).
				entries, newCount, wtErr := createRefWorktrees(ctx, config, repoPath, repoID, token, logger, event, fleetRefCount, fleetCap)
				if wtErr != nil {
					return GitSyncSelection{}, wtErr
				}
				fleetRefCount = newCount
				if len(entries) > 0 {
					refWorktreesByRepoPath[repoPath] = entries
				}
			}
			continue
		}
		// Reconciliation is bounded per cycle so a fleet of overdue scopes does
		// not stampede into simultaneous full snapshots.
		forceReconcile := reconcileBudgetRemaining(baseline.Reconcile, reconciledThisCycle) &&
			baseline.reconcileDue(ctx, config, repoPath)
		updated, delta, sourceSHA, updateErr := syncExistingRepository(ctx, config, repoPath, token, logger, event, baseline, forceReconcile)
		if updateErr == nil && updated {
			selected = append(selected, repoPath)
			refs, refsErr := remoteGitRefs(ctx, config, repoPath, token)
			if refsErr != nil {
				logGitSyncFailed(ctx, logger, event.withOperation("list_refs"), refsErr)
				return GitSyncSelection{}, refsErr
			}
			refsByRepoPath[repoPath] = refs
			if !delta.IsEmpty() {
				deltaByRepoPath[repoPath] = delta
			}
			sourceSHAByRepoPath[repoPath] = sourceSHA
			if forceReconcile {
				reconcileByRepoPath[repoPath] = true
				reconciledThisCycle++
				baseline.recordReconciliation(ctx)
			}
			// Create isolated worktrees for pinned refs (enabler #5417).
			entries, newCount, wtErr := createRefWorktrees(ctx, config, repoPath, repoID, token, logger, event, fleetRefCount, fleetCap)
			if wtErr != nil {
				return GitSyncSelection{}, wtErr
			}
			fleetRefCount = newCount
			if len(entries) > 0 {
				refWorktreesByRepoPath[repoPath] = entries
			}
		}
	}
	return GitSyncSelection{
		SelectedRepoPaths:         sortUniqueStrings(selected),
		DeltaByRepoPath:           deltaByRepoPath,
		RefsByRepoPath:            refsByRepoPath,
		ReconcileByRepoPath:       reconcileByRepoPath,
		SourceCommitSHAByRepoPath: sourceSHAByRepoPath,
		RefWorktreesByRepoPath:    refWorktreesByRepoPath,
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
	command := exec.CommandContext( // #nosec G204 -- runs git with internally-constructed clone arguments; binary is fixed, args are program-generated
		ctx,
		"git",
		"clone",
		"--progress",
		fmt.Sprintf("--depth=%d", config.CloneDepth),
		"--single-branch",
		remoteURL,
		repoPath,
	)
	command.Env = gitCommandEnv(config, token)
	var stderr bytes.Buffer
	progress := newGitProgressWriter(ctx, logger, event, &stderr)
	command.Stderr = progress
	if err := command.Run(); err != nil {
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
// or unreachable in the local checkout (shallow-clone prune or divergence) the
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
	if err := gitFetchBranch(ctx, config, repoPath, branch, token, logger, event); err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, GitSyncDelta{}, "", err
	}
	remoteRef := "refs/remotes/origin/" + branch
	remoteSHA, err := gitRevParse(ctx, repoPath, remoteRef, config, token)
	if err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, GitSyncDelta{}, "", err
	}

	baseline := strings.TrimSpace(baselineSHA)
	switch {
	case baseline == "":
		// No projected generation yet: there is no trustworthy baseline, so the
		// whole repository must be re-observed.
		notifyDeltaFallback(onFallback, "no_projected_baseline")
	case baseline == remoteSHA:
		// The last projected commit already equals the remote head; nothing new
		// has been observed since the last successful projection.
		logGitSyncCompleted(ctx, logger, event, false)
		return false, GitSyncDelta{}, "", nil
	case !isGitCommitReachable(ctx, config, repoPath, token, baseline):
		// The baseline is known but absent from local history (shallow-clone
		// prune or divergence); a delta diff would be wrong, so re-observe fully.
		notifyDeltaFallback(onFallback, "baseline_unreachable")
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
		logGitSyncCompleted(ctx, logger, event, true)
		return true, delta, remoteSHA, nil
	}

	if err := checkoutRemoteBranch(ctx, config, repoPath, token, branch); err != nil {
		logGitSyncFailed(ctx, logger, event, err)
		return false, GitSyncDelta{}, "", err
	}
	logGitSyncCompleted(ctx, logger, event, true)
	return true, GitSyncDelta{}, remoteSHA, nil
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

	output, err = gitRun(
		ctx,
		repoPath,
		config,
		token,
		"ls-remote",
		"--symref",
		"origin",
		"HEAD",
	)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "ref:" {
			continue
		}
		branch, branchErr := normalizeGitBranchName(strings.TrimPrefix(fields[1], "refs/heads/"))
		if branchErr != nil {
			return "", branchErr
		}
		if branch != "" {
			return branch, nil
		}
	}
	return "", nil
}

func gitFetchBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	branch string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) error {
	branch, err := normalizeGitBranchName(branch)
	if err != nil {
		return err
	}
	_, err = gitRunWithStderrWriter(
		ctx,
		repoPath,
		config,
		token,
		newGitProgressWriter(ctx, logger, event, nil),
		"fetch",
		"--progress",
		"origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch),
		fmt.Sprintf("--depth=%d", config.CloneDepth),
	)
	if err == nil {
		return nil
	}
	if !recoverStaleGitShallowLock(repoPath, err) {
		return err
	}
	_, err = gitRunWithStderrWriter(
		ctx,
		repoPath,
		config,
		token,
		newGitProgressWriter(ctx, logger, event, nil),
		"fetch",
		"--progress",
		"origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch),
		fmt.Sprintf("--depth=%d", config.CloneDepth),
	)
	return err
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

func gitRunWithStderrWriter(
	ctx context.Context,
	repoPath string,
	config RepoSyncConfig,
	token string,
	stderrWriter io.Writer,
	args ...string,
) (string, error) {
	commandArgs := make([]string, 0, len(args)+2)
	commandArgs = append(commandArgs, "-C", repoPath)
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...) // #nosec G204 -- runs git with internally-constructed arguments derived from config and validated repo paths
	command.Env = gitCommandEnv(config, token)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	if stderrWriter != nil {
		command.Stderr = io.MultiWriter(&stderr, stderrWriter)
	} else {
		command.Stderr = &stderr
	}
	if err := command.Run(); err != nil {
		flushProgressWriter(stderrWriter)
		return "", fmt.Errorf(
			"git %s: %w: %s",
			strings.Join(args, " "),
			err,
			sanitizeGitProgressMessage(strings.TrimSpace(stderr.String())),
		)
	}
	flushProgressWriter(stderrWriter)
	return strings.TrimSpace(stdout.String()), nil
}

func flushProgressWriter(writer io.Writer) {
	flusher, ok := writer.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}

func gitCommandEnv(config RepoSyncConfig, token string) []string {
	env := os.Environ()
	authMethod := strings.ToLower(strings.TrimSpace(config.GitAuthMethod))
	switch authMethod {
	case "token", "githubapp":
		if strings.TrimSpace(token) == "" {
			return env
		}
		index := len(env)
		env = append(
			env,
			fmt.Sprintf("GIT_CONFIG_COUNT=%d", 1),
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0="+githubHTTPExtraHeader(token),
		)
		_ = index
	case "ssh":
		command := buildSSHCommand(config)
		if command != "" {
			env = append(env, "GIT_SSH_COMMAND="+command)
		}
	}
	return env
}
