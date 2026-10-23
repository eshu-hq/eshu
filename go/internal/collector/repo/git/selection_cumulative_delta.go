// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// deltaGrowthValveRatio is the cumulative-delta growth valve (#8780): a delta
// whose touched paths reach this fraction of the union base's files takes a
// full snapshot instead (delta_exceeds_base_half). It is the #7926 half rule.
// The prove-the-theory shim measured the p95 touched/base ratio at 0.126 on
// the four busiest repositories even with a 72 h old base
// (docs/internal/evidence/8780-cumulative-delta.md), so the valve is a bound,
// not a tuning knob, and stays a constant.
const deltaGrowthValveRatio = 0.5

// deltaTouchedRatioOutcomeDelta labels a touched-ratio sample whose
// cumulative delta was used; a valve sample carries
// deltaFallbackDeltaExceedsBaseHalf. The two values are the closed outcome
// set of eshu_dp_collector_delta_touched_ratio.
const deltaTouchedRatioOutcomeDelta = "delta"

// cumulativeGitDelta diffs a cumulative delta for the sync at newRef (#8780,
// ruling Option B). The changed set is the added, modified, and renamed-to
// paths of fullCommit..newRef, where fullCommit is the scope's newest
// activated full generation: the union base the projector overlays the delta
// on, so every file that differs from the base is reparsed. The deleted set
// is D(fullCommit..newRef) plus D(activeCommit..newRef): a file the active
// delta chain added and newRef no longer has is in neither the base nor the
// first diff, and without the second term no retract would ever name it.
// One diff runs when the two commits are equal.
//
// The returned reason is non-empty when the delta must not be used: today
// only deltaFallbackDeltaExceedsBaseHalf, when the touched paths reach
// deltaGrowthValveRatio of the base's tracked files. BaseTouchedRatio is set
// whenever the ratio was computed, on both outcomes. The caller sets the
// baseline and union-base commits on a used delta.
func cumulativeGitDelta(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	activeCommit string,
	fullCommit string,
	newRef string,
) (GitSyncDelta, string, error) {
	delta, err := gitDiffDelta(ctx, config, repoPath, token, fullCommit, newRef)
	if err != nil {
		return GitSyncDelta{}, "", err
	}
	if activeCommit != fullCommit {
		sinceActive, err := gitDiffDelta(ctx, config, repoPath, token, activeCommit, newRef)
		if err != nil {
			return GitSyncDelta{}, "", err
		}
		delta.DeletedRelativePaths = sortUniquePathStrings(append(delta.DeletedRelativePaths, sinceActive.DeletedRelativePaths...))
	}
	touched := len(delta.ChangedFileTargets) + len(delta.DeletedRelativePaths)
	if touched == 0 {
		return delta, "", nil
	}
	baseFiles, err := gitTreeFileCount(ctx, config, repoPath, token, fullCommit)
	if err != nil {
		return GitSyncDelta{}, "", err
	}
	delta.BaseTouchedRatio = float64(touched) / float64(max(baseFiles, 1))
	if float64(touched) >= deltaGrowthValveRatio*float64(baseFiles) {
		return GitSyncDelta{BaseTouchedRatio: delta.BaseTouchedRatio}, deltaFallbackDeltaExceedsBaseHalf, nil
	}
	return delta, "", nil
}

// gitTreeFileCount counts the tracked paths of commit's tree, the same raw,
// pre-discovery path set the cumulative diff reports, so the valve compares
// like with like (the #8780 shim counted files the same way).
func gitTreeFileCount(ctx context.Context, config RepoSyncConfig, repoPath, token, commit string) (int, error) {
	output, err := gitRun(ctx, repoPath, config, token, "ls-tree", "-r", "--name-only", "-z", commit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, path := range strings.Split(output, "\x00") {
		if path != "" {
			count++
		}
	}
	return count, nil
}

// syncBaselineFromState builds the delta-sync baseline from a reconcile
// decision's FullReconcileState read in the same cycle (#8780), so the sweep
// path issues no second baseline statement. The state's active generation
// comes through the ingestion_scopes pointer (status 'active') rather than
// the activated_at predicate the standalone read and the #7319 fence share.
// Ack sets both in one transaction, and if they ever disagreed the fence
// would refuse the delta at preflight or Ack, so a stale fence commit cannot
// activate.
func syncBaselineFromState(state scope.FullReconcileState) scope.DeltaSyncBaseline {
	return scope.DeltaSyncBaseline{
		ActiveCommitSHA:              state.ActiveCommitSHA,
		ActiveIsDelta:                state.ActiveIsDelta,
		NewestActivatedFullCommitSHA: state.LastProjectedFullCommitSHA,
	}
}

// deltaSyncLog is the delta part of the git sync log events (#8780): the
// fence commit, the union-base commit, the cumulative set sizes, and whether
// the active generation the delta was diffed over is itself a delta. Commit
// SHAs ride only on logs, never on metric labels.
type deltaSyncLog struct {
	BaselineCommitSHA  string
	UnionBaseCommitSHA string
	ChangedCount       int
	DeletedCount       int
	ActiveIsDelta      bool
}

// withDelta returns e describing delta, diffed over an active generation
// that is a delta when activeIsDelta is true.
func (e gitSyncLogEvent) withDelta(delta GitSyncDelta, activeIsDelta bool) gitSyncLogEvent {
	e.Delta = deltaSyncLog{
		BaselineCommitSHA:  delta.BaselineCommitSHA,
		UnionBaseCommitSHA: delta.UnionBaseCommitSHA,
		ChangedCount:       len(delta.ChangedFileTargets),
		DeletedCount:       len(delta.DeletedRelativePaths),
		ActiveIsDelta:      activeIsDelta,
	}
	return e
}

func (d deltaSyncLog) attrs() []any {
	return []any{
		slog.String("delta_baseline_commit_sha", d.BaselineCommitSHA),
		slog.String("delta_union_base_commit_sha", d.UnionBaseCommitSHA),
		slog.Int("delta_changed_count", d.ChangedCount),
		slog.Int("delta_deleted_count", d.DeletedCount),
		slog.Bool("active_is_delta", d.ActiveIsDelta),
	}
}

// applySelectedDelta marks repository as a delta selection carrying delta's
// changed targets, deleted paths, fence commit, and union base.
func applySelectedDelta(repository *SelectedRepository, delta GitSyncDelta) {
	repository.Delta = true
	repository.FileTargets = sortUniquePathStrings(append(repository.FileTargets, delta.ChangedFileTargets...))
	repository.DeletedRelativePaths = sortUniquePathStrings(delta.DeletedRelativePaths)
	repository.DeltaBaselineCommitSHA = delta.BaselineCommitSHA
	repository.DeltaUnionBaseCommitSHA = delta.UnionBaseCommitSHA
}
