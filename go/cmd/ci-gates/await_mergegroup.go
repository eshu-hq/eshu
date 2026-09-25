// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// Triggering events `ci-gates await` can aggregate.
const (
	// eventPullRequest aggregates a pull request head through `gh pr checks`.
	eventPullRequest = "pull_request"
	// eventMergeGroup aggregates a merge-queue commit. A merge group has no
	// pull request of its own, so its changed paths come from the compare API
	// and its check rows from the commit's check runs.
	eventMergeGroup = "merge_group"
)

// mergeGroupCompareFileCap is the most files the compare API lists for one
// comparison (GitHub REST "Compare two commits": the response lists at most
// 300 files). A listing that reaches it may be partial, so the caller selects
// every blocking gate instead of path-matching.
const mergeGroupCompareFileCap = 300

// validateAwaitTarget checks the flag combination that identifies what is
// being aggregated. A pull request needs its number; a merge group must not
// carry one (it would name only the last PR of a possibly multi-PR group) and
// needs the base branch its changes are diffed against.
func validateAwaitTarget(event string, pr int, baseRef string) error {
	switch event {
	case eventPullRequest:
		if pr <= 0 {
			return fmt.Errorf("--pr is required for --event %s", eventPullRequest)
		}
	case eventMergeGroup:
		if pr != 0 {
			return fmt.Errorf("--pr must not be set for --event %s; the merge group commit is the target", eventMergeGroup)
		}
		if strings.TrimSpace(baseRef) == "" {
			return fmt.Errorf("--base-ref is required for --event %s", eventMergeGroup)
		}
	default:
		return fmt.Errorf("--event must be %s or %s, got %q", eventPullRequest, eventMergeGroup, event)
	}
	return nil
}

// runAwaitMergeGroup selects the blocking gates for everything the merge group
// commit adds on top of baseRef and waits for their merge_group check rows.
//
// The diff is three-dot (merge base of baseRef and the group head), so it
// covers every queued PR ahead of this one that has not reached baseRef yet.
// That is a superset of what the leaf workflows see when they compute their
// own selection earlier in the group's life, so the aggregate can never
// demand a gate a workflow legitimately skipped.
func runAwaitMergeGroup(
	ctx context.Context,
	runner ghRunner,
	reg *cigates.Registry,
	root, repo, baseRef, headSHA string,
	pollInterval time.Duration,
) error {
	paths, truncated, err := changedPathsForMergeGroup(ctx, runner, repo, baseRef, headSHA)
	if err != nil {
		return err
	}
	resolved, err := selectRequiredGates(reg, root, paths, truncated)
	if err != nil {
		return err
	}
	for i := range resolved {
		resolved[i].Event = eventMergeGroup
	}
	return awaitMergeGroupRequiredChecks(ctx, runner, repo, headSHA, resolved, pollInterval, os.Stdout)
}

