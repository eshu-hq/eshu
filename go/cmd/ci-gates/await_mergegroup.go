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
// needs the base branch it targets and the queue branch that carries its fixed
// base SHA.
func validateAwaitTarget(event string, pr int, baseRef, mergeGroupBranch string) error {
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
		if strings.TrimSpace(mergeGroupBranch) == "" {
			return fmt.Errorf("--merge-group-branch is required for --event %s", eventMergeGroup)
		}
	default:
		return fmt.Errorf("--event must be %s or %s, got %q", eventPullRequest, eventMergeGroup, event)
	}
	return nil
}

// runAwaitMergeGroup selects the registry's merge_group slim set and waits
// for its merge_group check rows. Changed paths play no role: the fixed set
// is the selection (#7859 P3), so no compare call is made and a late run
// after the queue fast-forwards baseRef evaluates the same set it always
// does. The queue branch is still parsed to fail closed on a malformed
// branch: a run that cannot name its group must never pass vacuously. The
// leaf workflows use the same fixed-set selection and union the head registry
// with the trusted default-branch policy. Unselected payload skips expensive
// steps but retains check rows; selected payload must actually run.
func runAwaitMergeGroup(
	ctx context.Context,
	runner ghRunner,
	reg *cigates.Registry,
	root, repo, baseRef, mergeGroupBranch, headSHA string,
	pollInterval time.Duration,
) error {
	if _, err := mergeGroupBaseSHA(mergeGroupBranch, baseRef); err != nil {
		return err
	}
	resolved, err := selectSlimRequiredGates(reg, root)
	if err != nil {
		return err
	}
	for i := range resolved {
		resolved[i].Event = eventMergeGroup
	}
	return awaitMergeGroupRequiredChecks(ctx, runner, repo, headSHA, mergeGroupBranch, resolved, pollInterval, os.Stdout)
}

// selectSlimRequiredGates resolves the registry's merge_group slim set to
// its workflow jobs. A slim set that names no gate is a registry defect, not
// an empty diff: it fails closed rather than passing the queue vacuously.
func selectSlimRequiredGates(reg *cigates.Registry, root string) ([]resolvedRequiredGate, error) {
	slim, err := reg.SlimGates()
	if err != nil {
		return nil, err
	}
	if len(slim) == 0 {
		return nil, fmt.Errorf("merge_group slim set is empty")
	}
	resolved, err := resolveRequiredGateWorkflows(root, slim)
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(os.Stdout, "required-gates: selected %d merge_group slim workflow job(s)\n", len(resolved))
	return resolved, nil
}

// mergeGroupChangedPaths resolves the group's fixed base SHA from its queue
// branch and lists the paths changed since it. It fails closed, with an error
// distinct from an empty diff, when the base SHA cannot be determined: falling
// back to the moving baseRef would reintroduce the empty late-run compare.
// Retained although no production caller remains after the fixed-set cutover:
// its tests pin the fixed-base compare discipline (#7281) the queue still relies on.
func mergeGroupChangedPaths(ctx context.Context, runner ghRunner, repo, baseRef, mergeGroupBranch, headSHA string) (paths []string, truncated bool, err error) {
	baseSHA, err := mergeGroupBaseSHA(mergeGroupBranch, baseRef)
	if err != nil {
		return nil, false, err
	}
	return changedPathsForMergeGroup(ctx, runner, repo, baseSHA, headSHA)
}

