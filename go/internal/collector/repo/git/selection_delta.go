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
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/deadline"
)

// How a sync detected that the remote default branch changed (#7678); the
// closed value set of the "detection" attribute on the change log.
const (
	// defaultBranchDetectionMoved: the remote HEAD fetched alongside the
	// tracked branch resolved to a different commit.
	defaultBranchDetectionMoved = "moved"
	// defaultBranchDetectionMissingRef: the tracked branch no longer exists on
	// the remote.
	defaultBranchDetectionMissingRef = "missing_ref"
)

// remoteHeadProbeRef holds the remote HEAD commit from the last default-branch
// fetch. It sits outside refs/heads and refs/tags so ref discovery never
// reports it.
const remoteHeadProbeRef = "refs/eshu/remote-head"

// fetchDefaultBranch fetches the tracked default branch and follows the remote
// when its default branch changed (#7678). clone records
// refs/remotes/origin/HEAD once and a single-branch fetch never refreshes it,
// so a stale branch either fails every fetch (deleted on the remote) or keeps
// being indexed (still present). The same fetch stores the remote HEAD; when
// it differs from the tracked tip, or the tracked branch is gone, the remote
// default is re-resolved and adopted. detection is empty when the default
// branch did not change.
func fetchDefaultBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	branch string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) (current string, remoteSHA string, detection string, _ error) {
	probeRef := remoteHeadProbeRef
	err := gitFetchBranch(ctx, config, repoPath, branch, token, logger, event, probeRef)
	if gitMissingRemoteRef(err, "HEAD") {
		// The remote HEAD names a missing branch; sync the tracked branch unprobed.
		probeRef = ""
		err = gitFetchBranch(ctx, config, repoPath, branch, token, logger, event, probeRef)
	}
	if err != nil {
		if !gitMissingRemoteRef(err, "refs/heads/"+branch) {
			return "", "", "", err
		}
		next, resolveErr := remoteDefaultBranch(ctx, config, repoPath, token)
		if resolveErr != nil {
			return "", "", "", fmt.Errorf("%w; resolve remote default branch: %w", err, resolveErr)
		}
		if next == "" || next == branch {
			return "", "", "", err
		}
		return adoptDefaultBranch(ctx, config, repoPath, token, next, defaultBranchDetectionMissingRef, logger, event)
	}

	refs := []string{"rev-parse", "refs/remotes/origin/" + branch}
	if probeRef != "" {
		refs = append(refs, probeRef)
	}
	output, err := gitRun(ctx, repoPath, config, token, refs...)
	if err != nil {
		return "", "", "", err
	}
	shas := strings.Fields(output)
	if len(shas) < 2 || shas[0] == shas[1] {
		if len(shas) == 0 {
			return branch, "", "", nil
		}
		return branch, shas[0], "", nil
	}
	next, err := remoteDefaultBranch(ctx, config, repoPath, token)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve remote default branch: %w", err)
	}
	if next == "" || next == branch {
		return branch, shas[0], "", nil
	}
	return adoptDefaultBranch(ctx, config, repoPath, token, next, defaultBranchDetectionMoved, logger, event)
}

// adoptDefaultBranch fetches next and only then repoints
// refs/remotes/origin/HEAD at it, so a failed fetch leaves the old pointer and
// the next sync detects the change again.
func adoptDefaultBranch(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	next string,
	detection string,
	logger *slog.Logger,
	event gitSyncLogEvent,
) (string, string, string, error) {
	event.Branch = next
	if err := gitFetchBranch(ctx, config, repoPath, next, token, logger, event, ""); err != nil {
		return "", "", "", err
	}
	nextRef := "refs/remotes/origin/" + next
	if _, err := gitRun(ctx, repoPath, config, token, "symbolic-ref", "refs/remotes/origin/HEAD", nextRef); err != nil {
		return "", "", "", err
	}
	sha, err := gitRevParse(ctx, repoPath, nextRef, config, token)
	if err != nil {
		return "", "", "", err
	}
	return next, sha, detection, nil
}

// remoteDefaultBranch asks the remote which branch its HEAD names. It returns
// an empty name when HEAD names no branch.
func remoteDefaultBranch(ctx context.Context, config RepoSyncConfig, repoPath string, token string) (string, error) {
	output, err := gitRunRemote(ctx, deadline.Resolve, repoPath, config, token, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "ref:" || !strings.HasPrefix(fields[1], "refs/heads/") {
			continue
		}
		branch, err := normalizeGitBranchName(strings.TrimPrefix(fields[1], "refs/heads/"))
		if err != nil {
			return "", err
		}
		if branch != "" {
			return branch, nil
		}
	}
	return "", nil
}

