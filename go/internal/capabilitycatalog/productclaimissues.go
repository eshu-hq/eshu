// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type issueStateResult struct {
	state IssueState
	err   error
}

// CheckProductClaimIssueStates verifies recorded issue states against the live
// GitHub issue API. Callers opt in because local docs checks must remain usable
// without network access.
func CheckProductClaimIssueStates(ctx context.Context, client *http.Client, apiBase, repo, token string, ledger ProductClaimLedger) []ProductClaimFinding {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	apiBase = strings.TrimRight(apiBase, "/")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	attachAuth := shouldAttachIssueAuth(apiBase)
	checked := map[int]issueStateResult{}
	var findings []ProductClaimFinding
	for _, claim := range ledger.Claims {
		for _, issue := range claim.Issues {
			if issue.Number <= 0 {
				continue
			}
			result, ok := checked[issue.Number]
			if !ok {
				state, err := fetchIssueState(ctx, client, apiBase, repo, token, attachAuth, issue.Number)
				result = issueStateResult{state: state, err: err}
				checked[issue.Number] = result
			}
			if result.err != nil {
				findings = append(findings, productClaimFinding(ProductClaimFindingStaleIssue, claim.ID, claim.Source.Path, claim.Source.Line, result.err.Error()))
				continue
			}
			if result.state != issue.State {
				findings = append(findings, productClaimFinding(ProductClaimFindingStaleIssue, claim.ID, claim.Source.Path, claim.Source.Line, fmt.Sprintf("issue %d state=%s expected=%s", issue.Number, result.state, issue.State)))
			}
		}
	}
	return findings
}

func fetchIssueState(ctx context.Context, client *http.Client, apiBase, repo, token string, attachAuth bool, number int) (IssueState, error) {
	url := fmt.Sprintf("%s/repos/%s/issues/%d", apiBase, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build issue-state request for %d: %w", number, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if attachAuth && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch issue %d state: %w", number, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch issue %d state: github status %d", number, resp.StatusCode)
	}
	var payload struct {
		State IssueState `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode issue %d state: %w", number, err)
	}
	if payload.State != IssueStateOpen && payload.State != IssueStateClosed {
		return "", fmt.Errorf("issue %d returned invalid state %q", number, payload.State)
	}
	return payload.State, nil
}

func shouldAttachIssueAuth(apiBase string) bool {
	parsed, err := url.Parse(apiBase)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Hostname() == "api.github.com"
}
