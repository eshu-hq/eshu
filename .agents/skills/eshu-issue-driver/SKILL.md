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
  `eshu-code-review` with `P0=0, P1=0, P2-blocking=0`, one late `make pre-pr`,
  a final
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

Poll with a **bounded background waiter that blocks until a condition holds**
(`until <check>; do sleep 40; done`, with an iteration cap), run as a background
command — a foreground sleep is refused by the Claude Code harness — not by
spending a turn per poll. One waiter per condition: duplicates racing on the
same condition waste turns, and a waiter whose match pattern cannot occur —
watching a log for a string that run never prints — spins to its cap while
reporting nothing. Kill
superseded waiters when the thing they watch is replaced. The cadence is a
ceiling on staleness, not a requirement to burn a turn each minute.

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
  the disposition of every out-of-scope defect — fixed inline by default, routed
  to a tracked follow-up only when the fix cannot ride along and the owner
  agreed.
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
  Report CI status with the exact query used, **and the head SHA it was read
  against**. Re-resolve the SHA at read time, never at watcher launch: a poller
  that captured `$SHA` when it started keeps reporting that commit after a
  force-push, so both of its "stable" reads can honestly describe a head that no
  longer exists. A read whose SHA is not the current head is stale — discard it.
  Before merging, confirm local `HEAD` equals the PR's `headRefOid`.
  Only the orchestrator runs the late
  `make pre-pr`; dispatched executors run focused proof only (see
  [Orchestration, PR, And CI Discipline](../../../CLAUDE.md)).
- **Cancelled is not failed, but a cancelled BLOCKING gate is not self-healing.**
  A cancelled job does not re-run itself — only a new push or an explicit rerun
  restarts one; a cancelled required gate strands
  `required-gates-complete` once every other check finishes, and nothing
  re-triggers it. Re-run those explicitly (`gh run rerun <run-id>`) and confirm
  the aggregate re-fires. An aggregate failure whose only cause is cancelled
  children says nothing about the diff: read the failing job's steps before
  concluding, since a hung setup step — a package install stalling until the
  runner kills it — presents identically to a real failure.