// gitMissingRemoteRef reports whether a fetch failed because the remote does
// not have ref. It matches git's "couldn't find remote ref" message, which
// gitCommandEnv keeps untranslated with LC_ALL=C.
func gitMissingRemoteRef(err error, ref string) bool {
	if err == nil {
		return false
	}
	fields := strings.Fields(err.Error())
	for i := 0; i+4 < len(fields); i++ {
		if fields[i] == "couldn't" && fields[i+1] == "find" && fields[i+2] == "remote" &&
			fields[i+3] == "ref" && fields[i+4] == ref {
			return true
		}
	}
	return false
}

// logDefaultBranchChanged records that a sync followed a changed remote
// default branch; event.Branch is the branch now tracked.
func logDefaultBranchChanged(ctx context.Context, logger *slog.Logger, event gitSyncLogEvent, previous, detection string) {
	if logger == nil {
		return
	}
	attrs := append(event.eventAttrs(time.Now()),
		slog.String("previous_branch", previous),
		slog.String("detection", detection),
	)
	logger.WarnContext(ctx, "git repository default branch changed", attrs...)
}

// IsEmpty reports whether the Git update carried no file-level delta metadata.
func (d GitSyncDelta) IsEmpty() bool {
	return len(d.ChangedFileTargets) == 0 && len(d.DeletedRelativePaths) == 0
}

func gitDiffDelta(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	oldRef string,
	newRef string,
) (GitSyncDelta, error) {
	output, err := gitRun(
		ctx,
		repoPath,
		config,
		token,
		"diff",
		"--name-status",
		"-z",
		"--find-renames",
		oldRef,
		newRef,
	)
	if err != nil {
		return GitSyncDelta{}, err
	}
	return parseGitDiffNameStatusDelta(repoPath, output), nil
}

func parseGitDiffNameStatusDelta(repoPath string, output string) GitSyncDelta {
	fields := strings.Split(output, "\x00")
	changed := make([]string, 0)
	deleted := make([]string, 0)
	for i := 0; i < len(fields); {
		status := strings.TrimSpace(fields[i])
		i++
		if status == "" {
			continue
		}
		if i >= len(fields) {
			break
		}
		oldPath := normalizeGitDeltaRelativePath(fields[i])
		i++
		if oldPath == "" {
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				i++
			}
			continue
		}
		switch status[0] {
		case 'D':
			deleted = append(deleted, oldPath)
		case 'R':
			deleted = append(deleted, oldPath)
			if i >= len(fields) {
				continue
			}
			newPath := normalizeGitDeltaRelativePath(fields[i])
			i++
			if newPath != "" {
				changed = append(changed, filepath.Join(repoPath, filepath.FromSlash(newPath)))
			}
		case 'C':
			if i >= len(fields) {
				continue
			}
			newPath := normalizeGitDeltaRelativePath(fields[i])
			i++
			if newPath != "" {
				changed = append(changed, filepath.Join(repoPath, filepath.FromSlash(newPath)))
			}
		default:
			changed = append(changed, filepath.Join(repoPath, filepath.FromSlash(oldPath)))
		}
	}
	return GitSyncDelta{
		ChangedFileTargets:   sortUniquePathStrings(changed),
		DeletedRelativePaths: sortUniquePathStrings(deleted),
	}
}

func normalizeGitDeltaRelativePath(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || path == "" || strings.HasPrefix(path, "../") || path == ".." {
		return ""
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, ".git/") || path == ".git" {
		return ""
	}
	return path
}

func sortUniquePathStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// cloneRepository clones repoID into repoPath under the clone deadline and the
// bounded retry (#8091). A killed or failed clone leaves a partial directory,
// and git refuses to clone into a non-empty one, so the retry removes it first
// (Command.BeforeRetry); the final failure removes it too, best effort, as it
// always did. The clone is not idempotent in place, which is why the retry
// removes the directory instead of resuming into it.
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
	if err := deadline.RunRetrying(ctx, config.GitDeadlines, deadline.Command{
		Operation: deadline.Clone,
		Host:      gitRetryHost(config, repoPath),
		Scope:     normalizeRepositoryID(repoID),
		Build: func(deadlineCtx context.Context) *exec.Cmd {
			progress.Flush()
			stderr.Reset()
			command := newGitCommand(
				deadlineCtx,
				append(config.GitDeadlines.LowSpeedArgs(),
					"clone",
					"--progress",
					fmt.Sprintf("--depth=%d", config.CloneDepth),
					"--single-branch",
					remoteURL,
					repoPath,
				)...,
			)
			command.Env = gitCommandEnv(config, token, repoPath)
			command.Stderr = progress
			return command
		},
		Stderr:      func() string { return sanitizeGitStderrLines(stderr.String()) },
		BeforeRetry: func() error { return os.RemoveAll(repoPath) },
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
