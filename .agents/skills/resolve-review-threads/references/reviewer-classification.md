# Reviewer Classification Reference

Detail for `resolve-review-threads`: the reviewers that show up on Eshu PRs,
the full review-thread query, worked classification examples, and failure
modes. Load this once the main workflow needs one of these specifics.

## Reviewers That Show Up On Eshu PRs

Classify every thread by reading the comment body and the cited `file:line`;
never trust the bot label alone to decide whether a finding is correct.

| Reviewer | Author handle | Notes |
| --- | --- | --- |
| Codex | `chatgpt-codex-connector[bot]` | Most common bot reviewer. Posts severity-tagged P0/P1/P2 findings with a `file:line` cite and a one-line rule reference. The P2s are the bulk; most are real, some are out-of-scope. |
| GitHub Copilot | `github-copilot[bot]` | Posts inline comments on specific lines, often without a severity tag. Sometimes duplicates codex findings; sometimes catches different surface issues (typos, missing error checks, doc gaps). |
| Claude (when tokens are available) | `claude[bot]` or a harness-specific handle | Posts summary reviews and inline comments. Tends to focus on architectural concerns and security/secret leakage. |
| Human reviewers | a real GitHub user | Authoritative. Treat their findings as P0/P1 unless the author can prove they are out of scope. |
| Eshu post-discord-invite | `github-actions[bot]` | Not a review; a Discord link post. Ignore. |
| Cloudflare Pages / Build & Release bots | `cloudflare-workers-and-pages[bot]`, `github-actions[bot]` | Not reviews; deployment status. Ignore. |

Severity framing is the same regardless of author: a Copilot comment saying
"this could be a nil pointer" and a codex P2 saying "missing nil check" are the
same finding, classified and resolved the same way.

Duplicate findings are common. When codex and Copilot both flag the same line,
resolve both threads but fix the underlying code once. When they disagree
(codex says "must add a marker", Copilot says "the row is fine"), trust the
code and the project rules over either bot.

## Full Review-Thread Query

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
If the PR has more than 100 review threads, paginate with `pageInfo` before
classifying anything — a partial classification is worse than leaving the PR
alone.

## Classification Examples

- Comment says "rename `foo` to `bar` for clarity"; HEAD shows the symbol is
  now `bar` at the same path. -> `fixed`.
- Comment says "this needs a test"; cited file is unchanged at HEAD, but a new
  `_test.go` exists in the same package covering the symbol. -> `ambiguous`
  (report it; the human can verify the new test matches the intent).
- Comment says "guard against nil"; cited line is byte-identical at HEAD and no
  nil-check is visible in the same function. -> `unchanged`.
- Comment says "extract this into a helper"; cited function was deleted and the
  logic moved to a new file. -> `ambiguous` unless the new helper is named in a
  way that clearly matches the comment.

## Failure Modes

| Failure | What to do |
| --- | --- |
| `gh` not authenticated | Use a GitHub connector fallback if available and report it; otherwise stop and print `gh auth status` verbatim. |
| PR is closed or merged | Stop. Print the PR state and exit. |
| GraphQL list truncated past 100 | Stop. Print the page count and exit; do not classify a partial set. |
| Mutation returns `isResolved: false` | Report the thread ID and response body. Do not retry. |
| File at `path` not found at HEAD | Classify `ambiguous` unless the comment asked for deletion or a move. |
| Comment body empty or bot-generated | Classify `unchanged` unless a clear automated rule applies. |
