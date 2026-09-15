---
name: resolve-review-threads
description: Resolve GitHub PR review threads (Codex, Copilot, Claude, human) via the resolveReviewThread mutation once each finding is verified fixed at current HEAD; classify fixed/unchanged/ambiguous and report counts. Not the review verdict itself (eshu-code-review) and not reply-text authoring or driving an issue to merged (eshu-issue-driver).
---

# Resolve Review Threads

Use right after a `git push` that addresses PR review comments, or when a PR
shows open threads the agent knows recent commits fixed. Load this skill
proactively when opening a PR — bot reviews often land within minutes, and
loading the classification rules after the first P2 arrives is too late.

This is the close-out step: it calls the GitHub GraphQL `resolveReviewThread`
mutation for threads whose underlying code is now fixed, and leaves threads
open when the fix is missing, partial, or unclear. It does not author reply
text (`resolve-review` or `post-merge:resolve-review` do that) and it does not
drive a PR to merge (`eshu-issue-driver` does).

Classify every thread by reading the comment body and the cited `file:line` —
never trust the bot label alone. See
[reviewer-classification.md](references/reviewer-classification.md) for the
reviewers that show up on Eshu PRs, the full GraphQL query, worked
classification examples, and the failure-mode table.

## Prerequisites

- `gh` is authenticated and can call `gh api graphql`, or the harness exposes
  an authenticated GitHub connector with equivalent PR-metadata and
  review-thread operations.
- The working tree is the PR's head branch, or its head SHA is reachable
  locally, so file-state lookups are accurate.
- The repo `owner`/`name` are known (`gh repo view --json owner,name` if not).

Stop and report if any prerequisite fails — do not guess.

## Workflow

1. **Resolve PR metadata.**

   ```bash
   gh pr view <pr-number> --json url,number,headRefOid,headRefName,baseRefName,state
   ```

   Capture `headRefOid` (the SHA file-state checks compare against) and
   `state`; refuse to run on a `MERGED` or `CLOSED` PR.

2. **List unresolved threads.** Query `reviewThreads(first:100)` for `id`,
   `isResolved`, `isOutdated`, `path`, `line`/`originalLine`, and the first
   comment's author and body. Filter to `isResolved == false`; keep
   `isOutdated == true` in scope — the concern usually outlives a line-number
   shift. Paginate before classifying anything if there are more than 100
   threads.

3. **Classify each thread:**

   | Label | Meaning | Action |
   | --- | --- | --- |
   | `fixed` | Cited `file:line` changed in a way that plausibly addresses the comment. | Resolve it. |
   | `unchanged` | Cited `file:line` is byte-identical to what the reviewer saw. | Leave open, report. |
   | `ambiguous` | Nearby code changed but the fix cannot be proven to address the comment. | Leave open, report. |

   Read the comment fully, inspect ~20 lines around `line` at HEAD, and check
   history (`git log -p --follow` or `git diff <comment-commit>..HEAD`). A
   deleted file is `fixed` only if the comment asked for deletion or a move. A
   test/docs/telemetry request with the cited file untouched defaults to
   `ambiguous` — do not auto-resolve on inferred coverage elsewhere. Err toward
   `ambiguous`: a false-positive resolve is worse than an honest open thread.

4. **Resolve `fixed` threads.**

   ```bash
   gh api graphql -F threadId="$THREAD_ID" \
     -f query='mutation($threadId:ID!){resolveReviewThread(input:{threadId:$threadId}){thread{isResolved}}}'
   ```

   Confirm `isResolved: true` in the response before counting it resolved. A
   200 with `isResolved: false` is a silent mutation failure (permissions,
   stale ID, race with the reviewer) — report it, do not retry in a loop.

5. **Report**, leading with the punchline (e.g. "Resolved 4 of 9 open
   threads; 5 still open"):

   - threads scanned, and how many were already resolved (skipped)
   - `fixed` threads: threadId, path:line, one-line reason
   - `unchanged`/`ambiguous` threads: threadId, path:line, first line of the
     comment, and — for ambiguous — what blocked a confident call
   - before/after unresolved counts

## Operating rules

- MUST NOT auto-resolve a thread not classified `fixed` from current-HEAD
  evidence.
- MUST treat outdated threads as still in scope.
- MUST stop and report on a truncated thread list rather than classify a
  partial set.
- MUST NOT loop-retry a failed `resolveReviewThread` mutation — one attempt,
  then report the thread ID and response body.
- MUST NOT author reply text, and MUST NOT push commits or amend the working
  tree, from this skill.
- MUST NOT add AI attribution anywhere.
- MUST keep the report under 40 lines for a typical PR; truncate comment
  bodies to their first line plus an ellipsis.

## Related skills

- `resolve-review` (global) and `post-merge:resolve-review` (plugin) — reply-text
  authoring, not resolution.
- `eshu-code-review` — produces the verdict this skill acts after, never before.
- `eshu-issue-driver` — drives the PR to merged; this skill is one step inside
  that drive, not a substitute for it.
