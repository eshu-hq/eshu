# Eshu Mandatory Agent Rules

Eshu is a self-hosted context graph that connects code, dependencies, supply
chain, infrastructure, and runtime into one queryable, evidence-backed source of
truth for CLI, MCP, and HTTP API workflows. Treat it as a production data
platform, not a script collection.

This file defines the shared rules for agents working in Eshu. Claude Code
reads it natively; do not add a `CLAUDE.md`, which would hide every scoped
`AGENTS.md` from Claude. Load the sections the task needs.

## Mandatory Startup

Read this file and the scoped `AGENTS.md` for each directory you touch. Use
[Read These First](#read-these-first) and [Skill Routing](#skill-routing) to
load only the context the change and its verification need. The
[Agent Engineering Guide](docs/internal/agent-guide.md) holds detailed
contracts. The [Agent Orchestration Model](docs/internal/agent-orchestration.md)
applies when you delegate work. A prose correction does not need a runtime
architecture tour.

Settle uncertainty from source, docs, or a bounded experiment. If those cannot
settle it (unclear ownership, design intent, acceptance criteria, or
authorization), escalate to an arbiter model with the raw observations. The
role is `arbiter-eshu`: Fable on Claude, Astra on Codex, Muse Spark on Muse. Act
on its verdict. Record the decision in the goal file or PR. Do not stop to ask
the owner.

Complete the authorized work through validation and fixes. A first
implementation is not complete when the task includes a working result or a PR.
Do not end a turn until one of these is true:

1. You verified that the whole job is complete.
2. Outside work is pending and a live watcher will wake you. Write
   `BLOCKED: <reason> WATCH=<pid>`.
3. The owner orders a stop.

A hard question, a failed attempt, a progress report, or an irreversible next
act is not a stopping point. Report the actual state. Mark anything you did not
check as NOT_CHECKED.

## Mandatory Pre-PR Code Review

Before you open a PR, push a PR update, or say the work is ready to merge, run
`eshu-code-review` on the final diff. Select proof by the changed contracts and
claims. Cover all applicable review surfaces, including the hostile read. Record
severity, confidence, disposition, and evidence for every finding. Keep the
review independent where the harness allows it. A small diff still needs
review. Leave irrelevant runtime detail out of the report.

Fix P0, P1, and blocking P2 findings before the expensive promotion gate. A P2
blocks when it contradicts a PR claim or is cheap and in the same edit. Any
other P2 needs a linked issue, the owner's agreement quoted in the PR, and its
severity-table category. P3 findings do not block. The authoritative bar is
[merge-bar.md](.agents/skills/eshu-code-review/references/merge-bar.md).

The promotion order is:

1. Run focused local proof.
2. Run a clean preliminary `eshu-code-review`. Capture a `ci-gates review-attest`
   receipt. A waiver note replaces the review. Capture it after step 3.
3. Run `make pre-push` (the fast local floor).
4. Run `ci-gates review-attest verify`. A match replaces a second full review.
5. Push. Make no edits between the verified attestation and the push.

A change to the base, commit, tree, worktree, submodule, PR claims, packet, or
verdict voids the receipt. Then rerun the affected proof and a full review.
Never publish an unreviewed diff. A rebase follows the decision table in
`eshu-code-review` ("Rebase Waiver"):

- No conflicts, no open finding, every commit `=` in `git range-diff`, equal
  patch-id, clean prior verdict: no review rerun. Run `make pre-push` first.
  After exit 0, capture a waiver note with that exit and head SHA, verify, and
  push. Record a green CI wave on the pushed head before merge.
- Conflicts, no open finding: scoped re-review of the resolutions. An added
  commit gets a scoped review when every other commit is `=`.
- Full review: no conflicts but a changed patch-id, or a commit changed,
  dropped, reordered, or reworded. Also full review: an open finding, or no
  clean prior verdict.

## Mandatory Pre-PR Local Proof

Prove the requested behavior locally before you open or update a PR. For a bug,
run the failing regression to green. For performance, measure before and after
on the touched path. For a runtime change, observe the changed behavior. For
docs, features, and refactors with no prior failure, use the matching checks.
Do not invent a failing reproduction.

Run focused verification during development. Fix the failures your change
causes and rerun the affected checks. Keep every gate the repository requires.
Do not repeat unchanged proof unless an edit, a failure, or an environment
change invalidates it. Local fixture tests that have no production access can
run within the authorized task without separate approval at each step. This does
not grant access to production data or approve external mutations.

Follow the promotion order above. CI must not be the first test of an unproven
change. If local proof is blocked, report the command and the cause before you
publish. Do not open a speculative PR to see whether a change works.

## Mandatory PR And Issue Text

Before a `gh pr create`, `gh pr edit`, `gh issue create`, or `gh issue edit`
that sets title or body text, load the `eshu-publish` skill. Start from its
template for your kind of PR or issue. Run
`.agents/skills/eshu-publish/scripts/check-shape.sh` on the draft before you
capture a `ci-gates review-attest` receipt, and fix every `FAIL` line. A PR body
opens with a `Refs #N.` line, a bold lead, and an "At a glance" table (optional
for a body under 600 characters with no heading). It keeps evidence in
`<details>`.

Skip the shape check for an issue made from an issue form (keep the form
headings), for a revert, and for an edit that changes no title or body text,
such as a label. This rule applies to every agent that writes this text. An edit
to the title or body after you capture a receipt voids the receipt.

## Mandatory Prove-The-Theory-First

Before you build a change that rests on a performance or behavior theory and
could slow or degrade a process, agents MUST first prove the theory with the
cheapest possible shim. Use a throwaway SQL script with `EXPLAIN ANALYZE`, a
one-off Cypher `PROFILE` or `EXPLAIN`, a microbenchmark, or a scratch query. Run
it against representative data BEFORE you write the change or dispatch an
executor. This proof is separate from the Pre-PR Local Proof and comes before it:
prove the THEORY, then do the WORK, then prove the WORK locally.

The gate is mandatory for any change on the accuracy, performance, or
concurrency contract, including hot-path Cypher and graph writes, Postgres SQL, schema DDL
and indexes, reducer projection and materialization, queue and lease behavior,
and anything with a repo-scale performance contract. A candidate index, query
rewrite, cache, prefilter, denormalization, or backend knob is a theory until it
is measured. Do not build on an unmeasured one. Agents MUST NOT dispatch an
executor, land production code, or open a PR on an unproven theory. Agents MUST
stop any executor already dispatched on an unproven theory until the proof
lands.

A diagnosis is a theory too. State a cause only with the observation behind it.
Never treat one passing sample as proof. The proof shape, the diagnosis rules,
and the disproven-theory and PR-acceptance rules are in
[Agent Engineering Guide](docs/internal/agent-guide.md#prove-the-theory-first).
This section complements [Evidence Rules](#evidence-rules) and
[Serialization Is Not A Fix](#serialization-is-not-a-fix).

## Runtime Shape

- **API** serves HTTP reads and admin/query surfaces.
- **MCP Server** serves tool-facing read workflows.
- **Ingester** owns repo sync, discovery, parsing, and fact emission.
- **Reducer / Resolution Engine** owns queued projection, repair, and shared
  materialization.
- **Bootstrap Index** owns one-shot local or deployment seeding.
- **Postgres** stores facts, queue state, content, status, and recovery data.
- **Neo4j** is the supported canonical graph backend. Correctness, performance
  budgets and gates are defined against the pinned Neo4j. NornicDB is secondary
  and not supported for deployment. Its other CI legs stay blocking until
  phase 2; the `corpus-gate (nornicdb)` cell is already watch-only (#7362).
  See [Graph Backend Decision](docs/internal/design/7331-neo4j-primary-graph-backend-decision.md).

There is no Python runtime on the normal platform path. Python remains only in
fixture corpora or offline tooling.

## Non-Negotiable Rules

Agents MUST read
[Agent Git And Worktree Hygiene](docs/internal/agent-git-hygiene.md) before any
git, worktree, stash, or push action. It holds the wrong-worktree recovery
procedure and the incident behind each rule below.

- If an edit lands outside the intended feature worktree, stop editing there and
  report the paths. Recover without discarding the work of others. Use the
  arbiter model for a non-obvious plan. MUST NOT recover silently.
- MUST use `rg` for all text searches. NEVER use `grep`. Corroborate census counts from a subdirectory search root with `git grep` or `git ls-files`: an anchored `.gitignore` line can hide tracked files from `rg` with exit 0 (#7750; the gitignore-rg-parity gate pins every fixed root, `.claude/` stays uncovered in #7771).
- MUST use `rg --files` or globbing for file discovery. NEVER use `find`.
- Use local docs to establish the relevant contract. Read source or external
  documentation as needed to settle the real uncertainty of the task.
- Research and design questions are work to complete. If evidence cannot settle
  one, escalate it to an arbiter model, not to the owner. See
  [Delegate An Undecided Design](docs/internal/agent-guide.md#delegate-an-undecided-design-do-not-escalate-it).
- The active goal authorizes the acts it needs. Before an irreversible act
  (merge, deploy, external mutation, destructive deletion, production data
  change, or a change to the golden standard), get an arbiter model's review.
  Then do the act and name it in the report. Scope approval does not permit
  unrelated acts. `CONSENT: <acts>` and `CLAUDE_GOAL_CONSENT` record grants (see
  [Agent Hooks](docs/internal/agent-hooks.md)). Never invent or widen a grant.
- A bug fix needs a failing regression test before the fix. New behavior needs
  tests of its contract. A refactor needs proof that the existing contract
  holds. Choose checks that exercise behavior, not tests that match
  implementation wording or harmless documentation edits.
- MUST keep files under 500 lines, including Go source and Markdown under `go/`
  and `docs/` (the gates enforce those). Split a file before it approaches the
  limit.
- MUST NOT add AI attribution to commits, PRs, or docs.
- MUST NOT push to `main` or `master`.
- MUST install the repo pre-commit hooks once per clone
  (`scripts/dev/bootstrap-hooks.sh`). MUST NOT use `--no-verify`. MUST run
  `make pre-push` before every push (the fast local floor; it writes no stamp).
  The CI `required-gates-complete` aggregate is the blocking, non-bypassable authority for
  Ifá/Odù contracts, performance, and end-to-end behavior. See
  [Verification Defaults](#verification-defaults).
- MUST create a git worktree before you execute a plan or PRD. MUST verify that
  `pwd` is that worktree before any Edit or Write.
- MUST run every command that changes tracked files (regenerators, formatters,
  `go mod tidy`) inside a worktree, including for diagnostics. Keep the main
  checkout a clean fast-forward of `origin/main`.
- MUST NOT use `git stash` when more than one worktree may be active. All
  worktrees share one stash stack, and concurrent agents corrupt it. To compare
  with a clean tree, use `git diff`, `git show <ref>:<path>`, or a throwaway
  worktree.
- MUST verify that HEAD is on a named branch before every commit
  (`git symbolic-ref -q HEAD`). MUST confirm that the pushed SHA equals local
  HEAD before you open or update a PR.
- MUST NOT put an issue-closing keyword (`Fixes`, `Closes`, `Resolves`, …) in a
  commit message or PR body unless that issue is meant to close on merge. Otherwise
  write the issue as `#NNNN`.
- MUST synchronize remote test machines by Git fetch and checkout of the
  reviewed branch. NEVER `rsync` an unreviewed worktree as performance evidence.
- MUST use the same branch and worktree name across repos when one workflow
  touches several repos.
- MUST follow Effective Go for Go, Google Python style for Python fixtures or
  tools, strict typing for TypeScript, HashiCorp Terraform practices, and Helm
  chart best practices.

## Life Motto

Accuracy, performance, and concurrency are the life motto of this repository.
Agents MUST protect all three on every change.

1. **Accuracy:** a wrong graph, query, or deployment truth is a product failure.
2. **Performance:** measure correct behavior and keep it within the repo-scale
   performance contract.
3. **Concurrency:** correctness and performance must hold under the intended
   concurrent worker, queue, graph-write, retry, and lease model.

Agents MUST NOT introduce correctness bugs, unmeasured performance degradation,
or serialized workarounds that hide concurrency defects.

Agents MUST NOT optimize behavior that has not been proven correct. Agents MUST
NOT make a system more reliable by hiding wrong results, swallowing failures,
single-threading work, or inventing silent fallbacks.

## Read These First

Read the doc that matches the change, not every doc for every change:

- Service ownership, startup, or deployment behavior:
  [Service Runtimes](docs/public/deployment/service-runtimes.md).
- Selecting or running a verification gate:
  [Local Testing](docs/public/reference/local-testing.md).
- Adding or changing an operator signal:
  [Telemetry](docs/public/reference/telemetry/index.md).
- Pipeline stages or ownership boundaries:
  [Architecture](docs/public/architecture.md).
- Docker Compose: [Docker Compose](docs/public/run-locally/docker-compose.md).
- Hot-path Cypher, graph writes, query handlers, reducer projection,
  materialization, or schema DDL:
  [Cypher Performance](docs/public/reference/cypher-performance.md).
- NornicDB knobs or compatibility:
  [NornicDB Tuning](docs/public/reference/nornicdb-tuning.md),
  [NornicDB Pitfalls](docs/public/reference/nornicdb-pitfalls.md), and
  [Graph Backend Installation](docs/public/reference/graph-backend-installation.md).
- Writing or editing this file, a skill, or another agent-facing doc:
  [Writing For Agents](docs/internal/writing-for-agents.md).

## Skill Routing

Project skills in `.agents/skills/` are the source of truth. `.claude/skills/`
and `.codex/skills/` link to them. Select the smallest set that covers the task.
Use the available names and descriptions to find skills. Read a skill once when
it applies, and load its references only for the workflow you selected. Do not
reload an unchanged skill for each edit or status message.

A `/goal` prompt that names skills is enough. The user need not name agents. The
prompt hook supplies phase candidates from the manifest. The coordinator owns
the goal, loads its skills, and dispatches bounded work only when it helps. For
model, access, and launcher details, see
[Goal and skill prompts](docs/internal/agent-orchestration.md#goal-and-skill-prompts).
To write a `/goal` condition, see
[Writing A `/goal` Condition](docs/internal/agent-goal-conditions.md).
OpenCode model selection stays a session choice. The model of the main session
does not change automatically.

Use the [task-to-skill map](docs/internal/agent-orchestration.md#task-to-skill-map)
and `.agents/skills/` to find skills for a task. State which skills are active.
Routine status messages use the same plain, evidence-backed style and need no
writing playbook.

## Golden Rules

- MUST understand the relevant flow before editing:
  `sync -> discover -> parse -> emit facts -> enqueue work -> reducer -> graph/content projection -> query surface`.
- MUST fix root cause, not symptoms.
- MUST prove accuracy first, then performance, then concurrency behavior for
  runtime-affecting work.
- MUST account for invalid input, empty state, stale state, partial failure,
  duplicates, retries, ordering, idempotency, concurrency, and rollback.
- MUST preserve package ownership boundaries. The ownership table lives in
  [Agent Engineering Guide](docs/internal/agent-guide.md#ownership-boundaries).
- MUST include telemetry an operator can use at 3 AM for runtime-affecting changes.
- MUST research official documentation before deciding on external SDK,
  database, queue, transaction, and concurrency behavior.

## Evidence Rules

- A bug fix MUST have a failing regression test first.
- Performance work MUST have before and after measurements.
- Performance issue priority MUST rest on the latest accepted measured
  bottleneck and its target contribution budget. Do not rank it by issue title, old backlog
  severity, or a real but small local optimization. Re-rank stale performance
  issues before you implement them.
- A performance comparison MUST use the same primary start and end events,
  corpus, profile, topology, and storage state. Report exact seconds and a human
  duration. Label totals that are not comparable. Do not manufacture a speedup.
- End-to-end and collector runs MUST be compared with the last known-good named
  baseline manifest that has matching metric boundaries, corpus, profile, topology, and
  storage state. A large regression from that baseline is a bug to root-cause,
  not an acceptable cost. State a time bound before you launch any long run.
- Queue and concurrency work MUST have proof of contention, retry, idempotency,
  ordering, and dead-letter behavior.
- A performance rewrite that touches a lock, claim, lease, or queue path MUST
  include a concurrency proof (contention, EvalPlanQual recheck, or lease
  safety), not only a row-set equivalence differential. It MUST be re-proven on
  the built binary against the real worst-case backlog, not only a small-N EXPLAIN.
- Graph truth work MUST show that fixture intent, reducer graph truth, and
  API/query truth agree.
- A runtime change MUST have operator-facing metrics, spans, logs, status, or
  pprof proof.
- A docs-only change MUST run the docs build gate when it changes navigation or
  project guidance.
- A new or tightened gate, validator, or guard MUST have a seeded-violation
  RED/GREEN pair: it fails on a planted violation and passes on the clean tree.
  A guard satisfied by a comment that describes it, or by a test built from a
  copy of its own data or implementation, is not proof.

Agents MUST NOT say work is ready without a list of the commands or runtime
proof they ran. MUST capture exit codes directly (`cmd; echo $?`, never `$?`
after a pipe). MUST cite verification that postdates the final edit, not an
earlier run. See
[Agent Engineering Guide](docs/internal/agent-guide.md#evidence-capture-pitfalls)
for the false-green incidents behind both rules.

Do not accept a PR on explanation alone. A code change MUST prove the code works
with focused tests or an integration gate. A runtime-affecting change MUST
include performance proof or a no-regression measurement for the touched path.

## Claim Evidence Lives In Known Locations

A dangling evidence pointer is NOT proof of absence. Before you downgrade any
capability, maturity, or support claim as "unvalidated" (especially a
`capability-matrix` support tier or a `product-claims` maturity), check every
committed-evidence location in
[Claim Evidence Locations](docs/internal/claim-evidence-locations.md). The
evidence MUST support the tier the claim asserts. A deployed-tier claim needs
deployed evidence: a remote-validation artifact, a `scripts/run-remote-e2e-*` or
compose driver, or a live-backend evidence note. A local unit test is not enough.
If matching-tier evidence exists, VALIDATE (wire the pointer to it), keep the
claim, and confirm with the owner before any bulk change. If it does not exist,
commit the matching deployed-validation artifact, or downgrade the claim to the
tier its evidence supports.

## Serialization Is Not A Fix

Agents MUST NOT ship worker-count reductions, single-threaded drains, batch
size `1`, or disabled concurrent writers as a fix for non-idempotent writes,
MERGE races, or commit-time uniqueness conflicts.

Accept serialization only as:

- a measured baseline,
- a temporary safeguard while landing the real fix in the same PR, or
- a documented permanent constraint with repo-scale performance proof.

If concurrency is required for the performance contract, agents MUST redesign
the write path, partition by conflict key, or make the write idempotent under
concurrent execution.

## Documentation Discipline

Every code PR that touches user-visible wire contracts, CLI flags, environment
variables, runtime profiles, capability ports, collector contracts, or chunk
boundaries MUST update affected docs in the same PR.

MUST document every new or touched exported Go type, interface, function, method,
constant group, and variable with a useful Go doc comment. Placeholder comments
that only repeat the identifier are not acceptable.

Every Go package directory in `go/` has three files: `doc.go`, `README.md`, and
`AGENTS.md`. They serve different audiences:

- `doc.go` for the godoc contract.
- `README.md` for human architecture and operational context.
- `AGENTS.md` for scoped agent instructions that Codex and other harnesses load
  for that directory tree.

MUST NOT remove scoped `AGENTS.md` files unless the replacement is proven to be
loaded by the target harness with the same scope and precedence.

MUST keep OpenAPI changes in lockstep with `go/internal/query/openapi/`, handler
tests, and [HTTP API Reference](docs/public/reference/http-api.md).

## Verification Defaults

MUST use [Local Testing](docs/public/reference/local-testing.md) as the source
of truth for gates.

After focused local proof and a preliminary full `eshu-code-review` with zero
P0/P1/P2-blocking findings, run `make pre-push` once, immediately before the
push or PR update. It is the fast
local floor: changed-package tests and race tests, the file cap, lint, build and
vet, `go vet ./...` on the merge of HEAD with `origin/main`, the allowlisted
fast registry gates, and the advisory docs-contradiction gate. It runs no live
Docker, NornicDB, or Postgres lane and writes no stamp. Every other triggered
gate prints `DEFER-CI` and still runs in `make pre-pr` and CI. Verify the review receipt against the exact
post-`pre-push` inputs before the push. If verification fails, run a new full
`eshu-code-review`.

If `make pre-push` prints `DEFER-CI` for a blocking gate on the surface you
changed, run that gate before you push, so CI is not its first run. Use
`make pre-pr` or the gate's `local.command` from the registry. `make pre-pr` and
`make pre-pr-full` are deeper preflights. They are RECOMMENDED, not required,
for queue/lease/claim code, schema DDL, hot-path Cypher or graph writes, reducer
projection or materialization, and a package move. Prefer `pre-pr-full` for a
move: build tags can hide files from `./...`, and only its whole-module race
lane exercises them. Run `make pre-pr` when a change touches `go/internal/ifa` or reducer
materialization.

CI stays authoritative. The `required-gates-complete` aggregate, with
`go-core-complete` and `go-race-complete`, MUST be green before a merge. It holds
the Ifá/Odù gates for contracts, performance, and end-to-end behavior. The live
cells need Docker, NornicDB, and Postgres and never run locally.

Docs, root agent files, and README changes need the docs build
(`mkdocs build --strict --config-file docs/mkdocs.yml`) and `git diff --check`.
A common check is `cd go && golangci-lint run ./...`. If it fails with
`plugin.Open` in a fresh clone or worktree, build the custom lint plugins first.
For that fix, the full `pre-push` scope, the Ifá/Odù gate list, and the other
check commands, see
[Agent Verification Details](docs/internal/agent-verification-details.md).

## Orchestration, PR, And CI Discipline

- For substantive implementation, review, and research, the orchestrator should use
  bounded subagents when independent work or review adds value. Match model
  capability to task difficulty with the tier map in
  [Agent Orchestration Model](docs/internal/agent-orchestration.md#roles-models-and-tools).
  A subagent never downgrades its own model. Leaf agents (executor, debugger,
  reviewer, performance engineer) do not dispatch.
- Only the **orchestrator** runs `make pre-push`, exactly once, immediately
  before the push. It also runs `make pre-pr` or `make pre-pr-full` when the
  change needs that deeper, optional preflight. Subagents MUST NOT run these:
  running the floor N times per branch wastes CPU. Subagents run focused
  verification. Executors also run the registry-selected static gates
  (`develop-eshu` role). Subagents paste the results in the handoff. The live
  gate binds fixed host ports and holds a cross-worktree mutex. See
  [serialization and contention](docs/internal/agent-guide.md#live-gate-serialization-and-contention).
- MUST check open and merged PRs (`gh pr list --state all --search <issue>`)
  and recent commits for the same root cause before you start an issue. A
  shallow clone hides history; see
  [Agent Git And Worktree Hygiene](docs/internal/agent-git-hygiene.md). MUST isolate formatter drift in its own commit. See
  [duplicate-work and formatter-drift guards](docs/internal/agent-guide.md#duplicate-work-and-formatter-drift-guards).
- Before you claim merge-ready, the PR **title AND description** MUST both match
  the final diff. The description MUST carry the before and after evidence.
- CI is complete ONLY after **two consecutive stable reads of the full check
  set** (`pending == 0` and an unchanged total). Large sets register in waves,
  so one `0-pending` read is a false done. State the query you used. Reconcile
  the review-thread API with the displayed unresolved comments before you
  declare the threads clear.
- When a PR uses `Refs #N` and leaves the issue open, comment on #N with what
  remains. That comment, on the issue your PR references, needs no extra
  approval. A reader of the issue then needs no search to find the fix.

## Pre-Ready Checklist

Every applicable condition must hold before you claim ready. If a condition does
not apply, say why when that matters to the review. Do not invent runtime or
telemetry work for a prose-only edit.

- You read the relevant local docs and used the relevant project skill.
- You understand the flow and ownership end to end.
- A bug fix has a failing regression first. Other code changes have contract
  proof. A code-change PR proves the code works before review acceptance.
- You declared the performance impact of runtime-affecting work. A runtime PR
  includes performance proof or no-regression evidence.
- You considered edge cases and concurrency behavior.
- You recorded telemetry, or explicit no-observability-change evidence.
- You updated docs for contract changes.
- You ran and cited focused verification.
- `git diff --check` is clean.
