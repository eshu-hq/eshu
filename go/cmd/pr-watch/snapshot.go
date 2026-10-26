// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Check is one entry of a PR head's status check set: a check run or a legacy
// status context.
type Check struct {
	// ID is a stable identity for one run of the check. A rerun of a check
	// with the same name gets a new ID, which is how a second failure of the
	// same job is reported again.
	ID   string
	Name string
	// Status is the check run status (QUEUED, IN_PROGRESS, COMPLETED, ...).
	// Empty for a status context.
	Status string
	// Result is the check run conclusion or the status context state.
	Result string
	URL    string
}

// Thread is one review thread.
type Thread struct {
	ID       string
	Resolved bool
	Path     string
	Line     int
	Author   string
	Body     string
	URL      string
}

// Review is one submitted pull request review.
type Review struct {
	ID     string
	Author string
	State  string
	Body   string
	URL    string
}

// Comment is one conversation (issue) comment.
type Comment struct {
	ID     string
	Author string
	Body   string
	URL    string
}

// Snapshot is everything the watcher reads about one PR in one poll. It is
// rebuilt from GitHub on every poll; nothing in it is carried between polls.
type Snapshot struct {
	Number      int
	State       string // OPEN, MERGED, or CLOSED
	MergedAt    string
	MergeCommit string
	Head        string
	HeadRef     string
	HeadTitle   string
	HeadAuthor  string
	BaseRef     string
	BaseOID     string
	Mergeable   string
	MergeState  string
	// RollupState is the head commit's combined check state (SUCCESS,
	// PENDING, FAILURE, ERROR, EXPECTED), empty when the head has no checks.
	RollupState string
	// TotalChecks is the rollup's own total count, not the number of nodes
	// read, so a set larger than one page is still counted correctly.
	TotalChecks int
	Checks      []Check
	Threads     []Thread
	Reviews     []Review
	Comments    []Comment
}

// pageInfo is a GraphQL cursor page descriptor.
type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type wireActor struct {
	Login string `json:"login"`
}

func (a *wireActor) login() string {
	if a == nil {
		return "ghost"
	}
	return a.Login
}

type wireContext struct {
	Typename   string `json:"__typename"`
	DatabaseID int64  `json:"databaseId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"detailsUrl"`
	Context    string `json:"context"`
	State      string `json:"state"`
	TargetURL  string `json:"targetUrl"`
}

type wireContexts struct {
	TotalCount int           `json:"totalCount"`
	PageInfo   pageInfo      `json:"pageInfo"`
	Nodes      []wireContext `json:"nodes"`
}

type wireCommentNode struct {
	ID     string     `json:"id"`
	Author *wireActor `json:"author"`
	Body   string     `json:"body"`
	URL    string     `json:"url"`
	State  string     `json:"state"`
}

