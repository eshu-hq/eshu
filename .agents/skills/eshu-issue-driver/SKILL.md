---
name: eshu-issue-driver
description: |
  Drive one or more Eshu GitHub issues (including epics) to a verifiable closed
  state — merged PRs, a severity-tagged review gate, and
  resolution of every bot and human review (codex, GitHub Copilot, Cursor,
  Claude, and human reviewers, treated uniformly). ACTIVATE when the user says
  "drive issue(s)", "work issue(s) to closed/done", "close out issue/epic", or
  "finish #NNNN until merged", or sets a /goal referencing this skill. Pass one
  or more issue numbers or URLs as args; epics are expanded to their children
  automatically. Scoped to this repository (eshu-hq/eshu) and routes only to the
  Eshu project skills under .agents/skills/.
---

# eshu-issue-driver

Drives a set of Eshu GitHub issues to a verifiable closed state with a
severity-tagged review gate. Designed to run under `/goal` so it loops turns
until the proof clauses in the DONE section all hold.

## Inputs

- **Issues**: one or more issue numbers or full URLs, from skill args or the
  active `/goal` line. Required — if none are provided, stop and ask. Never
  assume issue numbers.
- **Repo**: this repository, `eshu-hq/eshu`. (This skill is repo-owned; it does
  not drive other repos.)
- **gh auth**: ensure `gh` is authenticated to an account that can push to the
  repo and open PRs before any push/PR step. Do not hard-code an account; use
  whatever the local setup requires (switch with `gh auth switch` if needed).
  If `gh` auth is broken but the active harness exposes a GitHub connector with
  equivalent PR/issue/review-thread operations, use that connector as an
  explicit fallback and report the fallback in the proof notes.
- **fresh base**: before opening or updating a PR, `git fetch origin`, rebase on
  `origin/main`, and rerun the focused proof affected by the rebase. Then follow
  the complete Steps 5-7 promotion sequence: preliminary full
  `eshu-code-review` with `P0=0, P1=0, P2=0`, one late `make pre-pr`, final
  full review of the exact post-preflight diff, and only then push. Never push
  directly after a rebase. Use `--force-with-lease` when the reviewed rebase
  rewrites an already-pushed branch. Do not create or update a PR from a branch
  that is knowingly behind main or locally conflicted.

## How to run it (composition with /goal)

This skill is doctrine only — it does not loop by itself. Pair it with `/goal`:

```
/goal Drive issues <list> to fully closed per the eshu-issue-driver skill —
load the eshu-issue-driver skill now and follow it. Not done until every proof
clause in that skill's DONE section is pasted and clean. Stop after 50 turns if
blocked only on operator-side action (say so).
```

The `/goal` evaluator reads the conversation, which includes this loaded skill,
so "done per the skill" is checkable. Run with auto mode on so each turn runs
unattended. While a PR is open, poll conflicts, CI, and review comments about
every 60 seconds; do not only wait for the check rollup.

## Step 1 — Build the work set (expand epics)

For each input issue:

1. `gh issue view <n> --repo eshu-hq/eshu --json title,body,labels,state`.
2. Detect epic if ANY: an `epic`/`tracking` label, a task list of child refs
   (`- [ ] #NNNN` / `- [x] #NNNN`), or a "child issues"/"sub-tasks" section.
3. For an epic, enumerate every child issue number; recurse Step 1 on each child
   (children may themselves be epics).
4. Standalone (non-epic) issues are leaves.

Result: a flat list of **leaf** issues plus the set of **epic** issues. Restate
each leaf as problem + acceptance criteria + affected flow
(`sync -> discover -> parse -> emit -> enqueue -> reducer -> projection -> query`).
Ask before coding if any acceptance criteria are unclear.

**Before touching any code**, output a numbered plan of every leaf issue you
will tackle and the intended order. Wait for explicit user approval before
beginning exploration or editing. If the user does not respond within the
current turn, stop and ask — do not self-approve and proceed.

## Step 2 — Setup