- **Keep the machine quiet for the live lanes.** They bind fixed host ports and
  saturate CPU, so a build, a second gate, or another worktree's `make pre-pr`
  running alongside produces a RED that is starvation, not a defect. One session
  lost a 936s run this way and drew two wrong conclusions from it before a quiet
  re-run passed the identical diff in 451s; the same gate takes 139s unloaded.
  Serialize gate runs, and before treating any live-lane failure as real, check
  what else was running during it. Detect a live run by the lock in
  `scripts/lib/live-gate-lock.sh`, by `pgrep -f verify-golden-corpus-gate`, or
  by port 15432 being bound — `pgrep -f "make pre-pr"` has failed to match a
  running gate and been misread as the process having died.
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
- **If no external reviewer produces a review, dispatch a replacement.**
  A quota message is NOT a review and NOT a retryable failure — re-requesting it
  only burns polls. Recognise the shape rather than the exact wording: Copilot
  posts "reached their quota limit", `chatgpt-codex-connector[bot]` posts
  "reached your Codex usage limits". More than one can be down at the same time,
  and when all of them are, a PR silently drops to zero external review while
  every other gate stays green.

  "Every external reviewer" means every reviewer actually requested on this PR,
  not the full roster in Step 9. Verify it rather than assume it: for each
  requested reviewer, the fallback triggers only when it has either posted a
  quota/unavailable message or failed to produce a review. Cursor and Claude
  emit no quota shape, so if either was requested and has not answered, chase
  that review first — the fallback is for when no external reviewer can answer,
  not for when the two GitHub-App bots happen to be the only ones asked.

  The trigger is the set of reviewers that actually produced a review being
  empty after retries — whatever the reason, quota or silence. When it is, the
  coverage MUST be replaced, not waived: dispatch a subagent in a separate
  context to run the full `eshu-code-review` against the final diff, prompted to
  FIND defects (default to reject), covering proof tier, all required passes
  including hostile read, cross-pass contradiction check,
  severity/confidence/disposition per finding, and the generated-artifact,
  docs, private-data and evidence scans.

  This replacement is **additional to the Step 5 review gate, never the same
  verdict relabelled**. Step 5's separate-context review is the author-side gate
  and is already required; re-pointing at it here would leave the PR with the
  one review this clause exists to prevent.

  The replacement verdict MUST be posted as a PR comment — full template, the
  reviewing model named, and the base/head SHAs it covers — so the independence
  claim lands in GitHub truth rather than in a PR body the dispatching agent
  wrote about itself.

  The replacement must be **independent in a way the dispatching agent cannot
  self-certify**. Two requirements are hard, because both are checkable in any
  harness: it MUST run in a fresh context that did not write the diff, and its
  verdict MUST be posted as a PR comment naming the reviewing model.

  Prefer a **different model lineage than the author's wherever the harness
  offers one** — a different lineage catches what a single family rationalizes
  away ([Agent Orchestration Model](../../../docs/internal/agent-orchestration.md)
  reaches for a cross-family model as an independent verifier for that reason).
  This is a preference, not a MUST, because some harnesses expose only one
  lineage: Claude Code's Agent tool offers Anthropic models only, so a
  Claude-authored diff cannot get a cross-lineage reviewer there. When the
  harness cannot offer one, say so in the PR comment — "this review shares the
  author's lineage" — rather than leaving the reader to assume independence the
  setup could not provide. An unsatisfiable MUST would simply be skipped, and
  the clause skipped would be the one replacing external review.

  Self-review is the last resort, not a fallback of convenience. A `/goal`
  harness can dispatch subagents by definition, so "delegation is unavailable"
  is almost never true: it means the harness exposes no subagent capability at
  all. Taking that branch requires stating the specific reason delegation
  failed AND owner confirmation before proceeding — an agent that wrote the
  diff, reviewed it itself, and self-certified that delegation was unavailable
  has produced no independent review at all.

  State in the PR which reviewers were unavailable and what replaced them.
  Merging with "the bots were out of credit" and nothing in their place is a
  rule violation, not a shortcut: the merge bar is a reviewed diff, and where
  that review comes from is an implementation detail.
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
   run `cd go && go vet ./...` (there is no root `go.mod`, and vet compiles the
   test files too — a sibling PR moving a test helper breaks `go test`
   compilation while `go build` still passes; conflict-free is not
   compile-clean),
   rerun the focused gates affected by the rebase, confirm
   `git status --short` is clean, and inspect
   `git diff --stat origin/main..HEAD` for unrelated reversions or sibling-PR
   rollback.
5. **Preliminary review gate.** Run `eshu-code-review` on the rebased final diff
   after focused proof and before `make pre-pr`. Prefer separate-context
   reviewers in PARALLEL when the harness permits delegation; otherwise run the
   skill as an explicit self-review in the current agent. "Does not permit
   delegation" carries the strict meaning from the quota-exhaustion fallback
   above: the harness exposes no subagent capability at all, not the agent
   judging dispatch unnecessary. Either mode must be
   prompted to FIND defects (default to reject, not approve) and must include:
   - proof tier decision and required evidence,
   - all required passes including hostile-read verdict and cross-pass
     contradiction check,
   - severity, confidence, disposition, file:line, violated rule/skill, and fix
     for every finding,
   - generated-artifact, docs, private-data, and verification-evidence scan,
   - disposition for defects outside the PR scope: fixed inline by default (see
     Step 6), and only routed to a separate issue when the fix cannot ride along
     AND the owner agreed.

   Do not run `make pre-pr` while a blocking finding stands. Fix every P0, P1,
   and blocking P2, rerun affected focused proof, and repeat the full review
   until **P0=0, P1=0, and P2-blocking=0**, with every deferred P2 tracked
   (linked issue, owner agreement quoted in the PR), named, and carrying its
   severity-table category verbatim
   (`eshu-code-review/references/merge-bar.md`). In self-review mode,
   explicitly say it was self-review mode and list the evidence inspected.
6. **Promotion gate.** Once the preliminary review is clean and the branch is
   otherwise ready for its intended push, run `make pre-pr` exactly once. Do
   not spend its CPU cost as an early discovery loop. Then run a final full
   `eshu-code-review` against the exact post-preflight diff. If preflight changes
   generated or tracked files, or the final review finds anything, fix the
   issue, rerun affected focused proof, and repeat from the preliminary review.
   If `make pre-pr` fails, do not immediately rerun it. Fix the failure, rerun
   affected focused proof, repeat the preliminary full review to
   `P0=0, P1=0, P2-blocking=0`, and only then begin a new promotion attempt.
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
   is necessary, not sufficient. If no external reviewer produced a review (the
   Step 4 trigger — quota or silence), this clause is satisfied by the
   dispatched replacement reviewer, never by proceeding with no external review
   at all. During CI waiting, poll
   mergeability and review threads about every 60 seconds. If `origin/main`
   advances, mergeability changes, or the PR head changes for any reason,
   rebase on `origin/main`,
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

