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
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// reuseVerdict is the probe's answer: whether a prior head of the same pull
// request already proved this exact tree green on every required context.
type reuseVerdict struct {
	Reuse       bool
	SourceSHA   string
	Tree        string
	MergeSHA    string
	MergeTree   string
	FullRootSHA string
	Reason      string
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
	// Bound the probe like queue-select (2m): it sits on a workflow step's
	// critical path, and a hung gh call must fail fast to "no reuse",
	// never stall the caller to its job timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if runID, attempt, identityErr := nativeRunAttempt(); identityErr == nil {
		if input, bindErr := readTestedInput(ctx, execGHRunner{}, *repo, *pr, *headSHA, runID, attempt); bindErr == nil {
			ctx = withTestedInput(ctx, input)
		}
	}
	paths, truncated, err := changedPathsForPR(ctx, execGHRunner{}, *repo, *pr)
	if err != nil {
		return err
	}
	var verdict reuseVerdict
	if transferPRPathsSafe(paths, truncated) {
		verdict, err = probeReuseForAwait(ctx, execGHRunner{}, reg, *repo, *pr, *headSHA)
	} else {
		verdict.Reason = "PR changes gate definitions or changed-file listing is incomplete"
	}
	if err != nil {
		return err
	}
	input, _ := currentTestedInput(ctx)
	_, _ = fmt.Fprintf(os.Stdout, "reuse=%v\nsource_sha=%s\nsource_full_sha=%s\ntree=%s\ntarget_run_id=%d\ntarget_run_attempt=%d\nmerge_sha=%s\nmerge_tree=%s\nreason=%s\n",
		verdict.Reuse, verdict.SourceSHA, verdict.FullRootSHA, verdict.Tree, input.RunID, input.Attempt, input.MergeSHA, input.MergeTree, verdict.Reason)
	return nil
}

// probeReuseForAwait adapts the registry to the probe's context list for
// the await path, sharing runReuse's single source of truth.
func probeReuseForAwait(ctx context.Context, runner ghRunner, reg *cigates.Registry, repo string, pr int, headSHA string) (reuseVerdict, error) {
	return probeReuse(ctx, runner, repo, pr, headSHA, transferProofContexts(reg))
}

// transferProofContexts lists the contexts a previous head must show green
// before its verdict transfers to a new head (reuse, smoke tier, pr-plan).
//
// It deliberately returns EVERY manifest context, including entries with
// ruleset_required: false (#9094). The live ruleset stopped requiring
// required-gates-complete to merge, but this is a different question: the
// transfer paths judge a PREVIOUS head, and the aggregate's terminal status
// description is the proof they transfer (fullBackedSource). Dropping it would
// let a head with a red or proof-less aggregate donate its verdict. Merge-time
// consumers read the ruleset subset instead (ci-gates contexts --ruleset-only).
func transferProofContexts(reg *cigates.Registry) []string {
	contexts := make([]string, 0, len(reg.RequiredStatusChecks))
	for _, check := range reg.RequiredStatusChecks {
		contexts = append(contexts, check.Context)
	}
	return contexts
}

var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// validRepoName also rejects dot segments: the character class alone
// accepts "../x", which would interpolate into REST paths as traversal.
func validRepoName(repo string) bool {
	if !repoNamePattern.MatchString(repo) {
		return false
	}
	owner, name, _ := strings.Cut(repo, "/")
	for _, segment := range []string{owner, name} {
		if segment == "." || segment == ".." || strings.Contains(segment, "..") {
			return false
		}
	}
	return true
}

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
	if !validRepoName(repo) {
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
	target, bound := currentTestedInput(ctx)
	if !bound {
		verdict.Reason = "target Build Test input is not bound to an exact run and attempt"
		return verdict, nil
	}
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
			proof, source, fullBacked, err := fullBackedSource(ctx, runner, repo, pr, candidate, contexts)
			if err != nil {
				return reuseVerdict{}, err
			}
			if !fullBacked || source.MergeTree != target.MergeTree {
				continue
			}
			messagesEqual, err := sameReuseMessages(ctx, runner, repo, source, target)
			if err != nil {
				return reuseVerdict{}, err
			}
			if !messagesEqual {
				continue
			}
			return reuseVerdict{
				Reuse:       true,
				SourceSHA:   candidate,
				Tree:        head.Tree.SHA,
				MergeSHA:    target.MergeSHA,
				MergeTree:   target.MergeTree,
				FullRootSHA: proof.FullRootSHA,
				Reason:      "identical head and tested merge trees have a v3 full-backed verdict",
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
	if !commitSHAPattern.MatchString(commit.Tree.SHA) {
		return gitCommitResponse{}, fmt.Errorf("commit %s carries no valid tree SHA", sha)
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
// with the given name on the commit. It paginates: the max-id rule is only
// sound over the full run set, and a much-rerun head can exceed one page.
func latestCheckRun(ctx context.Context, runner ghRunner, repo, sha, name string) (string, bool, error) {
	endpoint := "repos/" + repo + "/commits/" + sha + "/check-runs?check_name=" + url.QueryEscape(name) + "&per_page=100"
	out, err := runner.Run(ctx, "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return "", false, fmt.Errorf("read check runs for %q: %w", name, err)
	}
	var pages []checkRunsResponse
	if err := json.Unmarshal(out, &pages); err != nil {
		return "", false, fmt.Errorf("parse check runs for %q: %w", name, err)
	}
	conclusion := ""
	var latest int64 = -1
	for _, page := range pages {
		for _, run := range page.CheckRuns {
			if run.ID > latest {
				latest = run.ID
				conclusion = run.Conclusion
			}
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
