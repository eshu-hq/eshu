---
name: resolve-review-threads
description: |
  Resolve unresolved GitHub PR review threads after their referenced code has
  been fixed in the latest commit. Use this skill right after pushing changes
  that address reviewer feedback on an Eshu PR, so threads do not linger open
  on the conversation tab. The skill takes a PR number, classifies each
  unresolved thread as `fixed`, `unchanged`, or `ambiguous` against the
  current HEAD, and only auto-resolves the `fixed` ones. The rest stay open
  with a structured report. Treats every reviewer uniformly — codex,
  GitHub Copilot, Claude (when present), and human reviewers — by reading the
  comment body and checking the cited file:line, not by trusting the bot
  label.
---

# Resolve Review Threads

Use this skill after pushing fixes that address PR review comments. The skill
is the close-out step: it calls the GitHub GraphQL `resolveReviewThread`
mutation for threads whose underlying code is now fixed, and leaves threads
open when the fix is missing, partial, or unclear. The goal is to keep the PR
conversation tab honest so the human reviewer is not chasing already-fixed
threads.

This is a project skill — narrower and stricter than the global
`post-merge:resolve-review` or `resolve-review` skills. Those skills focus on
authoring reply text. This skill focuses on the resolution mutation and the
classification rules around it.

## When To Use

- Right after `git push` for a fix that addresses one or more review comments.
- When a PR shows many open threads on the conversation tab but the agent
  knows recent commits addressed several of them.
- **Load this skill proactively when opening a PR.** Bot and human review
  comments often arrive within minutes of opening; the classification
  rules and the "MUST NOT auto-resolve unchanged" rule affect how the
  agent should respond to the first P2. Loading the skill after the
  comments arrive is too late — the agent will have already half-applied
  the wrong response.

## Reviewers That Show Up On Eshu PRs

The PR conversation tab accumulates review threads from several sources.
Classify every thread by reading the comment body and the cited file:line;
**never trust the bot label alone** to decide whether a finding is correct.

| Reviewer | Author handle | Notes |
| --- | --- | --- |
| Codex | `chatgpt-codex-connector[bot]` | Most common bot reviewer. Posts severity-tagged P0/P1/P2 findings with a `file:line` cite and a one-line rule reference. The P2s are the bulk; most are real, some are out-of-scope. |
| GitHub Copilot | `github-copilot[bot]` | Posts inline comments on specific lines, often without a severity tag. Sometimes duplicates codex findings; sometimes catches different surface issues (typos, missing error checks, doc gaps). |
| Claude (when tokens are available) | `claude[bot]` or a harness-specific handle | Posts summary reviews and inline comments. Tends to focus on architectural concerns and security/secret leakage. |
| Human reviewers | a real GitHub user | Authoritative. Treats their findings as P0/P1 unless the author can prove they are out of scope. |
| Eshu post-discord-invite | `github-actions[bot]` | Not a review; a Discord link post. Ignore. |
| Cloudflare Pages / Build & Release bots | `cloudflare-workers-and-pages[bot]`, `github-actions[bot]` | Not reviews; deployment status. Ignore. |

**Severity framing is the same regardless of author.** A Copilot comment
that says "this could be a nil pointer" and a codex P2 that says "missing
nil check" are the same finding, classified the same way, and resolved
the same way. Do not let the bot's identity change the classification.

