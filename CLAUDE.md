# Eshu Mandatory Agent Rules

Eshu is a self-hosted context graph that connects code, dependencies, supply
chain, infrastructure, and runtime into one queryable, evidence-backed source of
truth for CLI, MCP, and HTTP API workflows. Treat it as a production data
platform, not a script collection.

This file is mandatory. AI agents MUST follow it continuously while working in
this repository. The linked [Agent Engineering Guide](docs/internal/agent-guide.md)
is also mandatory; it is not optional background reading. If a rule here and a
linked detailed rule both apply, follow both. If the correct action is unclear,
stop and ask.

The root agent files (`AGENTS.md` and `CLAUDE.md`) MUST stay in lockstep.

## Mandatory Startup

Before making code or documentation changes, agents MUST:

1. Read this file.
2. Read [Agent Engineering Guide](docs/internal/agent-guide.md).
3. Read [Agent Orchestration Model](docs/internal/agent-orchestration.md) for
   how work is split across harnesses and models (coordinator/executor/debugger
   roles, the handoff contract, and the CI gate floor).
4. Read the local docs named under [Read These First](#read-these-first) when
   the touched surface matches those docs.
5. Load the applicable project skill from `.agents/skills/`.
6. Stop and ask if the correct owner, design intent, performance contract, or
   verification gate is unclear.

Skipping any startup step is not acceptable. Treat these rules as active for the
entire session, not as one-time context.

## Mandatory Pre-PR Code Review

Before creating any PR, pushing changes intended for an existing PR, or marking
an Eshu PR merge-ready, agents MUST run `eshu-code-review` against the final
diff. This applies to separate-context review and self-review. The verdict MUST
include the selected proof tier, all required passes including hostile read,
cross-pass contradiction check, severity/confidence/disposition for every
finding, generated-artifact and private-data scans, verification evidence, and
follow-on issue routing.

PRs MUST NOT be created, updated, pushed, or merged from unreviewed diffs.
Before the expensive `make pre-pr` promotion gate, agents MUST run a preliminary
full `eshu-code-review`. If that review reports any P0, P1, or P2 finding, fix
it, rerun the affected focused verification, and repeat the full review;
`make pre-pr` MUST NOT run until the verdict is P0=0, P1=0, and P2=0.

Once the preliminary review is clean, run `make pre-pr` exactly when the branch
is otherwise ready for its intended push or PR update, then run a final full
`eshu-code-review` against the exact post-preflight diff. If `make pre-pr`
changes tracked or generated files, or the final review finds anything, fix and
reprove the affected surface and repeat the sequence. No code or documentation
edits may occur between the final clean review and push; any diff change
invalidates that verdict.

## Mandatory Pre-PR Local Proof

Before creating any PR, and before pushing changes intended for an existing PR,
agents MUST prove the fix works on the local machine by running the actual
reproduction on the same branch until it demonstrably passes. A change either
works or it does not; agents MUST NOT open a PR to find out.

Speculative "does this work?" PRs are not acceptable. CI is not a test harness
for unproven changes, and opening PRs to discover whether a fix works wastes
CI/CD capacity for little gain.

"Proven locally" means the reproduction that failed before the change now
passes on the branch, run on this machine and cited in the PR body: a bug fix
runs its failing regression test to green, a performance change shows
before/after numbers on the touched path, and a runtime change shows the
observed behavior. Only after that local proof passes do agents complete the
preliminary clean review, run `make pre-pr` immediately before the intended
push, complete the final clean review, and open or update the PR.

For a change that is not fixing a failure — a docs update, a refactor, or a new
feature with no prior repro — local proof is the change's own appropriate
verification, run locally and cited: the docs build for docs/navigation
changes, the tests exercising the new or refactored behavior for a feature or
refactor, and the focused verification selected for the touched surface. The
late `make pre-pr` promotion gate remains reserved for after a preliminary
P0=0/P1=0/P2=0 review. This rule bars opening a PR to discover whether a change
works; it does not require a failing reproduction where none exists.

If the change cannot be proven locally, agents MUST stop and report exactly what
was run and what blocked it, rather than open a PR to find out.

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

A valid proof isolates the theory against representative data — ideally the
worst-case partition or dataset, not the average — and always shows the win:
OLD shape versus NEW shape measured on the same data (for example
`EXPLAIN ANALYZE` timings, `PROFILE` db-hits, or benchmark ns/op). The result
proof then depends on whether the change is meant to alter behavior:

- Output-preserving change (an optimization or rewrite whose results are meant
  to stay identical): also show exact-equivalence — the NEW shape returns
  identical results to the OLD shape (a symmetric set-difference of `0/0`,
  matching row counts, or identical output), so the speedup is not bought by
  changing the answer.
- Behavior change (a correctness or accuracy fix where the old path returned
  wrong graph/query/deployment truth): prove the intended delta instead — the
  NEW output matches the corrected expectation via a failing-then-green
  regression test or an explicit expected-diff, never identity with the old
  wrong output.

A theory that is disproven is a saved implementation, not a failure: record the
result and pick the next candidate. A change of this kind MUST NOT be created,
accepted, pushed, or merged unless the theory proof — the shim/`EXPLAIN`/`PROFILE`/
benchmark commands actually run, their before/after numbers, and the
equivalence or expected-delta check — is recorded alongside the finished
change's local proof. PRs MUST NOT be
accepted on the expectation that a rewrite is faster; the number and the
equivalence MUST be shown. This complements [Evidence Rules](#evidence-rules) and
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

- MUST use `rg` for all text searches. NEVER use `grep`.
- MUST use `rg --files` or globbing for file discovery. NEVER use `find`.
- MUST read local repo docs before searching code or the web.
- MUST ask when intent, architecture, risk, or active design ownership is
  unclear.
- MUST apply TDD when writing or modifying code.
- MUST keep files under 500 lines; split before they approach the limit.
- MUST NOT add AI attribution to commits, PRs, or docs.
- MUST install the repo's pre-commit hooks once per clone
  (`scripts/dev/bootstrap-hooks.sh`; idempotent, shared across worktrees) and
  MUST NOT `--no-verify` a commit. The commit-stage gates are fast; `--no-verify`
  is for push only (the pre-push gosec/e2e gates are slow). CI re-checks every
  gate regardless and is the non-bypassable source of truth.
- MUST NOT push to `main` or `master`.
- MUST synchronize source to remote test machines through Git fetch and
  checkout/fast-forward of the reviewed branch. MUST NOT use `rsync` or copy an
  unreviewed worktree as performance evidence.
- MUST create git worktrees before executing plans or PRDs.
- MUST verify `pwd` matches the intended feature worktree before any Edit or
  Write operation. Run `pwd` and confirm it is the feature worktree path, not
  the main repo checkout. If an edit lands in the wrong path, stop immediately,
  report it, and let the user decide how to recover.
- MUST use the same branch/worktree name across repos when one workflow touches
  multiple repos.
- MUST NOT use `git stash` (or stash pop/apply) when multiple worktrees may be
  active. The stash stack is shared across all worktrees of a repo, so
  concurrent agents stashing in different worktrees corrupt each other's
  uncommitted work. To compare against a clean tree use `git diff`,
  `git show <ref>:<path>`, or a throwaway worktree.
- MUST run any command that mutates a tracked file (regenerators,
  formatters, `go mod tidy`, `go run ./cmd/... -mode generate`, etc.)
  inside a worktree, even for diagnostic or investigative purposes. The
  main checkout must remain a clean fast-forward of `origin/main`
  between merges. A dirty main checkout confuses the next agent and
  makes the user's own uncommitted work look like the agent's. If a
  diagnostic mutation has already leaked into the main checkout, stop,
  run `git restore <file>` against the uncommitted change, fetch, and
  re-apply the equivalent regeneration inside a worktree if the result
  is still needed.
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

Before changing runtime, deployment, ingestion, parsing, graph, queue, or
observability behavior, agents MUST read:

1. [Service Runtimes](docs/public/deployment/service-runtimes.md)
2. [Local Testing](docs/public/reference/local-testing.md)
3. [Telemetry](docs/public/reference/telemetry/index.md)
4. [Architecture](docs/public/architecture.md)

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

Project skills in `.agents/skills/` are the source of truth for Eshu. Agents
MUST inspect the project skill names and descriptions before editing, then load
every project skill whose trigger applies to the touched surface. The short
list below is not exhaustive. The `.claude/skills/` and `.codex/skills/`
directories symlink to those repository-owned skills.

Skipping an applicable skill is a rule violation. If more than one skill
applies, use the minimal set that covers the touched surface and state which
skills are active.

- MUST use `eshu-diagnostic-rigor` for runtime diagnosis, reducer/queue
  attribution, graph backend diagnosis, local/CI proof runs, and evidence.
- MUST add `eshu-performance-rigor` for benchmarks, query/index optimization,
  throughput or bootstrap changes, scaled/remote performance proof, and every
  before/after latency or wall-time claim.
- MUST add `eshu-postgres-rigor` for Postgres SQL, schema DDL, indexes,
  migrations, queue/liveness/status queries, transactions, locks, or relational
  performance diagnostics.
- MUST add `golang-engineering` for Go edits and tests.
- MUST add `cypher-query-rigor` for Cypher, graph query/write/index, or backend
  dialect work.
- MUST add `concurrency-deadlock-rigor` for workers, leases, conflict keys,
  retries, or queue ordering.
- MUST add `eshu-correlation-truth` for correlation, materialization, deployment
  tracing, or query truth.
- MUST add `eshu-mcp-call-rigor` for MCP/API tool calls or bounded
  graph-backed query contracts.
- MUST add `eshu-golden-corpus-rigor` for changes the B-7 golden-corpus gate
  asserts (collector facts, reducer/projector graph output, query/MCP response
  shapes, a new verb/edge/correlation) or any cassette, B-12 snapshot, or gate
  file — keep the cassettes and snapshot (the golden standard) in lockstep.
- MUST add `eshu-release` for release, versioning, image, Helm, and GitHub
  Release work.
- MUST add `eshu-folder-doc-keeper` for package `README.md`, `doc.go`, or
  scoped `AGENTS.md` changes.
- MUST add `eshu-contract-rigor` for a fact-kind add/change, a payload-shape
  change, edits to `sdk/go/factschema` or `sdk/go/collector`, edits to
  `specs/fact-kind-registry.v1.yaml`, fixture packs, or an Odù overlapping a
  cassette or the B-12 snapshot.

## Golden Rules

- MUST understand the relevant flow before editing:
  `sync -> discover -> parse -> emit facts -> enqueue work -> reducer -> graph/content projection -> query surface`.
- MUST fix root cause, not symptoms.
- MUST prove accuracy first, then performance, then concurrency behavior for
  runtime-affecting work.
- MUST account for invalid input, empty state, stale state, partial failure,
  duplicates, retries, ordering, idempotency, concurrency, and rollback.
- MUST preserve package ownership boundaries. The ownership table lives in
  [Agent Engineering Guide](docs/internal/agent-guide.md#service-boundaries).
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

PRs MUST NOT be accepted on explanation alone. Code changes MUST prove the code
works with focused tests or an integration gate, and runtime-affecting changes
MUST include performance proof or a no-regression measurement for the touched
path.

## Claim Evidence Lives In Known Locations

A dangling evidence pointer is NOT proof of absence. Before downgrading any
capability, maturity, or support claim as "unvalidated" — especially an
outward-facing, marketing-visible one such as a `capability-matrix` support
tier or a `product-claims` maturity — agents MUST exhaustively check the
committed-evidence locations below. Downgrading a genuinely-validated claim is
a marketing-damaging false negative: when committed evidence exists, VALIDATE
(wire the pointer to the real evidence) instead of downgrading, and confirm
with the owner before any bulk claim downgrade. A specific proof-ID resolving
to nothing (e.g. a `remote_validation` ref with no artifact) means the pointer
was never wired — NOT that the capability is unvalidated; the evidence usually
lives elsewhere in this list.

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

Document every new or touched exported Go type, interface, function, method,
constant group, and variable with a useful Go doc comment. Placeholder comments
that only repeat the identifier are not acceptable.

Every Go package directory in `go/` has three files: `doc.go`, `README.md`, and
`AGENTS.md`. They serve different audiences:

- `doc.go` for the godoc contract.
- `README.md` for human architecture and operational context.
- `AGENTS.md` for scoped agent instructions that Codex and other harnesses load
  for that directory tree.

Do not remove scoped `AGENTS.md` files unless the replacement is proven to be
loaded by the target harness with the same scope and precedence.

Keep OpenAPI changes in lockstep with `go/internal/query/openapi*.go`, handler
tests, and [HTTP API Reference](docs/public/reference/http-api.md).

## Verification Defaults

Use [Local Testing](docs/public/reference/local-testing.md) as the source of
truth for gates.

After focused local proof and a preliminary full `eshu-code-review` with zero
P0/P1/P2 findings, run `make pre-pr` once, immediately before the intended push
or PR update. It is the one-command local promotion preflight that selects and
runs the credential-free gates required by changed paths; it is not an early
discovery loop. Exactness and race gates are blocking. Use `make pre-pr-full`,
`make frontend-preflight`, and `make security-preflight` for the heavier lanes.
Run the final full `eshu-code-review` on the exact post-preflight diff before
push. CI stays authoritative, but it should not be the first place a
credential-free failure appears.

Common checks:

```bash
cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server ./internal/query ./internal/mcp -count=1
cd go && go test ./internal/parser ./internal/collector/discovery ./internal/content/shape ./internal/collector -count=1
cd go && go test ./internal/terraformschema ./internal/relationships -count=1
cd go && go test ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer ./internal/runtime ./internal/status ./internal/storage/postgres -count=1
cd go && golangci-lint run ./...
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Docs, root agent files, and README changes require the docs build plus
`git diff --check`.

## Pre-Ready Checklist

- Relevant local docs read.
- Relevant project skill used.
- Flow and ownership understood end to end.
- Tests written first for code changes.
- Performance impact declared for runtime-affecting work.
- Edge cases and concurrency behavior considered.
- Telemetry or explicit no-observability-change evidence recorded.
- Docs updated for contract changes.
- Focused verification run and cited.
- Code-change PRs prove the code works before review acceptance.
- Runtime PRs include performance proof or no-regression evidence.
- `git diff --check` clean.