- Create a git **worktree per leaf issue** (never work on `main`). Use the same
  branch name across repos when a change spans repos.
- Load the applicable Eshu project skills for each touched surface and state
  which are active (all under `.agents/skills/`):
  - `golang-engineering` — any Go edit/test/doc.
  - `cypher-query-rigor` — Cypher, graph read/write/index, backend dialect.
  - `concurrency-deadlock-rigor` — workers, leases, conflict keys, retries,
    queue ordering, batching, shared state.
  - `eshu-correlation-truth` — correlation, materialization, deployment tracing,
    query truth.
  - `eshu-mcp-call-rigor` — MCP/API tool calls, bounded graph-backed queries.
  - `eshu-diagnostic-rigor` — runtime diagnostics, reducer throughput, perf proof.
  - `eshu-folder-doc-keeper` — package README.md / doc.go / scoped AGENTS.md.
  - `telemetry-coverage-discipline` — telemetry instruments/contract/dashboard.
  - `generator-script-discipline` — regenerators and generated artifacts.
  - `eshu-release` — release/version/image/Helm/GitHub Release work.
  - `eshu-code-review` — proof-tiered pre-push, PR, and merge-readiness review.
  - `eshu-security-scan-gates` — `.github/workflows/security-scan.yml`, a Go
    toolchain bump (the `go` directive in `go/go.mod`), or a red
    Trivy/gosec/govulncheck/nancy gate.

## Step 3 — Execution doctrine

- Follow the root `AGENTS.md`/`CLAUDE.md` and any scoped `AGENTS.md` to the letter.
- **Never** surface secrets or private/internal/proprietary data (hostnames, IPs,
  keys, credentials, internal URLs, employer-internal identifiers) in issues,
  PRs, commits, code, docs, or comments. Unsure = leave it out.
- `rg`/glob only (never `grep`/`find`). TDD: failing regression test first.
  Files < 500 lines. No AI attribution. No `git stash` across worktrees.
- Serialization is not a fix — partition by conflict key or make writes idempotent.
- Use subagents for independent parallel work when the active harness permits
  delegation. Orchestrator keeps planning, review arbitration, and merge calls;
  executors own scoped implementation/tests/refactors/docs; lookup agents own
  status polls.
- **Self-review is allowed and required.** Every PR must run
  `eshu-code-review` before push, PR creation or update, and merge-readiness.
  Prefer a separate-context review when delegation is available, but if the
  active harness forbids subagents or the repo owner explicitly wants the
  current agent to review, perform the `eshu-code-review` pass directly.
  Self-review must cover the complete diff, touched contracts, tests, generated
  artifacts, docs, private-data leakage, verification evidence, proof tier, and
  follow-on routing.
- **Commit early and often** per worktree. Agent deaths are usage-limit
  boundaries, not load — committed work survives them. Watch agent liveness;
  revive stalled agents, have them commit in-progress work, resume from last
  commit.

## Step 4 — Every few turns, before new work

- Rebase open PRs on `main`; resolve conflicts immediately (PRs merge fast).
  During CI/review waiting, check `gh pr view <n> --json mergeable,headRefOid`
  about every 60 seconds. If `mergeable` becomes `CONFLICTING` or `UNKNOWN`
  for more than one poll, fetch, rebase on `origin/main`, rerun affected focused
  proof, and repeat the complete Steps 5-7 promotion sequence before pushing
  with `--force-with-lease` and restarting the poll. A rebase never permits a
  direct push. Active agents merge constantly; a green check snapshot can
  become stale while the PR is waiting.
- CI is done only after **two consecutive stable reads of the full check set**
  (`gh pr checks <n> --json bucket` shows `pending == 0` AND the total count is
  unchanged across two polls). GitHub registers large check sets in waves, so a
  single `0-pending` read is a false "done" — never merge or claim green on it.
  Report CI status with the exact query used. Only the orchestrator runs the late
  `make pre-pr`; dispatched executors run focused proof only (see
  [Orchestration, PR, And CI Discipline](../../../CLAUDE.md)).
