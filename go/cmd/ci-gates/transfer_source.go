// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

type attemptJob struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Attempt    int    `json:"run_attempt"`
	Steps      []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"steps"`
}

// readAttemptJobs reads only the exact run attempt. Duplicate names and
// mismatched attempt rows are ambiguous evidence and refuse transfer.
func readAttemptJobs(ctx context.Context, runner ghRunner, repo string, input testedInput) (map[string]attemptJob, error) {
	endpoint := "repos/" + repo + "/actions/runs/" + strconv.FormatInt(input.RunID, 10) + "/attempts/" + strconv.Itoa(input.Attempt) + "/jobs?per_page=100"
	raw, err := runner.Run(ctx, "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return nil, fmt.Errorf("read run %d attempt %d jobs: %w", input.RunID, input.Attempt, err)
	}
	var pages []struct {
		Jobs []attemptJob `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &pages); err != nil || len(pages) == 0 {
		return nil, fmt.Errorf("decode run %d attempt %d jobs: %w", input.RunID, input.Attempt, err)
	}
	jobs := make(map[string]attemptJob)
	for _, page := range pages {
		for _, job := range page.Jobs {
			if job.Name == "" || job.Attempt != input.Attempt {
				return nil, fmt.Errorf("run %d has an unbound attempt job", input.RunID)
			}
			if _, duplicate := jobs[job.Name]; duplicate {
				return nil, fmt.Errorf("run %d has duplicate job %q", input.RunID, job.Name)
			}
			jobs[job.Name] = job
		}
	}
	return jobs, nil
}

func successfulAttemptJob(jobs map[string]attemptJob, name string) bool {
	job, ok := jobs[name]
	return ok && job.Status == "completed" && job.Conclusion == "success"
}

func successfulFullModeStep(jobs map[string]attemptJob, jobName string) bool {
	if !successfulAttemptJob(jobs, jobName) {
		return false
	}
	modeSteps := 0
	for _, step := range jobs[jobName].Steps {
		if step.Name == "Record full plan mode" {
			modeSteps++
			if step.Status != "completed" || step.Conclusion != "success" {
				return false
			}
		}
	}
	return modeSteps == 1
}

func planModeProvenByJobs(jobs map[string]attemptJob, mode string) bool {
	if !successfulAttemptJob(jobs, "pr-plan") {
		return false
	}
	name := "Record " + mode + " plan mode"
	matched := 0
	for _, step := range jobs["pr-plan"].Steps {
		if step.Name == name {
			matched++
			if step.Status != "completed" || step.Conclusion != "success" {
				return false
			}
		}
		for _, other := range []string{"full", "reuse", "smoke"} {
			if other != mode && step.Name == "Record "+other+" plan mode" && step.Conclusion == "success" {
				return false
			}
		}
	}
	return matched == 1
}

func planModeProven(ctx context.Context, runner ghRunner, repo string, input testedInput, mode string) (bool, error) {
	jobs, err := readAttemptJobs(ctx, runner, repo, input)
	if err != nil {
		return false, err
	}
	return planModeProvenByJobs(jobs, mode), nil
}

func fullGoCoreSucceeded(jobs map[string]attemptJob) bool {
	// The Go job is also green in smoke mode, where its composite runs only
	// a selected subset. Its own full-mode step distinguishes execution.
	return successfulFullModeStep(jobs, "go-core")
}

var goRaceShardNames = [...]string{"go-race (1)", "go-race (2)", "go-race (3)", "go-race (4)"}

func fullGoRaceSucceeded(jobs map[string]attemptJob) bool {
	for _, name := range goRaceShardNames {
		if !successfulAttemptJob(jobs, name) {
			return false
		}
	}
	return true
}

// proveFullCoverage accepts a new full root only when the exact caller
// attempt ran its full-mode sentinel, Go heavy lanes, planner, and every
// selected blocking job. Direct legacy leaf runs cannot be credited to the
// Build Test invocation simply because their check names are green.
func proveFullCoverage(ctx context.Context, runner ghRunner, repo string, input testedInput, selected []resolvedRequiredGate, root string, paths []string, truncated bool) (bool, error) {
	codeApplicable, err := fullCodeApplicable(root, paths, truncated)
	if err != nil {
		return false, err
	}
	jobs, err := readAttemptJobs(ctx, runner, repo, input)
	if err != nil {
		return false, err
	}
	if !planModeProvenByJobs(jobs, "full") {
		return false, nil
	}
	for _, gate := range selected {
		name := gate.Job
		if gate.WorkflowFile != "test.yml" {
			if _, ok := cigates.PRWorkflowStem(gate.WorkflowFile); !ok {
				return false, nil
			}
			name = calledCheckName(gate.WorkflowFile, gate.Job)
		}
		if name == "" || !successfulAttemptJob(jobs, name) {
			return false, nil
		}
	}
	if codeApplicable && (!fullGoCoreSucceeded(jobs) || !fullGoRaceSucceeded(jobs)) {
		return false, nil
	}
	return true, nil
}

