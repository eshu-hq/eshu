// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// maxCheckPages bounds the checks pagination so a misbehaving response cannot
// loop forever. 30 pages of 100 is far above any real check set.
const maxCheckPages = 30

// pullRequestQuery reads everything one poll needs in a single GraphQL call.
// Threads, reviews, and comments use last:100 so a burst always includes the
// newest items. Checks page through contexts(first:100, after:$after): the
// first page alone hides a pending or failed check past the hundredth.
const pullRequestQuery = `query($owner:String!,$repo:String!,$number:Int!,$after:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      number state mergedAt mergeCommit{oid}
      headRefName headRefOid baseRefName baseRef{target{oid}}
      mergeable mergeStateStatus
      reviewThreads(last:100){nodes{id isResolved path line comments(first:1){nodes{id author{login} body url}}}}
      reviews(last:100){nodes{id author{login} state body url}}
      comments(last:100){nodes{id author{login} body url}}
      commits(last:1){nodes{commit{
        oid messageHeadline author{user{login}}
        statusCheckRollup{state contexts(first:100,after:$after){
          totalCount pageInfo{hasNextPage endCursor}
          nodes{__typename
            ... on CheckRun{databaseId name status conclusion detailsUrl}
            ... on StatusContext{context state targetUrl}}}}
      }}}
    }
  }
}`

// ghEnv builds the environment for a gh child process: the parent environment
// without any ambient token, plus the pinned token and non-interactive flags.
// An empty token leaves GH_TOKEN unset so gh falls back to its keyring, which
// is what `gh auth token --user` needs.
func ghEnv(environ []string, token string) []string {
	env := make([]string, 0, len(environ)+3)
	for _, kv := range environ {
		if strings.HasPrefix(kv, "GH_TOKEN=") || strings.HasPrefix(kv, "GITHUB_TOKEN=") ||
			strings.HasPrefix(kv, "GH_PROMPT_DISABLED=") || strings.HasPrefix(kv, "NO_COLOR=") {
			continue
		}
		env = append(env, kv)
	}
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	return append(env, "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
}

// token resolves the pinned account's token from the gh keyring. It runs on
// every poll so a keyring change or re-login is picked up, and it runs with
// no ambient token so the answer is the keyring's, not a leaked variable's.
func (w *Watcher) token(ctx context.Context) (string, error) {
	out, err := w.exec(ctx, Command{
		Name: w.cfg.GH,
		Args: []string{"auth", "token", "--user", w.cfg.Account},
		Env:  ghEnv(w.cfg.Environ, ""),
		Dir:  w.cfg.Dir,
	})
	if err != nil {
		return "", fmt.Errorf("resolve token for account %q: %w", w.cfg.Account, err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("gh returned an empty token for account %q", w.cfg.Account)
	}
	return tok, nil
}

// gh runs one gh command pinned to the token.
func (w *Watcher) gh(ctx context.Context, token string, args ...string) ([]byte, error) {
	return w.exec(ctx, Command{Name: w.cfg.GH, Args: args, Dir: w.cfg.Dir, Env: ghEnv(w.cfg.Environ, token)})
}

// fetchSnapshot reads the current state of one PR, following the checks
// cursor until every check is read.
func (w *Watcher) fetchSnapshot(ctx context.Context, token string, number int) (Snapshot, error) {
	owner, repo, ok := strings.Cut(w.cfg.Repo, "/")
	if !ok {
		return Snapshot{}, fmt.Errorf("repo %q is not owner/name", w.cfg.Repo)
	}
	var snap Snapshot
	after := ""
	for page := 0; page < maxCheckPages; page++ {
		args := []string{
			"api", "graphql", "-f", "query=" + pullRequestQuery,
			"-f", "owner=" + owner, "-f", "repo=" + repo, "-F", "number=" + strconv.Itoa(number),
		}
		if after != "" {
			args = append(args, "-f", "after="+after)
		}
		raw, err := w.gh(ctx, token, args...)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read pr %d: %w", number, err)
		}
		if page == 0 {
			first, next, perr := parseResponse(raw)
			if perr != nil {
				return Snapshot{}, fmt.Errorf("read pr %d: %w", number, perr)
			}
			snap = first
			if !next.HasNextPage {
				return snap, nil
			}
			after = next.EndCursor
			continue
		}
		more, next, perr := parseChecksPage(raw)
		if perr != nil {
			return Snapshot{}, fmt.Errorf("read pr %d checks page %d: %w", number, page+1, perr)
		}
		snap.Checks = append(snap.Checks, more...)
		if !next.HasNextPage {
			return snap, nil
		}
		after = next.EndCursor
	}
	return snap, fmt.Errorf("pr %d has more than %d pages of checks", number, maxCheckPages)
}

var jobURL = regexp.MustCompile(`/actions/runs/\d+/job/(\d+)`)

// jobID extracts the Actions job ID from a check's details URL. It reports
// false for checks that are not Actions jobs.
func jobID(detailsURL string) (string, bool) {
	m := jobURL.FindStringSubmatch(detailsURL)
	if m == nil {
		return "", false
	}
	return m[1], true
}