// changedPathsForMergeGroup lists the paths changed between baseSHA (the
// group's fixed parent commit) and the merge group head via the compare API. Renames contribute both names, as
// changedPathsForPR does, so a gate triggered by the old path still runs.
func changedPathsForMergeGroup(ctx context.Context, runner ghRunner, repo, baseSHA, headSHA string) (paths []string, truncated bool, err error) {
	endpoint := "repos/" + repo + "/compare/" + baseSHA + "..." + headSHA
	output, err := runner.Run(ctx, "api", endpoint)
	if err != nil {
		return nil, false, fmt.Errorf("compare %s...%s: %w", baseSHA, headSHA, err)
	}
	var comparison struct {
		Files []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := json.Unmarshal(output, &comparison); err != nil {
		return nil, false, fmt.Errorf("decode compare %s...%s: %w", baseSHA, headSHA, err)
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
		return nil, false, fmt.Errorf("merge group %s has no changed paths against base %s", headSHA, baseSHA)
	}
	return paths, false, nil
}

// awaitMergeGroupRequiredChecks waits for the selected gates' merge_group
// check rows on the merge group commit.
func awaitMergeGroupRequiredChecks(
	ctx context.Context,
	runner ghRunner,
	repo, headSHA, mergeGroupBranch string,
	required []resolvedRequiredGate,
	pollInterval time.Duration,
	out io.Writer,
) error {
	printAwaitHeader(out, eventMergeGroup, mergeGroupBranch, headSHA)
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
	return readEventChecks(ctx, runner, repo, headSHA, eventMergeGroup)
}

// readEventChecks lists the workflow runs for a head commit filtered to one
// triggering event, keeps the newest run per workflow name by run id, and
// returns the commit's check runs joined to those runs on check suite id.
//
// Rows from older attempts or other events are dropped: a superseded attempt
// must never own the name, or its completed rows would satisfy -- or fail -- a
// gate the current attempt has not run yet (#8053: the pull-request await
// picked a failed `differential` row from a cancelled, superseded event while
// the newest event was still running). Each row carries its owning workflow
// run id so the await log names the run that was judged.
func readEventChecks(ctx context.Context, runner ghRunner, repo, headSHA, event string) ([]checkRollup, error) {
	runsEndpoint := "repos/" + repo + "/actions/runs?per_page=100&head_sha=" + url.QueryEscape(headSHA) + "&event=" + event
	runsOutput, err := runner.Run(ctx, "api", "--paginate", "--slurp", runsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("list %s runs for %s: %w", event, headSHA, err)
	}
	var runPages []struct {
		WorkflowRuns []struct {
			ID           int64  `json:"id"`
			Name         string `json:"name"`
			Event        string `json:"event"`
			HeadBranch   string `json:"head_branch"`
			HeadSHA      string `json:"head_sha"`
			CheckSuiteID int64  `json:"check_suite_id"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(runsOutput, &runPages); err != nil {
		return nil, fmt.Errorf("decode %s runs for %s: %w", event, headSHA, err)
	}
	// The runs API documents no pagination sort order, so the newest run per
	// workflow name is chosen by run id (ids increase monotonically), not by
	// list position. A superseded attempt must never own the name: its
	// completed rows would satisfy a gate the current attempt has not run yet.
	// On an id tie (only when ids are absent) the first-seen run is kept.
	type newestRun struct {
		id         int64
		suite      int64
		headBranch string
		headSHA    string
	}
	newest := make(map[string]newestRun)
	for _, page := range runPages {
		for _, run := range page.WorkflowRuns {
			if run.Event != event {
				continue
			}
			if current, seen := newest[run.Name]; seen && run.ID <= current.id {
				continue
			}
			newest[run.Name] = newestRun{id: run.ID, suite: run.CheckSuiteID, headBranch: run.HeadBranch, headSHA: run.HeadSHA}
		}
	}
	suiteWorkflow := make(map[int64]string, len(newest))
	suiteRun := make(map[int64]newestRun, len(newest))
	for name, run := range newest {
		suiteWorkflow[run.suite] = name
		suiteRun[run.suite] = run
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
				Name:       run.Name,
				State:      strings.ToUpper(state),
				Bucket:     checkRunBucket(run.Status, run.Conclusion),
				Workflow:   workflow,
				Event:      event,
				RunID:      suiteRun[run.CheckSuite.ID].id,
				HeadBranch: suiteRun[run.CheckSuite.ID].headBranch,
				HeadSHA:    suiteRun[run.CheckSuite.ID].headSHA,
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
