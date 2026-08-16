// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// ensureFirstRunIndexed brings the target repository to a queryable state. When
// the chosen runtime already exposes the target as an indexed, complete
// repository it reuses that index. Otherwise it runs a scan, which itself waits
// for indexing completeness (not process health) using the shared readiness
// logic. The returned outcome carries the truthful completeness and readiness
// labels; a non-nil error means readiness was not proven.
func ensureFirstRunIndexed(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	client scan.Client,
	deps Deps,
	opts Options,
) (firstRunIndexOutcome, error) {
	if deps.WorkspaceError != nil {
		return firstRunIndexOutcome{
			Status:       StepFailed,
			Detail:       deps.WorkspaceError.Error(),
			Completeness: "unknown",
			Readiness:    "unresolved-target",
		}, fmt.Errorf("resolve target repository: %w", deps.WorkspaceError)
	}

	if reuse, ok := firstRunDetectExistingIndex(deps, client); ok {
		return reuse, nil
	}

	if deps.RunScan == nil {
		return firstRunIndexOutcome{
			Status:       StepFailed,
			Detail:       "scan seam is not configured",
			Completeness: "unknown",
			Readiness:    "unconfigured",
		}, fmt.Errorf("index repository: scan seam is not configured")
	}

	scanOpts := scan.Options{
		Wait:         true,
		Timeout:      opts.Timeout,
		PollInterval: opts.PollInterval,
		Profile:      opts.Profile,
		Target:       firstRunScanTarget(deps.WorkspaceRoot),
	}
	resolveReposDir := deps.ReposDir
	if resolveReposDir == nil {
		resolveReposDir = scan.ReposDir
	}
	reposDir, err := resolveReposDir(deps.WorkspaceRoot)
	if err != nil {
		return firstRunIndexOutcome{
			Status:       StepFailed,
			Detail:       err.Error(),
			Completeness: "unknown",
			Readiness:    "cache-unresolved",
		}, fmt.Errorf("index repository: %w", err)
	}
	scanOpts.ReposDir = reposDir

	result, err := deps.RunScan(ctx, stdout, stderr, deps.ScanRuntime, scanOpts, false)
	if err != nil {
		return firstRunIndexOutcome{
			Status:       StepFailed,
			Detail:       err.Error(),
			Completeness: firstRunCompletenessFromScan(result),
			Readiness:    firstRunReadinessFromScan(result),
		}, fmt.Errorf("index repository: %w", err)
	}
	return firstRunIndexOutcome{
		Status:       StepOK,
		Detail:       fmt.Sprintf("indexed %s", deps.WorkspaceRoot),
		Completeness: "complete",
		Readiness:    "ready",
	}, nil
}

// firstRunDetectExistingIndex reports whether the chosen runtime already serves
// the target as an indexed repository whose pipeline is drained. It only treats
// the repository as reusable when the readiness verdict confirms completeness,
// never on process health alone.
func firstRunDetectExistingIndex(deps Deps, client scan.Client) (firstRunIndexOutcome, bool) {
	if deps.ListRepos == nil || deps.FetchStatus == nil {
		return firstRunIndexOutcome{}, false
	}
	repos, err := deps.ListRepos(client)
	if err != nil || len(repos.Repositories) == 0 {
		return firstRunIndexOutcome{}, false
	}
	if !firstRunRepoMatchesTarget(deps, repos, deps.WorkspaceRoot) {
		return firstRunIndexOutcome{}, false
	}
	status, err := deps.FetchStatus(client)
	if err != nil {
		return firstRunIndexOutcome{}, false
	}
	verdict := scan.EvaluateReadiness(status)
	if !verdict.Ready {
		return firstRunIndexOutcome{}, false
	}
	return firstRunIndexOutcome{
		Status:       StepOK,
		Detail:       "reused existing indexed repository",
		Completeness: "complete",
		Readiness:    "ready",
	}, true
}

// firstRunRepoMatchesTarget reports whether the resolved target matches an
// already-indexed repository. With no resolvable target the presence of any
// indexed repository is treated as a usable existing index. The match itself
// goes through the caller-supplied selector seam, since the selector matcher
// lives in go/cmd/eshu; a nil seam matches nothing, so a misconfigured caller
// falls back to a fresh scan rather than reusing an unproven index.
func firstRunRepoMatchesTarget(deps Deps, repos RepositoryList, workspaceRoot string) bool {
	target := strings.TrimSpace(workspaceRoot)
	if target == "" {
		return len(repos.Repositories) > 0
	}
	if deps.MatchesSelector == nil {
		return false
	}
	for _, repo := range repos.Repositories {
		if deps.MatchesSelector(repo, target) {
			return true
		}
	}
	return false
}

// firstRunScanTarget builds the scan target for the resolved workspace root.
func firstRunScanTarget(workspaceRoot string) scan.Target {
	return scan.Target{
		Path: workspaceRoot,
		Root: workspaceRoot,
		Kind: scan.TargetKind(workspaceRoot, false),
	}
}

// firstRunCompletenessFromScan maps a scan result status to a first-run
// completeness label without overstating success.
func firstRunCompletenessFromScan(result scan.Result) string {
	switch result.Status {
	case "ready":
		return "complete"
	case "partial":
		return "partial"
	case "submitted":
		return "stale"
	default:
		return "failed"
	}
}

// firstRunReadinessFromScan extracts a human readiness label from a scan result.
func firstRunReadinessFromScan(result scan.Result) string {
	if state := strings.TrimSpace(result.StatusReport.Health.State); state != "" {
		return state
	}
	return "unproven"
}