// changedPathsForMergeGroup lists the paths changed between baseRef and the
// merge group head via the compare API. Renames contribute both names, as
// changedPathsForPR does, so a gate triggered by the old path still runs.
func changedPathsForMergeGroup(ctx context.Context, runner ghRunner, repo, baseRef, headSHA string) (paths []string, truncated bool, err error) {
	endpoint := "repos/" + repo + "/compare/" + baseRef + "..." + headSHA
	output, err := runner.Run(ctx, "api", endpoint)
	if err != nil {
		return nil, false, fmt.Errorf("compare %s...%s: %w", baseRef, headSHA, err)
	}
	var comparison struct {
		Files []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := json.Unmarshal(output, &comparison); err != nil {
		return nil, false, fmt.Errorf("decode compare %s...%s: %w", baseRef, headSHA, err)
	}
	seen := make(map[string]struct{})
	for _, file := range comparison.Files {
		for _, path := range []string{file.Filename, file.PreviousFilename} {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, duplicate := seen[path]; duplicate {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	if len(comparison.Files) >= mergeGroupCompareFileCap {
		return paths, true, nil
	}
	if len(paths) == 0 {
		return nil, false, fmt.Errorf("merge group %s has no changed paths against %s", headSHA, baseRef)
	}
	return paths, false, nil
}

// awaitMergeGroupRequiredChecks waits for the selected gates' merge_group
// check rows on the merge group commit.
func awaitMergeGroupRequiredChecks(
	ctx context.Context,
	runner ghRunner,
	repo, headSHA string,
	required []resolvedRequiredGate,
	pollInterval time.Duration,
	out io.Writer,
) error {
	readChecks := func(ctx context.Context) ([]checkRollup, error) {
		return readMergeGroupChecks(ctx, runner, repo, headSHA)
	}
	return awaitRequiredChecks(ctx, runner, repo, headSHA, eventMergeGroup, readChecks, required, pollInterval, out)
}

// readMergeGroupChecks builds the same rows `gh pr checks` reports, for a
// commit that has no pull request: the check runs of the newest merge_group
// workflow run per workflow name, joined on check suite id. Rows from older
// attempts or other events are dropped, matching gh's newest-row rollup.
func readMergeGroupChecks(ctx context.Context, runner ghRunner, repo, headSHA string) ([]checkRollup, error) {
	runsEndpoint := "repos/" + repo + "/actions/runs?per_page=100&head_sha=" + url.QueryEscape(headSHA) + "&event=" + eventMergeGroup
	runsOutput, err := runner.Run(ctx, "api", "--paginate", "--slurp", runsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("list merge_group runs for %s: %w", headSHA, err)
	}
	var runPages []struct {
		WorkflowRuns []struct {
			Name         string `json:"name"`
			Event        string `json:"event"`
			CheckSuiteID int64  `json:"check_suite_id"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(runsOutput, &runPages); err != nil {
		return nil, fmt.Errorf("decode merge_group runs for %s: %w", headSHA, err)
	}
	suiteWorkflow := make(map[int64]string)
	seenName := make(map[string]struct{})
	for _, page := range runPages {
		for _, run := range page.WorkflowRuns {
			if run.Event != eventMergeGroup {
				continue
			}
			// Newest-first: the first run per name owns the current rows.
			if _, seen := seenName[run.Name]; seen {
				continue
			}
			seenName[run.Name] = struct{}{}
			suiteWorkflow[run.CheckSuiteID] = run.Name
		}
	}

	checksEndpoint := "repos/" + repo + "/commits/" + url.PathEscape(headSHA) + "/check-runs?per_page=100&filter=latest"
	checksOutput, err := runner.Run(ctx, "api", "--paginate", "--slurp", checksEndpoint)
	if err != nil {
		return nil, fmt.Errorf("list check runs for %s: %w", headSHA, err)
	}
	var checkPages []struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			CheckSuite struct {
				ID int64 `json:"id"`
			} `json:"check_suite"`
		} `json:"check_runs"`
	}
	if err := json.Unmarshal(checksOutput, &checkPages); err != nil {
		return nil, fmt.Errorf("decode check runs for %s: %w", headSHA, err)
	}
	var rows []checkRollup
	for _, page := range checkPages {
		for _, run := range page.CheckRuns {
			workflow, ok := suiteWorkflow[run.CheckSuite.ID]
			if !ok {
				continue
			}
			state := run.Status
			if strings.EqualFold(run.Status, "completed") {
				state = run.Conclusion
			}
			rows = append(rows, checkRollup{
				Name:     run.Name,
				State:    strings.ToUpper(state),
				Bucket:   checkRunBucket(run.Status, run.Conclusion),
				Workflow: workflow,
				Event:    eventMergeGroup,
			})
		}
	}
	return rows, nil
}

// checkRunBucket maps a check run's status and conclusion to the bucket gh
// assigns the same row (cli/cli pkg/cmd/pr/checks/aggregate.go), so the
// shared evaluator reads merge-group rows exactly as it reads PR rows. STALE
// and every not-yet-completed status fall to "pending", as gh's default arm
// does; isStaleCheck recognises STALE from the state before that bucket is
// consulted.
func checkRunBucket(status, conclusion string) string {
	if !strings.EqualFold(status, "completed") {
		return "pending"
	}
	switch strings.ToLower(conclusion) {
	case "success":
		return "pass"
	case "skipped", "neutral":
		return "skipping"
	case "cancelled":
		return "cancel"
	case "failure", "timed_out", "action_required", "startup_failure", "error":
		return "fail"
	default:
		return "pending"
	}
}
