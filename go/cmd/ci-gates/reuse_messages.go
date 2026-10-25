// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// sameReuseMessages guards every reuse link using the complete immutable
// parent1..tested-merge range. Gate inputs include all stored messages in that
// range, not only the new head's message. The one GitHub-generated merge
// message varies with the head SHA and is excluded only after exact validation.
func sameReuseMessages(ctx context.Context, runner ghRunner, repo string, source, target testedInput) (bool, error) {
	if !fullLowerSHA(source.BaseSHA) || source.BaseSHA != target.BaseSHA ||
		!fullLowerSHA(source.HeadSHA) || !fullLowerSHA(target.HeadSHA) ||
		!fullLowerSHA(source.MergeSHA) || !fullLowerSHA(target.MergeSHA) {
		return false, nil
	}
	sourceMessages, sourceOK, err := testedMessages(ctx, runner, repo, source)
	if err != nil || !sourceOK {
		return false, err
	}
	targetMessages, targetOK, err := testedMessages(ctx, runner, repo, target)
	if err != nil || !targetOK || len(sourceMessages) != len(targetMessages) {
		return false, err
	}
	for message, count := range sourceMessages {
		if targetMessages[message] != count {
			return false, nil
		}
	}
	return true, nil
}

// reuseComparePage is one immutable compare page. Pointers distinguish absent
// evidence from a valid empty commit message.
type reuseComparePage struct {
	TotalCommits *int `json:"total_commits"`
	Commits      []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message *string `json:"message"`
		} `json:"commit"`
	} `json:"commits"`
}

// testedMessages reads every compare page. A missing page, record, message, or
// synthetic merge refuses transfer; the endpoint uses immutable SHAs only.
func testedMessages(ctx context.Context, runner ghRunner, repo string, input testedInput) (map[string]int, bool, error) {
	if !validRepoName(repo) || !fullLowerSHA(input.BaseSHA) || !fullLowerSHA(input.HeadSHA) || !fullLowerSHA(input.MergeSHA) {
		return nil, false, nil
	}
	endpoint := "repos/" + repo + "/compare/" + input.BaseSHA + "..." + input.MergeSHA + "?per_page=100&page="
	messages := make(map[string]int)
	seen := make(map[string]bool)
	count, synthetic, head, total := 0, 0, 0, 0
	for pageIndex := 1; ; pageIndex++ {
		raw, err := runner.Run(ctx, "api", endpoint+fmt.Sprint(pageIndex))
		if err != nil {
			return nil, false, fmt.Errorf("read immutable tested messages page %d: %w", pageIndex, err)
		}
		var page reuseComparePage
		if err := json.Unmarshal(raw, &page); err != nil || page.TotalCommits == nil {
			return nil, false, nil
		}
		if pageIndex == 1 {
			total = *page.TotalCommits
			// A two-minute scheduling probe caps API reads. Larger histories
			// take the ordinary full path instead of downloading all pages.
			if total < 1 || total > 1000 {
				return nil, false, nil
			}
		}
		if *page.TotalCommits != total {
			return nil, false, nil
		}
		want := 100
		if pageIndex == (total+99)/100 {
			want = total - (pageIndex-1)*100
		}
		if len(page.Commits) != want {
			return nil, false, nil
		}
		for _, commit := range page.Commits {
			if !fullLowerSHA(commit.SHA) || seen[commit.SHA] || commit.Commit.Message == nil ||
				!utf8.ValidString(*commit.Commit.Message) || strings.ContainsRune(*commit.Commit.Message, utf8.RuneError) {
				return nil, false, nil
			}
			seen[commit.SHA] = true
			count++
			if commit.SHA == input.HeadSHA {
				head++
			}
			if commit.SHA == input.MergeSHA {
				synthetic++
				if *commit.Commit.Message != "Merge "+input.HeadSHA+" into "+input.BaseSHA {
					return nil, false, nil
				}
				continue
			}
			messages[*commit.Commit.Message]++
		}
		if pageIndex == (total+99)/100 {
			break
		}
	}
	return messages, count == total && synthetic == 1 && head == 1, nil
}
