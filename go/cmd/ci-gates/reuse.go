// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// reuseVerdict is the probe's answer: whether a prior head of the same pull
// request already proved this exact tree green on every required context.
type reuseVerdict struct {
	Reuse     bool
	SourceSHA string
	Tree      string
	Reason    string
}

// runReuse backs `ci-gates reuse`: it prints key=value lines a workflow step
// can consume as outputs. A false verdict is a successful probe (exit 0);
// only an inability to probe errors, which callers must treat as no reuse.
func runReuse(args []string) error {
	fs := flag.NewFlagSet("reuse", flag.ContinueOnError)
	registryPath := fs.String("registry", "", "path to ci-gates.v1.yaml registry")
	repo := fs.String("repo", "", "GitHub repository in owner/name form")
	pr := fs.Int("pr", 0, "pull request number")
	headSHA := fs.String("head-sha", "", "exact current pull request head SHA")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registryPath == "" || *repo == "" || *pr < 1 || strings.TrimSpace(*headSHA) == "" {
		return fmt.Errorf("--registry, --repo, --pr, and --head-sha are required")
	}
	reg, err := cigates.Load(*registryPath)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	contexts := make([]string, 0, len(reg.RequiredStatusChecks))
	for _, check := range reg.RequiredStatusChecks {
		contexts = append(contexts, check.Context)
	}
	verdict, err := probeReuse(context.Background(), execGHRunner{}, *repo, *pr, *headSHA, contexts)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "reuse=%v\nsource_sha=%s\ntree=%s\nreason=%s\n",
		verdict.Reuse, verdict.SourceSHA, verdict.Tree, verdict.Reason)
	return nil
}

var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// probeReuse reports whether headSHA's tree was already proven green by a
// previous head of the same pull request. Candidates, in order: the head a
// force-push replaced (GraphQL beforeCommit, only when that push produced
// exactly headSHA) and headSHA's first parent (an empty or amend-only commit
// pushed without force). Tree equality is necessary but not sufficient:
// every required context must report success on the candidate, from a check
// run, a commit status, or both -- and any source reporting anything else
// (failure, cancelled, pending, missing) fails the candidate closed.
func probeReuse(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, contexts []string) (reuseVerdict, error) {
	if !repoNamePattern.MatchString(repo) {
		return reuseVerdict{}, fmt.Errorf("invalid --repo %q", repo)
	}
	if !commitSHAPattern.MatchString(headSHA) {
		return reuseVerdict{}, fmt.Errorf("invalid --head-sha %q", headSHA)
	}
	// Zero required contexts would make the green check vacuous: a hermetic
	// registry may legitimately declare none, but reuse with no evidence is
	// exactly the fail-open this probe must never return.
	if len(contexts) == 0 {
		return reuseVerdict{}, fmt.Errorf("no required status contexts to reuse against")
	}
	head, err := gitCommit(ctx, runner, repo, headSHA)
	if err != nil {
		return reuseVerdict{}, err
	}
	candidates, err := reuseCandidates(ctx, runner, repo, pr, headSHA, head)
	if err != nil {
		return reuseVerdict{}, err
	}
	verdict := reuseVerdict{Tree: head.Tree.SHA, Reason: "no previous head with an identical tree and a full green verdict"}
	for _, candidate := range candidates {
		if candidate == headSHA || !commitSHAPattern.MatchString(candidate) {
			continue
		}
		commit, err := gitCommit(ctx, runner, repo, candidate)
		if err != nil {
			return reuseVerdict{}, err
		}
		if commit.Tree.SHA != head.Tree.SHA {
			continue
		}
		green, err := requiredContextsGreen(ctx, runner, repo, candidate, contexts)
		if err != nil {
			return reuseVerdict{}, err
		}
		if green {
			return reuseVerdict{
				Reuse:     true,
				SourceSHA: candidate,
				Tree:      head.Tree.SHA,
				Reason:    "identical tree already green on every required context",
			}, nil
		}
	}
	return verdict, nil
}

type gitCommitResponse struct {
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
}

func gitCommit(ctx context.Context, runner ghRunner, repo, sha string) (gitCommitResponse, error) {
	out, err := runner.Run(ctx, "api", "repos/"+repo+"/git/commits/"+sha)
	if err != nil {
		return gitCommitResponse{}, fmt.Errorf("read commit %s: %w", sha, err)
	}
	var commit gitCommitResponse
	if err := json.Unmarshal(out, &commit); err != nil {
		return gitCommitResponse{}, fmt.Errorf("parse commit %s: %w", sha, err)
	}
	if commit.Tree.SHA == "" {
		return gitCommitResponse{}, fmt.Errorf("commit %s carries no tree", sha)
	}
	return commit, nil
}