- Fetch ALL inline + bot review comments:
  `gh api repos/eshu-hq/eshu/pulls/<n>/comments`. Treat every reviewer
  uniformly — **codex (`chatgpt-codex-connector[bot]`), GitHub Copilot
  (`github-copilot[bot]`), Cursor, Claude, and human reviewers** — by reading
  the comment body and the cited `file:line`, not by trusting (or skipping) a
  bot label. Address each; resolve a thread only after the referenced code is
  fixed in HEAD (use the `resolve-review-threads` skill, which classifies each
  unresolved thread `fixed` / `unchanged` / `ambiguous` and auto-resolves only
  the `fixed` ones). Duplicate findings across bots: fix the code once, resolve
  both threads. When bots disagree, trust the code and the project rules.
- **If GitHub Copilot returns "couldn't review any files"** on its first pass,
  re-request the review immediately via `gh pr edit <n> --add-reviewer @copilot`
  (reviewer re-requests use `gh pr edit`, not `gh pr review`) and poll again
  before proceeding. An empty first review is not a pass — it is a failed
  request that must be retried.
- Check GHA on every PR. Enumerate **every** check's state, not just the green
  rollup; on red, root-cause (no symptom patch), fix, rerun. While checks are
  pending, poll the PR about every 60 seconds for merge conflicts and new review
  threads instead of staring only at the check watcher. If the rollup is stale
  or empty after a push, poll the underlying workflow runs for the head SHA
  before treating CI as absent. A clean PR *diff* can still inherit pre-existing
  red: this repo has no required-status-check enforcement, so whole-module Lint
  Go / Go tests that an earlier sequential gate masked (e.g. a failing "Verify
  hot-path evidence" step that aborts the job before Lint Go) surface only on
  the first PR to pass those earlier gates. Fix the inherited debt in that PR —
  do not merge through red because "it is not my diff." The only red you may
  carry is a check on a documented advisory allowlist (state it explicitly, e.g.
  the Docker `verify-reproducibility` build-determinism job); treat every other
  red as blocking.

## Step 5 — Per-PR gate (no skip)

1. TDD implementation, committed incrementally.
2. Run focused verification from `docs/public/reference/local-testing.md` for the
   touched packages; cite exact commands + results.
3. Runtime-affecting -> perf proof or no-regression measurement + operator
   telemetry (spans/metrics/logs).
4. Ensure `gh` auth can push, then `git fetch origin`, rebase on `origin/main`,
   rerun the focused gates affected by the rebase, confirm
   `git status --short` is clean, and inspect
   `git diff --stat origin/main..HEAD` for unrelated reversions or sibling-PR
   rollback.
5. **Preliminary review gate.** Run `eshu-code-review` on the rebased final diff
   after focused proof and before `make pre-pr`. Prefer separate-context
   reviewers in PARALLEL when the harness permits delegation; otherwise run the
   skill as an explicit self-review in the current agent. Either mode must be
   prompted to FIND defects (default to reject, not approve) and must include:
   - proof tier decision and required evidence,
   - all required passes including hostile-read verdict and cross-pass
     contradiction check,
   - severity, confidence, disposition, file:line, violated rule/skill, and fix
     for every finding,
   - generated-artifact, docs, private-data, and verification-evidence scan,
   - follow-on issue routing for defects outside the PR scope.

   Do not run `make pre-pr` if the verdict contains any finding. Fix every P0,
   P1, and P2, rerun affected focused proof, and repeat the full review until
   **P0=0, P1=0, and P2=0**. In self-review mode, explicitly say it was
   self-review mode and list the evidence inspected.
6. **Promotion gate.** Once the preliminary review is clean and the branch is
   otherwise ready for its intended push, run `make pre-pr` exactly once. Do
   not spend its CPU cost as an early discovery loop. Then run a final full
   `eshu-code-review` against the exact post-preflight diff. If preflight changes
   generated or tracked files, or the final review finds anything, fix the
   issue, rerun affected focused proof, and repeat from the preliminary review.
   If `make pre-pr` fails, do not immediately rerun it. Fix the failure, rerun
   affected focused proof, repeat the preliminary full review to zero findings,
   and only then begin a new promotion attempt.
   Make no edits between the final clean review and push; any diff change
   invalidates the verdict.
7. Push the reviewed rebased head.
   Use `git push --force-with-lease` when rebasing an already-pushed branch.
8. Open or update the PR only after the rebased head is on GitHub. Use a
   humanized description and update affected docs in the same PR. Immediately
   check `gh pr view <n> --json mergeable,statusCheckRollup` and fix conflicts
   before waiting on CI.
9. **NO MERGE** until the external bot reviews (codex / Copilot / Cursor / Claude)
   AND the review gate above both land AND all their findings resolve. CI green
   is necessary, not sufficient. During CI waiting, poll mergeability and review
   threads about every 60 seconds. If `origin/main` advances, mergeability
   changes, or the PR head changes for any reason, rebase on `origin/main`,
   rerun affected focused proof, and repeat the complete Steps 5-7 sequence on
   the new base/head: clean preliminary review, one late `make pre-pr`, final
   exact-diff review, then push the reviewed head and continue the CI wait. Do
   not push a rebased or otherwise changed head directly.
10. **When the goal is "drive to merged-closed", execute the merge.** Do not
   defer the merge back to the user when all gates are green and all review
   threads are resolved. Use `gh pr merge <n> --repo eshu-hq/eshu --squash
   --delete-branch` and confirm the returned state is `MERGED`. Deferring is
   only appropriate when an explicit blocker exists (operator-only gate,
   outstanding P0/P1 finding, unresolved thread).

## Step 6 — New issues

When work surfaces a separate defect or follow-up, file a GH issue (clear repro,
acceptance criteria, no private data), work it as part of this goal, and link it
to the originating issue.

## DONE (proof — paste each turn before claiming done)

- For every leaf issue AND every epic:
  `gh issue view <n> --repo eshu-hq/eshu --json state` shows `CLOSED`.
- For every follow-up issue filed: closed, or deferred with a written reason.
- `gh pr list --repo eshu-hq/eshu --state merged --search "<n>"` shows the PRs
  MERGED (`gh pr list` defaults to `--state open`, so omitting the state would
  return nothing once the work has merged).
- For each open PR owned by this work:
  `gh pr view <n> --repo eshu-hq/eshu --json mergeable,statusCheckRollup` shows
  no conflicts and CI green. **Confirm merge state directly from the GitHub API —
  do not assert it from local git or memory.**
- The PR history shows the branch was fetched/rebased on `origin/main` before
  PR creation or the latest PR update, the rebased head was pushed, and the
  CI-wait loop polled mergeability about every 60 seconds until merge.
- `gh api repos/eshu-hq/eshu/pulls/<n>/comments` shows zero unresolved
  review/bot threads (codex / Copilot / Cursor / Claude / human).
- Latest `eshu-code-review` verdict shows **P0=0, P1=0, and P2=0** with
  re-review proof,
  the selected proof tier, all required passes including hostile read,
  cross-pass contradiction check, generated-artifact/doc/private-data scan,
  verification evidence, and follow-on routing for any out-of-scope defect. If
  this was self-review mode, the verdict explicitly says so and lists the
  inspected evidence.
- The promotion record names the preliminary review phase, reviewed head, and
  P0/P1/P2 counts; the exact `make pre-pr` command and result; the
  post-preflight head and clean-status result; and the final review phase and
  P0/P1/P2 counts. The recorded order must show a zero-finding preliminary
  review before preflight and a zero-finding final review afterward.
- **Before closing any issue as fixed**: run the full verification suite from
  `docs/public/reference/local-testing.md` with exact tool versions. Do NOT
  shortcut by verifying a pre-existing fix, trusting a prior CI run, or
  asserting correctness from code inspection alone. Cite the commands run and
  their output. A fix that cannot be reproduced by running the gates is not done.

Not done until ALL of the above are pasted and clean.
