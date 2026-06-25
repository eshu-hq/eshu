---
name: eshu-issue-driver
description: |
  Drive one or more Eshu GitHub issues (including epics) to a verifiable closed
  state — merged PRs, a separate-context severity-tagged review gate, and
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
separate-context, severity-tagged review gate. Designed to run under `/goal` so
it loops turns until the proof clauses in the DONE section all hold.

## Inputs

- **Issues**: one or more issue numbers or full URLs, from skill args or the
  active `/goal` line. Required — if none are provided, stop and ask. Never
  assume issue numbers.
- **Repo**: this repository, `eshu-hq/eshu`. (This skill is repo-owned; it does
  not drive other repos.)
- **gh auth**: ensure `gh` is authenticated to an account that can push to the
  repo and open PRs before any push/PR step. Do not hard-code an account; use
  whatever the local setup requires (switch with `gh auth switch` if needed).

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
unattended. For wall-clock polling of conflicts/CI/review comments, run a
parallel `/loop 3m`.

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
- **Subagents for all parallel work.** Orchestrator stays Opus (planning, review
  arbitration, merge calls); Sonnet executors for impl/tests/refactors/docs;
  Haiku for lookups/status polls. **Author never reviews own code.**
- **Commit early and often** per worktree. Agent deaths are usage-limit
  boundaries, not load — committed work survives them. Watch agent liveness;
  revive stalled agents, have them commit in-progress work, resume from last
  commit.

## Step 4 — Every few turns, before new work

- Rebase open PRs on `main`; resolve conflicts immediately (PRs merge fast).
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
- Check GHA on every PR; on red, root-cause (no symptom patch), fix, rerun.

## Step 5 — Per-PR gate (no skip)

1. TDD implementation, committed incrementally.
2. Run focused verification from `docs/public/reference/local-testing.md` for the
   touched packages; cite exact commands + results.
3. Runtime-affecting -> perf proof or no-regression measurement + operator
   telemetry (spans/metrics/logs).
4. **Review gate (separate context, author never reviews own code).** Dispatch
   reviewers in PARALLEL, each a fresh agent with a distinct lens, each loading
   the Eshu skills for the touched surface, each prompted to FIND defects
   (default to reject, not approve):
   - **Accuracy (Opus):** wrong graph/query/deploy truth, fact loss,
     fixture-vs-reducer-vs-API disagreement. Skills: `eshu-correlation-truth`,
     `cypher-query-rigor`, `eshu-mcp-call-rigor`.
   - **Concurrency (Opus):** races, deadlocks, lock order/duration,
     MERGE/commit-uniqueness conflicts, missing idempotency under
     workers/retries/ordering, serialization-as-a-fix. Skill:
     `concurrency-deadlock-rigor`.
   - **Reliability (Opus):** silent failures/fallbacks, swallowed errors,
     partial-failure/rollback/dead-letter gaps, missing runtime telemetry,
     unmeasured perf regression. Skills: `eshu-diagnostic-rigor`,
     `golang-engineering`.
   - **Security:** secret / private-data leakage on the full diff.

   Severity tags (priority order accuracy -> performance -> concurrency/reliability):
   - **P0** = correctness / data-loss / security / private-data leak / main-break
     / deadlock. BLOCKS commit and PR. Fix now, re-review, before anything else.
   - **P1** = concurrency defect under intended load, accuracy regression, missing
     idempotency/retry/ordering handling, silent failure, missing runtime
     telemetry, unmeasured perf change on the path. BLOCKS merge. Fix + re-review
     before opening/merging the PR.
   - **P2** = edge case, doc drift, test gap, minor perf, naming. Fix inline OR
     file a GH issue and link it; never silently drop.

   Each finding cites `file:line` + the violated rule/skill + the fix. Paste the
   verdict (counts per severity + resolution). Proceed only when **P0=0 and P1=0
   with re-review proof**.
5. Ensure `gh` auth can push, then push.
6. Open the PR with a humanized description; update affected docs in the same PR.
7. **NO MERGE** until the external bot reviews (codex / Copilot / Cursor / Claude)
   AND the internal review gate above both land AND all their findings resolve.
   CI green is necessary, not sufficient.
8. **When the goal is "drive to merged-closed", execute the merge.** Do not
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
- `gh api repos/eshu-hq/eshu/pulls/<n>/comments` shows zero unresolved
  review/bot threads (codex / Copilot / Cursor / Claude / human).
- Latest internal review verdict shows **P0=0 and P1=0** with re-review proof.
- **Before closing any issue as fixed**: run the full verification suite from
  `docs/public/reference/local-testing.md` with exact tool versions. Do NOT
  shortcut by verifying a pre-existing fix, trusting a prior CI run, or
  asserting correctness from code inspection alone. Cite the commands run and
  their output. A fix that cannot be reproduced by running the gates is not done.

Not done until ALL of the above are pasted and clean.
