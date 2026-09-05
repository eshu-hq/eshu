# Eshu Mandatory Agent Rules

Eshu is a self-hosted context graph that connects code, dependencies, supply
chain, infrastructure, and runtime into one queryable, evidence-backed source of
truth for CLI, MCP, and HTTP API workflows. Treat it as a production data
platform, not a script collection.

This file defines the shared rules for agents working in Eshu. Keep `AGENTS.md`
and `CLAUDE.md` byte-identical. Detailed rules apply to their named surfaces;
load the relevant sections when the task needs them.

## Mandatory Startup

Read this file and scoped `AGENTS.md` instructions for the touched directories.
Use [Read These First](#read-these-first) and [Skill Routing](#skill-routing) to
load only the context needed to understand the change and its verification.
The [Agent Engineering Guide](docs/internal/agent-guide.md) provides detailed
contracts; [Agent Orchestration Model](docs/internal/agent-orchestration.md)
applies when delegating work. A prose correction does not require a runtime
architecture tour.

Resolve uncertainty from source, docs, or a bounded experiment when possible.
Ask when missing ownership, design intent, acceptance criteria, or authorization
cannot be established from the task and available evidence.

Complete the authorized work through relevant validation and fixes. A first
implementation is not completion when the task includes a working result or a
PR. Continue reversible local work without repeatedly asking for approval;
report a concrete blocker when further progress needs user action.

## Mandatory Pre-PR Code Review

Before opening a PR, pushing a PR update, or claiming merge-ready, run
`eshu-code-review` on the final diff. Select proof by the changed contracts and
claims, cover all applicable review surfaces including hostile read, and record
severity, confidence, disposition, and evidence for every finding. Preserve
independent review where the harness permits it. A small diff still requires
review; irrelevant runtime detail does not belong in its report.

Fix P0, P1, and blocking P2 findings before the expensive promotion gate. A P2
blocks when it contradicts a PR claim or is cheap and in the same edit. Other
P2s require a linked issue, the owner's agreement quoted in the PR, and their
severity-table category. P3s do not block. The authoritative bar is
[merge-bar.md](.agents/skills/eshu-code-review/references/merge-bar.md).

After focused proof and a clean preliminary review, capture a
`ci-gates review-attest` receipt and run `make pre-pr` when ready to push.
Verify the receipt after preflight: a match replaces a second full semantic
review. Changed base, commit, tree, worktree, submodule, PR claims, packet, or
verdict invalidates the receipt and requires affected proof and full review
again. Make no edits between verified attestation and push. Never publish an
unreviewed diff.

## Mandatory Pre-PR Local Proof

Prove the requested behavior locally before opening or updating a PR. For a bug,
run the failing regression to green; for performance, measure before/after on
the touched path; for runtime changes, observe the changed behavior. For docs,
features, and refactors without a prior failure, use their appropriate checks;
do not invent a failing reproduction.

Run focused verification during development, fix failures caused by the change,
and rerun affected checks. Preserve all repository-required gates, but avoid
repeating unchanged proof without an invalidating edit, failure, or environment
change. Local fixture tests that have no production access may run within the
authorized task without a separate approval at each step. This does not grant
access to production data or authorize external mutations.

The order is focused local proof, clean preliminary review, one late
`make pre-pr`, attestation verification (or a new full review if invalidated),
then push and PR creation/update. CI must not be the first test of an unproven
change. If local proof is blocked, report the command and cause before
publishing; do not open a speculative PR to discover whether the change works.

## Mandatory Prove-The-Theory-First

Before implementing any change that rests on a performance or behavior theory and
that could slow down or degrade a process, agents MUST first prove the theory
with the cheapest possible shim — a throwaway SQL script with `EXPLAIN ANALYZE`,
a one-off Cypher `PROFILE`/`EXPLAIN`, a microbenchmark, or a scratch query — run
against representative data, BEFORE writing the real change or dispatching an
executor to build it. Proving the theory first is separate from, and comes
before, the Pre-PR Local Proof of the finished change: prove the THEORY, then do
the WORK, then prove the WORK locally.

This gate is mandatory for any change on the accuracy/performance/concurrency
contract, including hot-path Cypher and graph writes, Postgres SQL, schema DDL
and indexes, reducer projection and materialization, queue and lease behavior,
and anything with a repo-scale performance contract. A candidate index, query
rewrite, cache, prefilter, denormalization, or backend knob is a theory until it
is measured — do not build on an unmeasured one. Agents MUST NOT dispatch an
executor, land production code, or open a PR on a theory that has not been
proven, and MUST stop any executor already dispatched on an unproven theory
until the proof lands.

A diagnosis is a theory too: state a cause only with the observation behind it,
and never treat one passing sample as proof. The required proof shape, the
diagnosis rules, and the disproven-theory/PR-acceptance rules are in
[Agent Engineering Guide](docs/internal/agent-guide.md#prove-the-theory-first);
it complements [Evidence Rules](#evidence-rules) and
[Serialization Is Not A Fix](#serialization-is-not-a-fix).

## Runtime Shape

- **API** serves HTTP reads and admin/query surfaces.
- **MCP Server** serves tool-facing read workflows.
- **Ingester** owns repo sync, discovery, parsing, and fact emission.
- **Reducer / Resolution Engine** owns queued projection, repair, and shared
  materialization.
- **Bootstrap Index** owns one-shot local or deployment seeding.
- **Postgres** stores facts, queue state, content, status, and recovery data.
- **NornicDB** is the default canonical graph backend. Neo4j is compatibility
  only when it satisfies Eshu's shared Cypher/Bolt contract.

There is no Python runtime on the normal platform path. Python remains only in
fixture corpora or offline tooling.

## Non-Negotiable Rules

Agents MUST read
[Agent Git And Worktree Hygiene](docs/internal/agent-git-hygiene.md) before any
git, worktree, stash, or push action; it carries the wrong-worktree recovery
procedure and the incident behind each rule below.

- If an edit lands outside the intended feature worktree, agents MUST stop
  immediately, report it, and let the owner decide the recovery. MUST NOT
  self-recover silently.

- MUST use `rg` for all text searches. NEVER use `grep`.
- MUST use `rg --files` or globbing for file discovery. NEVER use `find`.
- Use local docs to establish the relevant contract; read source or external
  documentation as needed to settle the task's actual uncertainty.
- Research questions under settled design intent are work to complete. Use a
  bounded specialist when that adds useful expertise; ask the owner only for
  decisions or information the evidence cannot settle. The detailed guidance
  is [Delegate An Undecided Design](docs/internal/agent-guide.md#delegate-an-undecided-design-do-not-escalate-it).
- Honor authorization already supplied in the conversation for the named acts.
  A request to create a PR includes its necessary branch push, not permission
  to merge or deploy. Ask before external mutations outside that authorization,
  destructive deletion, production data changes, or changing the golden
  standard without explicit approval. Scope approval is not permission to
  perform unrelated acts. Harnesses may persist the owner's grant through
  `CONSENT: <acts>`, `CONSENT: all`, or `CLAUDE_GOAL_CONSENT` per
  [Agent Hooks](docs/internal/agent-hooks.md); never invent or expand a grant.
- Bug fixes require a failing regression test before the fix. New behavior
  needs tests of its contract; refactors need proof that the existing contract
  is preserved. Choose checks that exercise the behavior, not tests that merely
  match implementation wording or harmless documentation edits.
- MUST keep files under 500 lines; split before they approach the limit.
- MUST NOT add AI attribution to commits, PRs, or docs.
- MUST NOT push to `main` or `master`.
- MUST install the repo's pre-commit hooks once per clone
  (`scripts/dev/bootstrap-hooks.sh`) and MUST NOT `--no-verify`. `make pre-pr`
  writes a per-SHA stamp the push requires; a rebase or amend invalidates it.
- MUST create git worktrees before executing plans or PRDs, and MUST verify
  `pwd` is that worktree before any Edit or Write.
- MUST run any tracked-file-mutating command (regenerators, formatters,
  `go mod tidy`) inside a worktree, including for diagnostics. The main checkout
  stays a clean fast-forward of `origin/main`.
- MUST NOT use `git stash` when multiple worktrees may be active — the stash
  stack is shared and concurrent agents corrupt each other. To compare against a
  clean tree use `git diff`, `git show <ref>:<path>`, or a throwaway worktree.
- MUST verify HEAD is on a named branch before every commit
  (`git symbolic-ref -q HEAD`), and confirm the pushed SHA equals local HEAD
  before opening or updating a PR.
- MUST NOT put issue-closing keywords (`Fixes`, `Closes`, `Resolves`, …) in a
  commit message or PR body unless that issue is meant to close on merge.
  Reference issues as `#NNNN` otherwise.
- MUST synchronize remote test machines by Git fetch and checkout of the
  reviewed branch. NEVER `rsync` an unreviewed worktree as performance evidence.
- MUST use the same branch/worktree name across repos when one workflow touches
  multiple repos.
- MUST follow Effective Go for Go, Google Python style for Python fixtures or
  tools, strict typing for TypeScript, HashiCorp Terraform practices, and Helm
  chart best practices.

## Life Motto

Accuracy, performance, and concurrency are the life motto of this repository.
Agents MUST protect all three on every change.

1. **Accuracy:** wrong graph, query, or deployment truth is a product failure.
2. **Performance:** correct behavior must be measured and kept within the
   repo-scale performance contract.
3. **Concurrency:** correctness and performance must hold under the intended
   concurrent worker, queue, graph-write, retry, and lease model.

Agents MUST NOT introduce correctness bugs, unmeasured performance degradation,
or serialized workarounds that hide concurrency defects.

Agents MUST NOT optimize behavior that has not been proven correct. Agents MUST
NOT make a system more reliable by hiding wrong results, swallowing failures,
single-threading work, or inventing silent fallbacks.

## Read These First

Read the sections relevant to the changed contract:

- [Service Runtimes](docs/public/deployment/service-runtimes.md) for service
  ownership, startup, and deployment behavior.
- [Local Testing](docs/public/reference/local-testing.md) for selecting and
  running verification gates.
- [Telemetry](docs/public/reference/telemetry/index.md) for operator signals.
- [Architecture](docs/public/architecture.md) for pipeline and ownership changes.

If a change affects Docker Compose, agents MUST also read
[Docker Compose](docs/public/run-locally/docker-compose.md).

If a change touches hot-path Cypher, graph writes, query handlers, reducer
projection, materialization, or schema DDL, agents MUST also read
[Cypher Performance](docs/public/reference/cypher-performance.md).

If a change affects NornicDB knobs or compatibility, agents MUST also read:

- [NornicDB Tuning](docs/public/reference/nornicdb-tuning.md)
- [NornicDB Pitfalls](docs/public/reference/nornicdb-pitfalls.md)
- [Graph Backend Installation](docs/public/reference/graph-backend-installation.md)

## Skill Routing

Project skills in `.agents/skills/` are the source of truth; `.claude/skills/`
and `.codex/skills/` symlink to them. Select the smallest set that covers the
actual task. Use available names and descriptions for discovery, read a skill
once when it applies, and load its references only for the selected workflow.
Do not reload an unchanged skill for each edit or every status message.

| Task | Skill |
| --- | --- |
| Diagnose unexplained runtime, backend, or queue behavior | `eshu-diagnostic-rigor` |
| Benchmark, optimize, or validate a performance claim | `eshu-performance-rigor` |
| Postgres SQL, schema, transactions, locks, or queue claims | `eshu-postgres-rigor` |
| Go code or tests | `golang-engineering` |
| Cypher, graph queries/writes/indexes, backend dialect | `cypher-query-rigor` |
| Workers, leases, retries, shared state, queue ordering | `concurrency-deadlock-rigor` |
| Correlation, materialization, deployment tracing, query truth | `eshu-correlation-truth` |
| Eshu MCP/API calls or bounded tool contracts | `eshu-mcp-call-rigor` |
| Facts, projected truth, query shapes asserted by B-7; cassettes or B-12 snapshot | `eshu-golden-corpus-rigor` |
| Fact kinds, payload schemas, SDK contracts, registry or fixture packs | `eshu-contract-rigor` |
| Release, version, image, Helm, or GitHub Release | `eshu-release` |
| Package README, doc.go, scoped AGENTS.md | `eshu-folder-doc-keeper` |
| Telemetry contracts, coverage, dashboards, missing signals | `telemetry-coverage-discipline` |
| Generators and committed outputs | `generator-script-discipline` |
| Security-scan workflow or scanner failures | `eshu-security-scan-gates` |
| Issue/epic work explicitly requested through closure | `eshu-issue-driver` |
| Final diff, pre-push review, merge-readiness | `eshu-code-review` |
| Resolve review threads after verified fixes | `resolve-review-threads` |
| Resume, handoff, PR monitoring, liveness, worktree cleanup | `eshu-session-lifecycle` |
| Draft or polish PRs, reviews, issues, docs, or substantial updates | `eshu-humanizer` |

State which skills are active. Routine status messages use the same plain,
evidence-backed house style without loading a writing playbook each time.

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
- MUST include telemetry an operator can use at 3 AM for runtime-affecting
  changes.
- MUST research official documentation before deciding on external SDK,
  database, queue, transaction, and concurrency behavior.

## Evidence Rules

- Bug fixes MUST have a failing regression test first.
- Performance work MUST have before/after measurements.
- Performance issue priority MUST be based on the latest accepted measured
  bottleneck and target contribution budget, not on issue title, old backlog
  severity, or a real-but-small local optimization. Re-rank stale performance
  issues before implementing them.
- Performance comparisons MUST use the same primary start and terminal events,
  corpus/profile/topology, and storage state. Report exact seconds plus a human
  duration and label non-comparable totals instead of manufacturing a speedup.
- End-to-end and collector runs MUST be compared against the last known-good
  named baseline manifest with matching metric boundaries, corpus, profile,
  topology, and storage state. A large regression from that baseline is a bug
  to root-cause, not an acceptable cost, and no long run may be launched
  without a stated time bound.
- Queue/concurrency work MUST have contention, retry, idempotency, ordering, and
  dead-letter proof.
- Performance rewrites that touch a lock/claim/lease/queue path MUST include a
  concurrency proof (contention, EvalPlanQual recheck, or lease-safety), not only
  a row-set equivalence differential, and MUST be re-proven on the built binary
  against the real worst-case backlog, not only a small-N EXPLAIN.
- Graph truth work MUST have fixture intent, reducer graph truth, and API/query
  truth agreement.
- Runtime changes MUST have operator-facing metrics, spans, logs, status, or pprof
  proof.
- Docs-only changes MUST run the docs build gate when navigation or project
  guidance changes.

Agents MUST NOT say work is ready without listing the commands or runtime proof
actually run.

MUST capture exit codes directly (`cmd; echo $?`, never `$?` after a pipe) and
MUST cite verification that postdates the final edit, not an earlier run — see
[Agent Engineering Guide](docs/internal/agent-guide.md#evidence-capture-pitfalls)
for the false-green incidents both rules were learned from.

PRs MUST NOT be accepted on explanation alone. Code changes MUST prove the code
works with focused tests or an integration gate, and runtime-affecting changes
MUST include performance proof or a no-regression measurement for the touched
path.

## Claim Evidence Lives In Known Locations

A dangling evidence pointer is NOT proof of absence. Before downgrading any
capability, maturity, or support claim as "unvalidated" — especially an
outward-facing, marketing-visible one such as a `capability-matrix` support
tier or a `product-claims` maturity — agents MUST exhaustively check the
committed-evidence locations below. A specific proof-ID resolving to nothing
(e.g. a `remote_validation` ref with no artifact) means the pointer was never
wired, NOT that the capability is unvalidated; the evidence usually lives
elsewhere in this list. Downgrading a genuinely-validated claim is a
marketing-damaging false negative.

The evidence found MUST substantiate the specific tier the claim asserts —
this does NOT license retaining a top-tier claim on lower-tier proof. A
`production` / deployed-tier `supported` claim needs deployed evidence: a
committed `docs/internal/remote-validation/<slug>.md` production artifact, a
`scripts/run-remote-e2e-*` / compose driver, or a live-backend
`docs/internal/evidence/*.md` — NOT merely a local unit test that only
exercises a lower profile. When matching-tier evidence genuinely exists,
VALIDATE (wire the pointer to it), keep the claim, and confirm with the owner
before any bulk change. When it does NOT, take the action the remote-validation
contract already mandates: commit the matching deployed-validation artifact, or
downgrade the claim to the tier its committed evidence actually supports. Never
retain a `production:supported` matrix row (or a GA `product-claims` maturity)
whose sole committed evidence is a lower-tier test.

Committed validation evidence lives in:

- `docs/internal/evidence/*.md` — per-issue validation records, including live
  NornicDB Bolt-driver before/after validations of query/graph truth.
- `docs/internal/remote-validation/<slug>.md` — production-validation artifacts
  for capability-matrix `remote_validation` proof-IDs (#5407 gate).
- `go/internal/**/*_test.go` (e.g. `internal/query`, `internal/mcp`) — the
  `go_test` suites the matrix local profiles cite.
- `scripts/run-remote-e2e-*.sh` plus `docs/public/run-locally/docker-compose.*.yaml`
  — deployed / e2e drivers (the matrix `compose_e2e` evidence kind).
- `testdata/cassettes/` and `testdata/golden/e2e-20repo-snapshot.json` (the B-12
  golden snapshot) — replay/golden evidence.

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

MUST keep OpenAPI changes in lockstep with `go/internal/query/openapi*.go`, handler
tests, and [HTTP API Reference](docs/public/reference/http-api.md).

## Verification Defaults

MUST use [Local Testing](docs/public/reference/local-testing.md) as the source
of truth for gates.

After focused local proof and a preliminary full `eshu-code-review` with zero
P0/P1/P2-blocking findings, run `make pre-pr` once, immediately before the intended push
or PR update. It is the one-command local promotion preflight that selects and
runs the credential-free gates required by changed paths; it is not an early
discovery loop. Exactness and race gates are blocking. Use `make pre-pr-full`,
`make frontend-preflight`, and `make security-preflight` for the heavier lanes.
Verify the preliminary review receipt against the exact post-preflight inputs
before push. If verification fails, run a new full `eshu-code-review`. CI stays
authoritative, but MUST NOT be the first place a credential-free failure
appears.

Common checks:

```bash
cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./internal/query ./internal/mcp -count=1
cd go && go test ./internal/parser/... ./internal/collector/discovery ./internal/content/shape ./internal/collector -count=1
cd go && go test ./internal/terraformschema ./internal/relationships -count=1
cd go && go test ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer ./internal/runtime ./internal/status ./internal/storage/postgres -count=1
cd go && golangci-lint run ./...
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Docs, root agent files, and README changes require the docs build plus
`git diff --check`.

The bare `golangci-lint run ./...` above needs the repo's custom `filelength`
and `dirgate` linter plugins built first in a fresh clone or worktree --
otherwise it fails with `plugin.Open` / "unable to load custom analyzer"
naming `tools/golangci-lint-filelength/filelength.so` or
`tools/golangci-lint-dirgate/dirgate.so`. Build both once per clone/worktree
with `cd tools/golangci-lint-filelength && make build` and
`cd tools/golangci-lint-dirgate && make build` (the `.so` files are
gitignored, so this is a per-checkout step, not a one-time repo action).
`scripts/dev/precommit-go.sh lint`/`lint-all` (what `make pre-pr` actually
runs) avoid this entirely by running against a config copy with both plugins
stripped -- see that script's own header for why, and prefer it over the bare
command when you only need the day-to-day check, not a `plugin.Open`
diagnosis.

## Orchestration, PR, And CI Discipline

- For substantive implementation, review, and research, the orchestrator should
  use bounded subagents when independent work or review adds value. Match model
  capability to task difficulty using the tier map in
  [Agent Orchestration Model](docs/internal/agent-orchestration.md#roles-models-and-tools).
  A subagent never downgrades its own model. Leaf agents (executor, debugger,
  reviewer, performance engineer) may not dispatch.
- Only the **orchestrator** runs `make pre-pr`, exactly once, immediately before
  the intended push. Subagents MUST NOT each run it — the full gate is expensive
  and per-agent runs are wasted CPU. They run focused verification only and paste
  it in the handoff. The live gate binds fixed host ports and holds a cross-worktree mutex:
  [serialization and contention](docs/internal/agent-guide.md#live-gate-serialization-and-contention).
- MUST check open PRs and recent commits for the same root cause before starting
  a newly filed issue, and MUST isolate formatter drift into its own commit:
  [duplicate-work and formatter-drift guards](docs/internal/agent-guide.md#duplicate-work-and-formatter-drift-guards).
- Before claiming merge-ready, the PR **title AND description** MUST both match
  the final diff, and the description MUST carry the before/after evidence.
- CI MUST be treated as complete ONLY after **two consecutive stable reads of
  the full check set** (`pending == 0`, total count unchanged) — large sets register in waves,
  so a single `0-pending` read is a false done. State the query used. Reconcile
  the review-thread API against displayed unresolved comments before declaring
  threads clear.

## Pre-Ready Checklist

Every applicable condition must hold before claiming ready. State why a
condition is inapplicable when that is material to the review; do not manufacture
runtime or telemetry work for a prose-only edit.

- Relevant local docs read.
- Relevant project skill used.
- Flow and ownership understood end to end.
- Bug fixes have a failing regression first; other code changes have contract proof.
- Performance impact declared for runtime-affecting work.
- Edge cases and concurrency behavior considered.
- Telemetry or explicit no-observability-change evidence recorded.
- Docs updated for contract changes.
- Focused verification run and cited.
- Code-change PRs prove the code works before review acceptance.
- Runtime PRs include performance proof or no-regression evidence.
- `git diff --check` clean.