**Duplicate findings are common.** When codex and Copilot both flag the
same line, resolve both threads but only fix the underlying code once.
When they disagree (codex says "must add a marker", Copilot says "the
row is fine"), trust the code and the project rules over either bot.
- When a reviewer asks "are the open threads still real?" — this skill answers
  with evidence.

Do not use this skill to write reply text. Do not use it to auto-resolve a
thread on a fix the agent has not actually verified.

## Prerequisites

- `gh` is authenticated and can call `gh api graphql` on the repo.
- The current working tree is the PR's head branch, or the PR's head commit
  SHA is reachable locally so file-state lookups are accurate.
- The repo `owner` and `name` are known. Use `gh repo view --json owner,name`
  when unsure.

If any prerequisite fails, stop and report — do not guess.

## Workflow

### 1. Resolve PR Metadata

```bash
gh pr view <pr-number> --json url,number,headRefOid,headRefName,baseRefName,state
```

Capture:

- `headRefOid` — the SHA HEAD will be compared against.
- `headRefName` — the branch the PR is built from.
- `state` — refuse to run when the PR is `MERGED` or `CLOSED`; threads on a
  closed PR are not actionable.

### 2. List Unresolved Threads

```graphql
query($owner:String!,$repo:String!,$num:Int!) {
  repository(owner:$owner,name:$repo) {
    pullRequest(number:$num) {
      reviewThreads(first:100) {
        nodes {
          id
          isResolved
          isOutdated
          path
          line
          originalLine
          comments(first:1) {
            nodes { author{login} body path line originalLine }
          }
        }
      }
    }
  }
}
```

Run with `gh api graphql -F owner=... -F repo=... -F num=... -f query='...'`.
Filter to `isResolved == false`. Keep `isOutdated == true` in the working set
— the anchored line moved, but the concern usually still stands.

If the PR has more than 100 review threads, paginate with `pageInfo` before
classifying anything. Partial classification on a truncated list is worse than
leaving the PR alone.

### 3. Classify Each Thread

For each unresolved thread, decide one of three labels.

| Label | Meaning | Action |
| --- | --- | --- |
| `fixed` | The cited file:line has changed in a way that plausibly addresses the comment. | Call `resolveReviewThread`. |
| `unchanged` | The cited file:line is byte-identical to what the reviewer saw. | Leave open. Report. |
| `ambiguous` | The file changed near the cited line, but the agent cannot prove the change addresses the comment. | Leave open. Report. |

Classification inputs:

- Read the comment body fully. Note what it asks for (rename, extract,
  guardrail, doc, test, telemetry, etc.).
- Read the current state of `path` around `line` (or `originalLine` when
  `line` is null). A small window — 20 lines above and below — is enough.
- Compare against history: `git log -p --follow -- <path>` from the comment's
  commit forward, or `git diff <comment-commit>..HEAD -- <path>`.
- If the file no longer exists at HEAD, that is `fixed` only when the comment
  asked for deletion or a move; otherwise it is `ambiguous`.
- If the comment asks for a test, a docs change, or telemetry, the cited file
  may be untouched but the fix may live elsewhere. Default to `ambiguous` in
  that case and report; do not auto-resolve.

Err on the side of `ambiguous`. A false-positive auto-resolve is worse than
leaving an honest thread open.

### 4. Resolve `fixed` Threads

```bash
gh api graphql \
  -F threadId="$THREAD_ID" \
  -f query='mutation($threadId:ID!){resolveReviewThread(input:{threadId:$threadId}){thread{isResolved}}}'
```

Confirm the response shows `isResolved: true` before counting the thread as
resolved. A 200 with `isResolved: false` means the mutation silently failed
(permissions, stale ID, race with the reviewer) — report it, do not retry in
a loop.

### 5. Report

Print a short, structured summary:

- threads scanned (count)
- threads already resolved (count, skipped)
- threads classified `fixed` (count, list of `threadId` plus `path:line` plus
  one-line reason)
- threads classified `unchanged` (list of `threadId` plus `path:line` plus
  first line of comment body)
- threads classified `ambiguous` (list of `threadId` plus `path:line` plus
  first line of comment body plus what the agent saw that prevented a
  confident call)
- before/after unresolved counts

Lead with the punchline ("Resolved 4 of 9 open threads; 5 still open"), then
the lists. Do not bury the answer.

## Operating Rules

- MUST NOT auto-resolve a thread the agent has not classified as `fixed`
  using evidence from the current HEAD.
- MUST treat outdated threads (`isOutdated == true`) as still in scope; the
  comment concern usually outlives a line-number shift.
- MUST stop and report when the GraphQL list is truncated; do not classify a
  partial set.
- MUST NOT loop-retry a failed `resolveReviewThread` mutation. One attempt,
  then report the failure with the thread ID and the GraphQL response body.
- MUST NOT post reply text from this skill. Use `resolve-review` or
  `post-merge:resolve-review` for reply authoring.
- MUST NOT push commits or amend the working tree from this skill. The fix
  commits already happened; this skill only mutates GitHub state.
- MUST NOT add AI attribution anywhere — no co-author trailers, no
  "generated by" notes in the report.
- MUST keep the report under 40 lines for a typical PR; truncate long comment
  bodies to their first line plus an ellipsis.

## Classification Examples

- Comment says "rename `foo` to `bar` for clarity"; HEAD shows the symbol is
  now `bar` at the same path. -> `fixed`.
- Comment says "this needs a test"; cited file is unchanged at HEAD, but a
  new `_test.go` exists in the same package covering the symbol. ->
  `ambiguous` (report it; the human can verify the new test matches the
  intent).
- Comment says "guard against nil"; cited line is byte-identical at HEAD and
  no nil-check is visible in the same function. -> `unchanged`.
- Comment says "extract this into a helper"; cited function was deleted and
  the logic moved to a new file. -> `ambiguous` unless the new helper is
  named in a way that clearly matches the comment.

## Failure Modes

| Failure | What to do |
| --- | --- |
| `gh` not authenticated | Stop. Print the `gh auth status` output verbatim. |
| PR is closed or merged | Stop. Print the PR state and exit. |
| GraphQL list truncated past 100 | Stop. Print the page count and exit; do not classify a partial set. |
| Mutation returns `isResolved: false` | Report the thread ID and response body. Do not retry. |
| File at `path` not found at HEAD | Classify `ambiguous` unless the comment asked for deletion or a move. |
| Comment body empty or bot-generated | Classify `unchanged` unless a clear automated rule applies. |

## Related Skills

- `resolve-review` (global) — authoring reply text on review threads.
- `post-merge:resolve-review` (plugin) — broader close-out flow after a PR
  merges, including filing follow-up issues.
- `pr-review-toolkit:review-pr` (plugin) — review authoring, not resolution.

This skill is the narrow project-owned counterpart for the resolution-only
step. Run it after the others, not instead of them.