type wirePullRequest struct {
	Number      int     `json:"number"`
	State       string  `json:"state"`
	MergedAt    *string `json:"mergedAt"`
	MergeCommit *struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
	HeadRefName string `json:"headRefName"`
	HeadRefOID  string `json:"headRefOid"`
	BaseRefName string `json:"baseRefName"`
	BaseRef     *struct {
		Target struct {
			OID string `json:"oid"`
		} `json:"target"`
	} `json:"baseRef"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
	ReviewThreads    struct {
		Nodes []struct {
			ID         string `json:"id"`
			IsResolved bool   `json:"isResolved"`
			Path       string `json:"path"`
			Line       *int   `json:"line"`
			Comments   struct {
				Nodes []wireCommentNode `json:"nodes"`
			} `json:"comments"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	Reviews struct {
		Nodes []wireCommentNode `json:"nodes"`
	} `json:"reviews"`
	Comments struct {
		Nodes []wireCommentNode `json:"nodes"`
	} `json:"comments"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				OID             string `json:"oid"`
				MessageHeadline string `json:"messageHeadline"`
				Author          *struct {
					User *wireActor `json:"user"`
				} `json:"author"`
				StatusCheckRollup *struct {
					State    string       `json:"state"`
					Contexts wireContexts `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type wireResponse struct {
	Data struct {
		Repository struct {
			PullRequest *wirePullRequest `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// parseResponse decodes one GraphQL response into a Snapshot plus the cursor
// for the next page of checks. It returns an error when GitHub reports errors
// or the pull request is absent.
func parseResponse(raw []byte) (Snapshot, pageInfo, error) {
	var resp wireResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Snapshot{}, pageInfo{}, fmt.Errorf("decode graphql response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return Snapshot{}, pageInfo{}, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}
	pr := resp.Data.Repository.PullRequest
	if pr == nil {
		return Snapshot{}, pageInfo{}, fmt.Errorf("graphql response has no pull request")
	}
	snap := Snapshot{
		Number:     pr.Number,
		State:      pr.State,
		Head:       pr.HeadRefOID,
		HeadRef:    pr.HeadRefName,
		BaseRef:    pr.BaseRefName,
		Mergeable:  pr.Mergeable,
		MergeState: pr.MergeStateStatus,
	}
	if pr.MergedAt != nil {
		snap.MergedAt = *pr.MergedAt
	}
	if pr.MergeCommit != nil {
		snap.MergeCommit = pr.MergeCommit.OID
	}
	if pr.BaseRef != nil {
		snap.BaseOID = pr.BaseRef.Target.OID
	}
	for _, t := range pr.ReviewThreads.Nodes {
		th := Thread{ID: t.ID, Resolved: t.IsResolved, Path: t.Path}
		if t.Line != nil {
			th.Line = *t.Line
		}
		if len(t.Comments.Nodes) > 0 {
			first := t.Comments.Nodes[0]
			th.Author, th.Body, th.URL = first.Author.login(), first.Body, first.URL
		}
		snap.Threads = append(snap.Threads, th)
	}
	for _, r := range pr.Reviews.Nodes {
		snap.Reviews = append(snap.Reviews, Review{ID: r.ID, Author: r.Author.login(), State: r.State, Body: r.Body, URL: r.URL})
	}
	for _, c := range pr.Comments.Nodes {
		snap.Comments = append(snap.Comments, Comment{ID: c.ID, Author: c.Author.login(), Body: c.Body, URL: c.URL})
	}
	var next pageInfo
	if n := len(pr.Commits.Nodes); n > 0 {
		commit := pr.Commits.Nodes[n-1].Commit
		snap.HeadTitle = commit.MessageHeadline
		if commit.Author != nil {
			snap.HeadAuthor = commit.Author.User.login()
		}
		if rollup := commit.StatusCheckRollup; rollup != nil {
			snap.RollupState = rollup.State
			snap.TotalChecks = rollup.Contexts.TotalCount
			snap.Checks = convertChecks(rollup.Contexts.Nodes)
			next = rollup.Contexts.PageInfo
		}
	}
	return snap, next, nil
}

func convertChecks(nodes []wireContext) []Check {
	checks := make([]Check, 0, len(nodes))
	for _, n := range nodes {
		switch n.Typename {
		case "CheckRun":
			checks = append(checks, Check{
				ID: fmt.Sprintf("run-%d", n.DatabaseID), Name: n.Name,
				Status: n.Status, Result: n.Conclusion, URL: n.DetailsURL,
			})
		default:
			checks = append(checks, Check{
				ID: "status-" + n.Context + "-" + n.State, Name: n.Context,
				Result: n.State, URL: n.TargetURL,
			})
		}
	}
	return checks
}

// Failing reports whether the check ended in a state that needs attention.
// The set is matched as a whole: ACTION_REQUIRED, STARTUP_FAILURE, STALE,
// TIMED_OUT, and CANCELLED are failures as much as FAILURE is, and a status
// context can end in ERROR.
func (c Check) Failing() bool {
	switch strings.ToUpper(c.Result) {
	case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE", "ERROR":
		return true
	}
	return false
}

// Pending reports whether the check has not finished. A running check has no
// conclusion, so pending is decided from status, never from a nil conclusion.
func (c Check) Pending() bool {
	if c.Status != "" {
		return c.Status != "COMPLETED"
	}
	return c.Result == "PENDING" || c.Result == "EXPECTED"
}
