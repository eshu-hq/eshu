// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// changedPathsForPR returns the PR's changed paths and whether the listing
// was truncated. The pull-files endpoint caps at 3000 entries: when the
// returned count differs from changedFiles the path list is partial, so the
// caller must select every blocking gate instead of path-matching.
func changedPathsForPR(ctx context.Context, runner ghRunner, repo string, pr int) (paths []string, truncated bool, err error) {
	endpoint := "repos/" + repo + "/pulls/" + strconv.Itoa(pr) + "/files"
	output, err := runner.Run(ctx, "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return nil, false, fmt.Errorf("list changed files for PR #%d: %w", pr, err)
	}
	var pages [][]struct {
		Filename         string `json:"filename"`
		PreviousFilename string `json:"previous_filename"`
	}
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, false, fmt.Errorf("decode changed files for PR #%d: %w", pr, err)
	}
	seen := make(map[string]struct{})
	fileCount := 0
	appendPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, duplicate := seen[path]; duplicate {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, page := range pages {
		for _, file := range page {
			fileCount++
			appendPath(file.Filename)
			appendPath(file.PreviousFilename)
		}
	}
	countOutput, err := runner.Run(
		ctx,
		"pr", "view", strconv.Itoa(pr),
		"--repo", repo,
		"--json", "changedFiles",
		"--jq", ".changedFiles",
	)
	if err != nil {
		return nil, false, fmt.Errorf("read changed-file count for PR #%d: %w", pr, err)
	}
	expectedCount, err := strconv.Atoi(strings.TrimSpace(string(countOutput)))
	if err != nil {
		return nil, false, fmt.Errorf("decode changed-file count for PR #%d: %w", pr, err)
	}
	if fileCount != expectedCount {
		// Truncated listing: the partial path list is still returned
		// for logging, but truncated=true tells the caller to select
		// every blocking gate instead of path-matching.
		return paths, true, nil
	}
	if len(paths) == 0 {
		return nil, false, fmt.Errorf("PR #%d has no changed file paths", pr)
	}
	return paths, false, nil
}

// Per-commit union is deliberately not a fallback here: single-commit file
// lists cap at 300 entries, so the union is partial too (see
// TestChangedPathsForPR_ReportsTruncationOnCappedFilesResponse).
func verifyPRHead(ctx context.Context, runner ghRunner, repo string, pr int, expected string) error {
	output, err := runner.Run(
		ctx,
		"pr", "view", strconv.Itoa(pr),
		"--repo", repo,
		"--json", "headRefOid",
		"--jq", ".headRefOid",
	)
	if err != nil {
		return fmt.Errorf("read PR #%d head: %w", pr, err)
	}
	actual := strings.TrimSpace(string(output))
	if actual != expected {
		return fmt.Errorf("PR #%d head changed while aggregating checks: got %s, want %s", pr, actual, expected)
	}
	return nil
}

func readPRChecks(ctx context.Context, runner ghRunner, repo string, pr int) ([]checkRollup, error) {
	output, commandErr := runner.Run(
		ctx,
		"pr", "checks", strconv.Itoa(pr),
		"--repo", repo,
		"--json", "name,state,bucket,workflow,event",
	)
	var checks []checkRollup
	if err := json.Unmarshal(output, &checks); err != nil {
		if commandErr != nil {
			return nil, fmt.Errorf("read PR #%d checks: %w", pr, commandErr)
		}
		return nil, fmt.Errorf("decode PR #%d checks: %w", pr, err)
	}
	// `gh pr checks` intentionally exits 8 while checks are pending and 1 when
	// a check is red. The JSON is still authoritative in both cases, so a valid
	// payload is evaluated instead of treating those status exits as API errors.
	return checks, nil
}
