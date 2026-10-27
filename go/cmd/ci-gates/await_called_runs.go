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
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// calledJobsFullAttempts bounds full-page jobs-endpoint reads: one initial
// read plus retries for transient GitHub failures (a 502 on the
// per_page=100 jobs read broke an otherwise green wave on #9130).
const calledJobsFullAttempts = 3

// calledJobsRetryDelays paces full-page jobs-endpoint retries. Small and
// CI-friendly: the whole sequence costs seconds against a 30s poll.
var calledJobsRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second}

// calledJobsSleep waits between jobs-endpoint retries and honors
// cancellation, mirroring the await poll loop. Tests stub it out.
var calledJobsSleep = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// calledAttemptPins bounds how many times a jobs read chases a run whose
// attempt keeps moving under it. Exhaustion fails closed, never green.
const calledAttemptPins = 3

// calledJobPage is one slurped page of an attempt jobs listing.
type calledJobPage struct {
	Jobs []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		RunAttempt int    `json:"run_attempt"`
	} `json:"jobs"`
}

// readCurrentCalledChecks reads only jobs belonging to the newest Build Test
// run's current attempt for the requested PR and trusted workflow path.
// A head can be shared by PRs, and display names are not unique. The jobs endpoint is necessary:
// commit check runs do not identify which rerun attempt produced a row.
//
// The pinned attempt is re-confirmed against a fresh runs listing before the
// rows are classified: an attempt bump mid-read re-reads the new attempt
// instead of erroring, and rows are never mixed across attempts.
func readCurrentCalledChecks(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string) ([]checkRollup, error) {
	commits := make(map[string]error)
	id, attempt, _, err := newestBuildTestRun(ctx, runner, repo, pr, headSHA, commits)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, nil
	}
	if attempt <= 0 {
		return nil, fmt.Errorf("build-test run %d has no current attempt", id)
	}
	var branch string
	for range calledAttemptPins {
		var pages []calledJobPage
		pages, err = readCalledAttemptJobs(ctx, runner, repo, id, attempt)
		if err != nil {
			return nil, err
		}
		var freshID int64
		var freshAttempt int
		freshID, freshAttempt, branch, err = newestBuildTestRun(ctx, runner, repo, pr, headSHA, commits)
		if err != nil {
			return nil, err
		}
		if freshID == 0 {
			return nil, fmt.Errorf("build-test run %d attempt %d disappeared during jobs read", id, attempt)
		}
		if freshID != id || freshAttempt != attempt {
			id, attempt = freshID, freshAttempt
			if attempt <= 0 {
				return nil, fmt.Errorf("build-test run %d has no current attempt", id)
			}
			continue
		}
		return classifyCalledJobs(pages, id, attempt, branch, headSHA)
	}
	return nil, fmt.Errorf("build-test run %d attempt %d kept moving during jobs read", id, attempt)
}

// newestBuildTestRun returns the newest bound Build Test run for the head: the
// highest run id, breaking ties by highest attempt. Unbound runs stay
// excluded, exactly as before.
func newestBuildTestRun(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, commits map[string]error) (int64, int, string, error) {
	endpoint := "repos/" + repo + "/actions/runs?per_page=100&head_sha=" + url.QueryEscape(headSHA) + "&event=pull_request"
	output, err := runner.Run(ctx, "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return 0, 0, "", fmt.Errorf("list current Build Test run: %w", err)
	}
	var pages []struct {
		WorkflowRuns []calledWorkflowRun `json:"workflow_runs"`
	}
	if err := json.Unmarshal(output, &pages); err != nil {
		return 0, 0, "", fmt.Errorf("decode Build Test runs: %w", err)
	}
	var id int64
	var attempt int
	var branch string
	for _, page := range pages {
		for _, run := range page.WorkflowRuns {
			if run.Name != "Build Test" || run.Path != ".github/workflows/test.yml" ||
				run.Event != eventPullRequest || run.HeadSHA != headSHA {
				continue
			}
			bound, bindErr := calledRunBelongsToPR(ctx, runner, repo, pr, headSHA, run, commits)
			if bindErr != nil {
				return 0, 0, "", bindErr
			}
			if !bound {
				continue
			}
			if run.ID > id || run.ID == id && run.RunAttempt > attempt {
				id, attempt, branch = run.ID, run.RunAttempt, run.HeadBranch
			}
		}
	}
	return id, attempt, branch, nil
}

// readCalledAttemptJobs reads one attempt's jobs with bounded retries. Repeated
// full-page failures fall back to a small-page read: tiny pages already pass
// through gh --paginate where the full page 502s (#9130). Every failure
// mode returns an error, never a partial or empty green.
func readCalledAttemptJobs(ctx context.Context, runner ghRunner, repo string, id int64, attempt int) ([]calledJobPage, error) {
	base := "repos/" + repo + "/actions/runs/" + strconv.FormatInt(id, 10) + "/attempts/" + strconv.Itoa(attempt) + "/jobs"
	full := base + "?per_page=100"
	var lastErr error
	for i := range calledJobsFullAttempts {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("list Build Test attempt %d jobs: %w", attempt, ctxErr)
		}
		var output []byte
		output, lastErr = runner.Run(ctx, "api", "--paginate", "--slurp", full)
		if lastErr == nil {
			return decodeCalledJobs(output)
		}
		if i+1 < calledJobsFullAttempts {
			delay := calledJobsRetryDelays[len(calledJobsRetryDelays)-1]
			if i < len(calledJobsRetryDelays) {
				delay = calledJobsRetryDelays[i]
			}
			if sleepErr := calledJobsSleep(ctx, delay); sleepErr != nil {
				return nil, fmt.Errorf("list Build Test attempt %d jobs: %w", attempt, sleepErr)
			}
		}
	}
	small, smallErr := runner.Run(ctx, "api", "--paginate", "--slurp", base+"?per_page=1")
	if smallErr != nil {
		return nil, fmt.Errorf("list Build Test attempt %d jobs after %d tries: %v; small-page fallback: %v",
			attempt, calledJobsFullAttempts, lastErr, smallErr)
	}
	return decodeCalledJobs(small)
}

func decodeCalledJobs(output []byte) ([]calledJobPage, error) {
	var pages []calledJobPage
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, fmt.Errorf("decode Build Test jobs: %w", err)
	}
	return pages, nil
}

// classifyCalledJobs rolls one pinned attempt's jobs into checks. Rows from
// any other attempt are genuinely mixed evidence and error; duplicate names
// stay AMBIGUOUS. Both fail closed.
func classifyCalledJobs(pages []calledJobPage, id int64, attempt int, branch, headSHA string) ([]checkRollup, error) {
	var checks []checkRollup
	names := make(map[string]int)
	for _, page := range pages {
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
