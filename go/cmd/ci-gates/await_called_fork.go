// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// calledWorkflowRun retains the raw PR field: only an explicit empty array permits
// fork fallback. Missing or null association must never become that signal.
type calledWorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Event      string `json:"event"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	RunAttempt          int             `json:"run_attempt"`
	Status              string          `json:"status"`
	Conclusion          string          `json:"conclusion"`
	PullRequests        json.RawMessage `json:"pull_requests"`
	ReferencedWorkflows json.RawMessage `json:"referenced_workflows"`
}

func calledRunBelongsToPR(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, run calledWorkflowRun, commits map[string]error) (bool, error) {
	association := bytes.TrimSpace(run.PullRequests)
	if len(association) == 0 || bytes.Equal(association, []byte("null")) {
		return false, nil
	}
	var prs []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(association, &prs); err != nil {
		return false, fmt.Errorf("decode run %d PR association: %w", run.ID, err)
	}
	if len(prs) != 0 {
		return len(prs) == 1 && prs[0].Number == pr, nil
	}
	mergeSHA, ok := referencedForkMergeSHA(repo, pr, run.ReferencedWorkflows)
	if !ok {
		return false, nil
	}
	key := repo + "@" + mergeSHA
	if _, cached := commits[key]; !cached {
		commits[key] = verifyForkMergeCommit(ctx, runner, repo, mergeSHA, headSHA)
	}
	if err := commits[key]; err != nil {
		return false, fmt.Errorf("bind fork run %d: %w", run.ID, err)
	}
	return true, nil
}

func referencedForkMergeSHA(repo string, pr int, raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var refs []struct {
		Path string `json:"path"`
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
	}
	if err := json.Unmarshal(raw, &refs); err != nil || len(refs) == 0 {
		return "", false
	}
	wantRef := fmt.Sprintf("refs/pull/%d/merge", pr)
	prefix := repo + "/.github/workflows/"
	seen := make(map[string]bool, len(refs))
	var mergeSHA string
	// No workflow stem is privileged: queue-selection.yml is deleted, so the
	// binding is the completeness of the local immutable set itself. Every
	// entry must still be a local reusable workflow pinned to this PR's merge
	// ref at one shared full SHA with no duplicate stems.
	for _, entry := range refs {
		fileAndSHA, ok := strings.CutPrefix(entry.Path, prefix)
		if !ok || entry.Ref != wantRef || !fullLowerSHA(entry.SHA) {
			return "", false
		}
		file, pathSHA, ok := strings.Cut(fileAndSHA, "@")
		if !ok || pathSHA != entry.SHA {
			return "", false
		}
		stem, ok := cigates.PRWorkflowStem(file)
		if !ok || seen[stem] || mergeSHA != "" && mergeSHA != entry.SHA {
			return "", false
		}
		seen[stem] = true
		mergeSHA = entry.SHA
	}
	return mergeSHA, true
}

func verifyForkMergeCommit(ctx context.Context, runner ghRunner, repo, mergeSHA, headSHA string) error {
	endpoint := "repos/" + repo + "/git/commits/" + mergeSHA
	output, err := runner.Run(ctx, "api", endpoint)
	if err != nil {
		return fmt.Errorf("read immutable merge commit %s: %w", mergeSHA, err)
	}
	var commit struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err := json.Unmarshal(output, &commit); err != nil {
		return fmt.Errorf("decode immutable merge commit %s: %w", mergeSHA, err)
	}
	if commit.SHA != mergeSHA || len(commit.Parents) != 2 ||
		!fullLowerSHA(commit.Parents[0].SHA) || !fullLowerSHA(commit.Parents[1].SHA) ||
		commit.Parents[1].SHA != headSHA {
		return fmt.Errorf("immutable merge commit %s is not a two-parent merge of head %s", mergeSHA, headSHA)
	}
	return nil
}

func fullLowerSHA(sha string) bool {
	if len(sha) != 40 {
		return false
	}
	for _, char := range sha {
		if char >= '0' && char <= '9' || char >= 'a' && char <= 'f' {
			continue
		}
		return false
	}
	return true
}
