// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// readCurrentCalledChecks reads only jobs belonging to the newest Build Test
// run's current attempt for the requested PR and trusted workflow path.
// A head can be shared by PRs, and display names are not unique. The jobs endpoint is necessary:
// commit check runs do not identify which rerun attempt produced a row.
func readCurrentCalledChecks(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string) ([]checkRollup, error) {
	endpoint := "repos/" + repo + "/actions/runs?per_page=100&head_sha=" + url.QueryEscape(headSHA) + "&event=pull_request"
	output, err := runner.Run(ctx, "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return nil, fmt.Errorf("list current Build Test run: %w", err)
	}
	var pages []struct {
		WorkflowRuns []struct {
			ID           int64  `json:"id"`
			Name         string `json:"name"`
			Path         string `json:"path"`
			Event        string `json:"event"`
			HeadSHA      string `json:"head_sha"`
			HeadBranch   string `json:"head_branch"`
			RunAttempt   int    `json:"run_attempt"`
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, fmt.Errorf("decode Build Test runs: %w", err)
	}
	var id int64
	var attempt int
	var branch string
	for _, page := range pages {
		for _, run := range page.WorkflowRuns {
			if run.Name != "Build Test" || run.Path != ".github/workflows/test.yml" ||
				run.Event != eventPullRequest || run.HeadSHA != headSHA ||
				len(run.PullRequests) != 1 || run.PullRequests[0].Number != pr {
				continue
			}
			if run.ID > id || run.ID == id && run.RunAttempt > attempt {
				id, attempt, branch = run.ID, run.RunAttempt, run.HeadBranch
			}
		}
	}
	if id == 0 {
		return nil, nil
	}
	if attempt <= 0 {
		return nil, fmt.Errorf("build-test run %d has no current attempt", id)
	}
	jobsEndpoint := "repos/" + repo + "/actions/runs/" + strconv.FormatInt(id, 10) + "/attempts/" + strconv.Itoa(attempt) + "/jobs?per_page=100"
	output, err = runner.Run(ctx, "api", "--paginate", "--slurp", jobsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("list Build Test attempt %d jobs: %w", attempt, err)
	}
	var jobPages []struct {
		Jobs []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			RunAttempt int    `json:"run_attempt"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(output, &jobPages); err != nil {
		return nil, fmt.Errorf("decode Build Test jobs: %w", err)
	}
	var checks []checkRollup
	names := make(map[string]int)
	for _, page := range jobPages {
		for _, job := range page.Jobs {
			if !strings.HasPrefix(job.Name, "call-") || !strings.Contains(job.Name, " / ") {
				continue
			}
			if job.RunAttempt != attempt {
				return nil, fmt.Errorf("build-test job %q belongs to attempt %d, want %d", job.Name, job.RunAttempt, attempt)
			}
			state := job.Status
			if strings.EqualFold(job.Status, "completed") {
				state = job.Conclusion
			}
			check := checkRollup{Name: job.Name, State: strings.ToUpper(state), Bucket: checkRunBucket(job.Status, job.Conclusion), Workflow: "Build Test", Event: eventPullRequest, RunID: id, HeadSHA: headSHA, HeadBranch: branch}
			if prior, duplicate := names[job.Name]; duplicate {
				checks[prior].State, checks[prior].Bucket = "AMBIGUOUS", "fail"
				continue
			}
			names[job.Name] = len(checks)
			checks = append(checks, check)
		}
	}
	return checks, nil
}

func readPRChecksWithCalled(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, required []resolvedRequiredGate) ([]checkRollup, error) {
	files := make([]string, 0, len(required))
	for _, gate := range required {
		if _, ok := cigates.PRWorkflowStem(gate.WorkflowFile); ok {
			files = append(files, gate.WorkflowFile)
		}
	}
	if err := cigates.ValidatePRWorkflowCallerStems(files); err != nil {
		return nil, err
	}
	rollup, err := readPRChecks(ctx, runner, repo, pr)
	if err != nil {
		return nil, err
	}
	aliasSeen := false
	for _, gate := range required {
		alias := calledCheckName(gate.WorkflowFile, gate.Job)
		for _, check := range rollup {
			if alias != "" && check.Workflow == "Build Test" && check.Name == alias && check.Event == eventPullRequest {
				aliasSeen = true
			}
		}
	}
	if !aliasSeen {
		return rollup, nil
	}
	current, err := readCurrentCalledChecks(ctx, runner, repo, pr, headSHA)
	if err != nil {
		return nil, err
	}
	filtered := make([]checkRollup, 0, len(rollup)+len(current))
	for _, check := range rollup {
		if check.Workflow == "Build Test" && strings.HasPrefix(check.Name, "call-") && strings.Contains(check.Name, " / ") {
			continue
		}
		filtered = append(filtered, check)
	}
	filtered = append(filtered, current...)
	// A caller row from an obsolete attempt remains visible in gh's rollup
	// before the current attempt creates its replacement. Hold the gate as
	// pending rather than letting a direct green row hide that absence.
	for _, gate := range required {
		alias := calledCheckName(gate.WorkflowFile, gate.Job)
		if alias == "" {
			continue
		}
		wasSeen, nowSeen := false, false
		for _, check := range rollup {
			if check.Workflow == "Build Test" && check.Name == alias {
				wasSeen = true
			}
		}
		for _, check := range current {
			if check.Name == alias {
				nowSeen = true
			}
		}
		if wasSeen && !nowSeen {
			filtered = append(filtered, checkRollup{Name: alias, State: "PENDING", Bucket: "pending", Workflow: "Build Test", Event: eventPullRequest, HeadSHA: headSHA})
		}
	}
	return filtered, nil
}