## Step 6 — Defects surfaced mid-drive: FIX INLINE, do not file

When work surfaces a separate defect, **fix it in the change at hand**, with its
own failing-then-green test. Filing a GH issue is the exception, not the default.

This is a deliberate correction. Filing an issue per finding reads as diligence
but produces backlog sprawl: it converts work that could have been finished into
tickets nobody picks up, and it fragments one coherent problem across many
trackers. One drive of epic #5455 filed ten issues that way.

A defect found while fixing something adjacent is usually still in scope —
especially in the same function, file, or evidence path. Bias hard toward fixing
it.

File a separate issue ONLY when the fix genuinely cannot ride along:

- it needs a design decision the repo owner must make;
- it would change unrelated projected truth and needs its own proof; or
- it blocks on credentials, infrastructure, or repo-admin access.

Even then, **ask the owner first** rather than filing unilaterally. When you do
file, work it as part of this goal and link it to the originating issue, and add
it to the epic's follow-ups list at creation time.

## DONE (proof — paste each turn before claiming done)

- For every leaf issue AND every epic:
  `gh issue view <n> --repo eshu-hq/eshu --json state` shows `CLOSED`.
- For every follow-up issue filed: closed, or deferred with a written reason.
  Filing one at all requires the owner's agreement — quote the message granting
  it. Every other clause here demands a command and its output; an exception
  that the agent invoking it can self-certify is not a gate.
- `gh pr list --repo eshu-hq/eshu --state merged --search "<n>"` shows the PRs
  MERGED (`gh pr list` defaults to `--state open`, so omitting the state would
  return nothing once the work has merged).
- For each open PR owned by this work:
  `gh pr view <n> --repo eshu-hq/eshu --json mergeable,statusCheckRollup` shows
  no conflicts and CI green. **Confirm merge state directly from the GitHub API —
  do not assert it from local git or memory.**
- The PR history shows the branch was fetched/rebased on `origin/main` before
  PR creation or the latest PR update, the rebased head was pushed, and the
  CI-wait loop watched mergeability continuously until merge (a bounded waiter
  blocking on the condition satisfies this; a literal poll-per-turn is not
  required, but leaving a PR unwatched between long gates is not acceptable).
- `gh api repos/eshu-hq/eshu/pulls/<n>/comments` shows zero unresolved
  review/bot threads (codex / Copilot / Cursor / Claude / human).
- If any external reviewer was unavailable,
  `gh api repos/eshu-hq/eshu/issues/<n>/comments` shows the replacement
  verdict comment, naming the reviewing model and which reviewers were
  unavailable.
- Latest `eshu-code-review` verdict shows **P0=0, P1=0, and P2-blocking=0**,
  every deferred P2 tracked and named, with re-review proof,
  the selected proof tier, all required passes including hostile read,
  cross-pass contradiction check, generated-artifact/doc/private-data scan,
  verification evidence, and the disposition of every out-of-scope defect —
  fixed inline by default, routed to a tracked follow-up only when the fix
  cannot ride along and the owner agreed. If this was self-review mode, the
  verdict explicitly says so and lists the inspected evidence.
- The promotion record names the preliminary review phase, reviewed head, and
  P0/P1/P2-blocking/P2-deferred counts; the exact `make pre-pr` command and
  result; the post-preflight head and clean-status result; and the final review
  phase and the same counts. The recorded order must show a preliminary review
  with no blocking finding before preflight, and the same afterward.
- **Before closing any issue as fixed**: run the full verification suite from
  `docs/public/reference/local-testing.md` with exact tool versions. Do NOT
  shortcut by verifying a pre-existing fix, trusting a prior CI run, or
  asserting correctness from code inspection alone. Cite the commands run and
  their output. A fix that cannot be reproduced by running the gates is not done.

Not done until ALL of the above are pasted and clean.
