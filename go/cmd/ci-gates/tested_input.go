// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// testedInput identifies the immutable merge tree tested by one exact Build
// Test invocation. Run identity is kept alongside the commit because a later
// attempt can replace the jobs that supplied a green verdict.
type testedInput struct {
	RunID         int64
	Attempt       int
	MergeSHA      string
	MergeTree     string
	BaseSHA       string
	HeadSHA       string
	RunStatus     string
	RunConclusion string
}

type testedInputContextKey struct{}

func withTestedInput(ctx context.Context, input testedInput) context.Context {
	return context.WithValue(ctx, testedInputContextKey{}, input)
}

func currentTestedInput(ctx context.Context) (testedInput, bool) {
	input, ok := ctx.Value(testedInputContextKey{}).(testedInput)
	return input, ok && input.RunID > 0 && input.Attempt > 0 && fullLowerSHA(input.MergeSHA) && fullLowerSHA(input.MergeTree)
}

func nativeRunAttempt() (int64, int, error) {
	runID, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("GITHUB_RUN_ID is not a positive run ID: %w", err)
	}
	attempt, err := strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT"))
	if err != nil {
		return 0, 0, fmt.Errorf("GITHUB_RUN_ATTEMPT is not positive: %w", err)
	}
	if runID <= 0 || runID > 1_000_000_000_000_000 || attempt <= 0 || attempt > 1000 {
		return 0, 0, fmt.Errorf("native run identity is out of range")
	}
	return runID, attempt, nil
}

// readTestedInput binds a caller invocation to the immutable PR merge commit
// named by all its local reusable workflows. A moving PR merge ref is never
// read; it only serves as an association that is checked against the run.
func readTestedInput(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, runID int64, attempt int) (testedInput, error) {
	if !validRepoName(repo) || pr < 1 || !fullLowerSHA(headSHA) || runID <= 0 || runID > 1_000_000_000_000_000 || attempt <= 0 || attempt > 1000 {
		return testedInput{}, fmt.Errorf("invalid tested-input target")
	}
	endpoint := "repos/" + repo + "/actions/runs/" + strconv.FormatInt(runID, 10)
	raw, err := runner.Run(ctx, "api", endpoint)
	if err != nil {
		return testedInput{}, fmt.Errorf("read Build Test run %d: %w", runID, err)
	}
	var run calledWorkflowRun
	if err := json.Unmarshal(raw, &run); err != nil {
		return testedInput{}, fmt.Errorf("decode Build Test run %d: %w", runID, err)
	}
	if run.ID != runID || run.RunAttempt != attempt || run.Repository.FullName != repo || run.Name != "Build Test" ||
		run.Path != ".github/workflows/test.yml" || run.Event != eventPullRequest || run.HeadSHA != headSHA {
		return testedInput{}, fmt.Errorf("build test run %d does not match target invocation", runID)
	}
	bound, err := calledRunBelongsToPR(ctx, runner, repo, pr, headSHA, run, make(map[string]error))
	if err != nil {
		return testedInput{}, fmt.Errorf("bind Build Test run %d to PR %d: %w", runID, pr, err)
	}
	if !bound {
		return testedInput{}, fmt.Errorf("build test run %d does not belong to PR %d", runID, pr)
	}
	mergeSHA, ok := referencedForkMergeSHA(repo, pr, run.ReferencedWorkflows)
	if !ok {
		return testedInput{}, fmt.Errorf("build test run %d lacks complete immutable local workflow references", runID)
	}
	commitRaw, err := runner.Run(ctx, "api", "repos/"+repo+"/git/commits/"+mergeSHA)
	if err != nil {
		return testedInput{}, fmt.Errorf("read tested merge %s: %w", mergeSHA, err)
	}
	var commit struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := json.Unmarshal(commitRaw, &commit); err != nil {
		return testedInput{}, fmt.Errorf("decode tested merge %s: %w", mergeSHA, err)
	}
	if commit.SHA != mergeSHA || !fullLowerSHA(commit.Tree.SHA) || len(commit.Parents) != 2 ||
		!fullLowerSHA(commit.Parents[0].SHA) || commit.Parents[1].SHA != headSHA {
		return testedInput{}, fmt.Errorf("tested merge %s is not the immutable merge of head %s", mergeSHA, headSHA)
	}
	return testedInput{RunID: runID, Attempt: attempt, MergeSHA: mergeSHA, MergeTree: commit.Tree.SHA, BaseSHA: commit.Parents[0].SHA, HeadSHA: headSHA, RunStatus: run.Status, RunConclusion: run.Conclusion}, nil
}