type publisherStatus struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
}

func latestPublisherStatus(ctx context.Context, runner ghRunner, repo, sha string) (publisherStatus, bool, error) {
	raw, err := runner.Run(ctx, "api", "repos/"+repo+"/commits/"+sha+"/statuses?per_page=100")
	if err != nil {
		return publisherStatus{}, false, fmt.Errorf("read publisher status on %s: %w", sha, err)
	}
	var statuses []publisherStatus
	if err := json.Unmarshal(raw, &statuses); err != nil {
		return publisherStatus{}, false, fmt.Errorf("decode publisher status on %s: %w", sha, err)
	}
	for _, status := range statuses {
		if status.Context == "required-gates-complete" {
			return status, true, nil
		}
	}
	return publisherStatus{}, false, nil
}

// fullBackedSource proves a candidate and its direct full-run root against
// their still-current exact attempts. Historical generic/v2 statuses cannot
// be upgraded by checking a merge tree observed today.
func fullBackedSource(ctx context.Context, runner ghRunner, repo string, pr int, candidate string, contexts []string) (transferProof, testedInput, bool, error) {
	status, found, err := latestPublisherStatus(ctx, runner, repo, candidate)
	if err != nil || !found || status.State != "success" {
		return transferProof{}, testedInput{}, false, err
	}
	proof, ok := parseTransferProof(status.Description)
	if !ok || proof.Mode == "smoke" {
		return transferProof{}, testedInput{}, false, nil
	}
	input, err := readTestedInput(ctx, runner, repo, pr, candidate, proof.Input.RunID, proof.Input.Attempt)
	if err != nil || input.MergeSHA != proof.Input.MergeSHA || input.RunStatus != "completed" || input.RunConclusion != "success" {
		return transferProof{}, testedInput{}, false, err
	}
	jobs, err := readAttemptJobs(ctx, runner, repo, input)
	if err != nil {
		return transferProof{}, testedInput{}, false, err
	}
	for _, contextName := range contexts {
		if contextName == "required-gates-complete" {
			continue
		}
		if !successfulAttemptJob(jobs, contextName) {
			return transferProof{}, testedInput{}, false, nil
		}
	}
	rootSHA := candidate
	if proof.Mode == "reuse" {
		rootSHA = proof.FullRootSHA
		if rootSHA == candidate {
			return transferProof{}, testedInput{}, false, nil
		}
		// A reuse attempt must not masquerade as a full execution. Source
		// aggregates can pass, but the heavy jobs must have skipped.
		if successfulAttemptJob(jobs, "go-core") {
			return transferProof{}, testedInput{}, false, nil
		}
		for _, job := range goRaceShardNames {
			if successfulAttemptJob(jobs, job) {
				return transferProof{}, testedInput{}, false, nil
			}
		}
		if !planModeProvenByJobs(jobs, "reuse") {
			return transferProof{}, testedInput{}, false, nil
		}
	} else if !planModeProvenByJobs(jobs, "full") {
		return transferProof{}, testedInput{}, false, nil
	}
	rootInput, rootJobs := input, jobs
	if proof.Mode == "reuse" {
		rootStatus, found, err := latestPublisherStatus(ctx, runner, repo, rootSHA)
		if err != nil || !found || rootStatus.State != "success" {
			return transferProof{}, testedInput{}, false, err
		}
		rootProof, ok := parseTransferProof(rootStatus.Description)
		if !ok || rootProof.Mode != "full" {
			return transferProof{}, testedInput{}, false, nil
		}
		rootInput, err = readTestedInput(ctx, runner, repo, pr, rootSHA, rootProof.Input.RunID, rootProof.Input.Attempt)
		if err != nil || rootInput.MergeSHA != rootProof.Input.MergeSHA || rootInput.MergeTree != input.MergeTree || rootInput.RunStatus != "completed" || rootInput.RunConclusion != "success" {
			return transferProof{}, testedInput{}, false, err
		}
		rootHead, err := gitCommit(ctx, runner, repo, rootSHA)
		if err != nil {
			return transferProof{}, testedInput{}, false, err
		}
		candidateHead, err := gitCommit(ctx, runner, repo, candidate)
		if err != nil {
			return transferProof{}, testedInput{}, false, err
		}
		if rootHead.Tree.SHA != candidateHead.Tree.SHA {
			return transferProof{}, testedInput{}, false, nil
		}
		rootJobs, err = readAttemptJobs(ctx, runner, repo, rootInput)
		if err != nil {
			return transferProof{}, testedInput{}, false, err
		}
	}
	if !planModeProvenByJobs(rootJobs, "full") {
		return transferProof{}, testedInput{}, false, nil
	}
	proof.FullRootSHA = rootSHA
	proof.Input = input
	return proof, input, true, nil
}