// reuseCandidates lists prior heads worth probing: the force-pushed-away
// head when the latest force-push produced headSHA, then the first parent.
func reuseCandidates(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string, head gitCommitResponse) ([]string, error) {
	var candidates []string
	before, err := forcePushBefore(ctx, runner, repo, pr, headSHA)
	if err != nil {
		return nil, err
	}
	if before != "" {
		candidates = append(candidates, before)
	}
	if len(head.Parents) > 0 {
		candidates = append(candidates, head.Parents[0].SHA)
	}
	return candidates, nil
}

type forcePushResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				TimelineItems struct {
					Nodes []struct {
						BeforeCommit struct {
							OID string `json:"oid"`
						} `json:"beforeCommit"`
						AfterCommit struct {
							OID string `json:"oid"`
						} `json:"afterCommit"`
					} `json:"nodes"`
				} `json:"timelineItems"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

// forcePushBefore returns the head replaced by the most recent force-push,
// or "" when there is none or it did not produce headSHA.
func forcePushBefore(ctx context.Context, runner ghRunner, repo string, pr int, headSHA string) (string, error) {
	owner, name, _ := strings.Cut(repo, "/")
	query := fmt.Sprintf(`{repository(owner:%q,name:%q){pullRequest(number:%d){timelineItems(itemTypes:[HEAD_REF_FORCE_PUSHED_EVENT],last:1){nodes{... on HeadRefForcePushedEvent{beforeCommit{oid} afterCommit{oid}}}}}}}`, owner, name, pr)
	out, err := runner.Run(ctx, "api", "graphql", "-f", "query="+query)
	if err != nil {
		return "", fmt.Errorf("read force-push history: %w", err)
	}
	var response forcePushResponse
	if err := json.Unmarshal(out, &response); err != nil {
		return "", fmt.Errorf("parse force-push history: %w", err)
	}
	nodes := response.Data.Repository.PullRequest.TimelineItems.Nodes
	if len(nodes) == 0 || nodes[0].AfterCommit.OID != headSHA {
		return "", nil
	}
	return nodes[0].BeforeCommit.OID, nil
}

// requiredContextsGreen requires every context to be reported successful by
// at least one of its sources on the candidate commit, and by none of them
// as anything else.
func requiredContextsGreen(ctx context.Context, runner ghRunner, repo, sha string, contexts []string) (bool, error) {
	statuses, err := commitStatuses(ctx, runner, repo, sha)
	if err != nil {
		return false, err
	}
	for _, name := range contexts {
		reported := false
		run, found, err := latestCheckRun(ctx, runner, repo, sha, name)
		if err != nil {
			return false, err
		}
		if found {
			reported = true
			if run != "success" {
				return false, nil
			}
		}
		if state, ok := statuses[name]; ok {
			reported = true
			if state != "success" {
				return false, nil
			}
		}
		if !reported {
			return false, nil
		}
	}
	return true, nil
}

type checkRunsResponse struct {
	CheckRuns []struct {
		ID         int64  `json:"id"`
		Conclusion string `json:"conclusion"`
	} `json:"check_runs"`
}

// latestCheckRun returns the conclusion of the highest-id (newest) check run
// with the given name on the commit.
func latestCheckRun(ctx context.Context, runner ghRunner, repo, sha, name string) (string, bool, error) {
	endpoint := "repos/" + repo + "/commits/" + sha + "/check-runs?check_name=" + url.QueryEscape(name) + "&per_page=100"
	out, err := runner.Run(ctx, "api", endpoint)
	if err != nil {
		return "", false, fmt.Errorf("read check runs for %q: %w", name, err)
	}
	var response checkRunsResponse
	if err := json.Unmarshal(out, &response); err != nil {
		return "", false, fmt.Errorf("parse check runs for %q: %w", name, err)
	}
	conclusion := ""
	var latest int64 = -1
	for _, run := range response.CheckRuns {
		if run.ID > latest {
			latest = run.ID
			conclusion = run.Conclusion
		}
	}
	return conclusion, latest >= 0, nil
}

type commitStatusEntry struct {
	Context string `json:"context"`
	State   string `json:"state"`
}

// commitStatuses maps each status context to its newest state. The endpoint
// returns newest first, so the first entry per context wins.
func commitStatuses(ctx context.Context, runner ghRunner, repo, sha string) (map[string]string, error) {
	out, err := runner.Run(ctx, "api", "repos/"+repo+"/commits/"+sha+"/statuses?per_page=100")
	if err != nil {
		return nil, fmt.Errorf("read commit statuses: %w", err)
	}
	var entries []commitStatusEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parse commit statuses: %w", err)
	}
	latest := make(map[string]string, len(entries))
	for _, entry := range entries {
		if _, seen := latest[entry.Context]; !seen {
			latest[entry.Context] = entry.State
		}
	}
	return latest, nil
}
