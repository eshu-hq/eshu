# Agent Engineering Guide

This maintainer-only guide expands the mandatory root `AGENTS.md` and
`CLAUDE.md` rules. Keep root guidance mirrored; put detailed workflow guidance
here or in scoped package docs.

The rules for a touched surface are mandatory. Read the relevant sections when
the task needs them; this guide is not a startup reading list for every edit.

## Operating Standard

Talk to the repo owner like a peer: direct, plain, and specific. Lead with the
result, numbers, and caveats. Define jargon the first time it matters.

For runtime work, the order is fixed:

1. **Accuracy:** persisted facts, graph truth, API/MCP/CLI truth, and fixture
   intent agree.
2. **Performance:** the correct path has a before/after or no-regression
   measurement on the same input shape.
3. **Concurrency:** idempotency, retry boundaries, claim ordering, transaction
   scope, conflict keys, and dead-letter behavior hold under intended worker
   counts.

Use the project skill that matches the touched surface:

- `eshu-correlation-truth` for materialization, deployment tracing, or query
  truth
- `eshu-diagnostic-rigor` for diagnosing unexplained runtime, backend, or queue
  behavior; `eshu-performance-rigor` for measured performance claims
- `eshu-postgres-rigor` for Postgres SQL, schema, indexes, queues, locks,
  transactions, or relational performance diagnostics
- `cypher-query-rigor` for graph query/write/index or backend dialect work
- `concurrency-deadlock-rigor` for workers, leases, retries, queues, or shared
  writes
- `golang-engineering` for Go code edits and Go tests
- `eshu-folder-doc-keeper` for package `README.md`, `doc.go`, or scoped
  `AGENTS.md` changes

## Ownership Boundaries

Do not collapse package ownership casually.

| Area | Owns |
| --- | --- |
| `go/internal/collector/` | Git collection, discovery, snapshotting, parsing inputs |
| `go/internal/parser/` | Parser registry, adapters, language behavior, SCIP support |
| `go/internal/facts/` | Durable fact models and queue contracts |
| `go/internal/storage/postgres/` | Facts, queue, status, content, recovery, decisions |
| `go/internal/storage/cypher/` | Backend-neutral Cypher writes, canonical writers, edge helpers, instrumentation |
| `go/internal/storage/neo4j/` | Neo4j-specific graph adapters |
| `go/internal/projector/` | Source-local projection stages |
| `go/internal/reducer/` | Cross-domain materialization and shared projection |
| `go/internal/relationships/` | Terraform, Helm, Kustomize, Argo extraction |
| `go/internal/query/` | HTTP handlers, OpenAPI, query/read surfaces |
| `go/internal/runtime/` | Admin, status, probes, retry policy, lifecycle |
| `go/internal/status/` | Pipeline and request lifecycle reporting |
| `go/internal/telemetry/` | OTEL tracing, metrics, structured logs |
| `go/internal/truth/` | Canonical truth contracts |

Handlers depend on ports such as `GraphQuery`, `GraphWrite`, and
`ContentStore`, not concrete backend drivers. Backend behavior belongs only in
documented seams: schema DDL, runtime settings, retry classification, query
builders, and measured adapters.

## Runtime Contract

| Runtime | Responsibility | Command |
| --- | --- | --- |
| API | HTTP API, admin/query reads | Helm: `eshu api start`; direct binary: `/usr/local/bin/eshu-api` |
| MCP Server | MCP tool server | Helm: `eshu mcp start --transport http`; Compose/direct binary: `/usr/local/bin/eshu-mcp-server` |
| Ingester | Repo sync, parsing, fact emission | `/usr/local/bin/eshu-ingester` |
| Reducer | Queue drain, graph projection, repair flows | `/usr/local/bin/eshu-reducer` |
| Bootstrap Index | One-shot initial indexing | `/usr/local/bin/eshu-bootstrap-index` |

The direct service binaries are the support/version-check artifacts. Helm starts
API and MCP through the `eshu` CLI wrapper; Compose starts MCP through
`/usr/local/bin/eshu-mcp-server`.

Before local runtime validation that executes Eshu binaries, rebuild first:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

`eshu graph start` discovers helper binaries through `PATH`.

## Code Change Workflow

Use TDD for bugs:

1. Write the failing regression test.
2. Run the focused test and confirm the expected failure.
3. Fix the right ownership boundary.
4. Rerun the focused test.
5. Add edge-case coverage for retries, ordering, idempotency, concurrency, or
   rollback when relevant.
6. Run the smallest package or integration gate that proves the contract.

Use this root-cause shape:

1. Gather evidence.
2. Form hypotheses.
3. Prove or disprove likely causes.
4. Fix the actual failure mode.
5. Add regression coverage and telemetry when runtime behavior changed.

### Never `git stash` across concurrent worktrees

The git stash stack is shared across every worktree of a repository, not
isolated per worktree. When two agents work in separate worktrees and both run
`git stash`, their stashes share one stack and collide. We hit this in
practice: two feature worktrees had their uncommitted changes symmetrically
swapped (cloud and images change-sets traded places), discovered only at commit
time. No data was lost because both change-sets were clean and recoverable by
committing to the correct branch, but it cost real time and risked a bad merge.

Do not use `git stash`, `git stash pop`, or `git stash apply` in this
repository when more than one worktree may be active. To compare against a clean
tree, use `git diff`, `git show <ref>:<path>`, or a throwaway worktree instead.

### Rebasing a sibling branch after a squash-merge

PRs land via squash-merge, so a merged PR's commits are not ancestors of a
sibling branch that shares files with it. When PR A merges and sibling PR B
still touches some of the same files, rebase B onto fresh `origin/main`:

1. `git fetch origin main` first. Worktrees here are often shallow clones; a
   stale or shallow `origin/main` can make `git rebase` explode into hundreds of
   spurious conflicts (empty merge-base). If that happens, abort, confirm the
   real divergence (`git log --oneline origin/main..HEAD`, or a `gh` compare),
   `git fetch --deepen=25`, then rebase.
2. `git rebase origin/main`. The true merge-base stays clean, so conflicts
   appear only in files BOTH PRs edited — resolve them keeping **both** sides
   (e.g. PR A's new flag AND PR B's new flag), then `git rebase --continue`.
3. Validate the COMBINED result — run the relevant gate before pushing. The
   rebase merged two independent changes that were each only proven alone, so a
   green result on either branch in isolation does not prove the merge.
4. Force-push the rebased branch. When pushing through an ad-hoc credential-helper
   URL (no configured remote, so no remote-tracking ref) `--force-with-lease`
   fails with "stale info"; confirm the remote tip with
   `git ls-remote <url> <branch>` first, then push with plain `--force`.

## Performance And Evidence

Performance work needs a written impact declaration before implementation:
stage, cardinality, hot path, baseline or known-normal timing, proof ladder,
and stop threshold.

Capture before/after data with the same benchmark, trace, metric sample,
runtime status report, or Compose proof. For full-corpus and remote proof,
report these separately:

- collector stream complete
- projection or bootstrap complete
- queue-zero

Hot-path changes touching Cypher, graph writes, reducers, projectors, queues,
workers, leases, batching, runtime stages, collectors, Compose, Helm, pprof, or
NornicDB knobs must update a tracked repo file with one benchmark marker and
one observability marker:

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`
- `Observability Evidence:`
- `No-Observability-Change:`

PR text alone is not proof. Review acceptance requires the exact command, run,
or measurement that proves the changed behavior.

The five evidence markers above are the per-PR audit trail. The
**telemetry discipline** — the X1 contract doc, X2 verifier, X3 CI gate,
and X4 operator dashboard — is the machine-enforced link between the
markers and a runnable signal. See
[`docs/internal/telemetry-discipline-precedent.md`](telemetry-discipline-precedent.md)
for the failure class the discipline prevents, the historical incidents
(#3633 and earlier), and the contributor runbook for adding a new
`eshu_dp_*` metric or a new pipeline stage. The CHANGELOG entry under
"Unreleased" summarizes the four artifacts and the cross-link to the
historical incidents.

### Prove The Theory First

The root `AGENTS.md`/`CLAUDE.md` mandates proving a performance or behavior
theory with the cheapest possible shim, against representative data, BEFORE
writing the real change or dispatching an executor to build it — see that
gate's text for scope (hot-path Cypher/graph writes, Postgres SQL, schema
DDL/indexes, reducer projection/materialization, queue/lease behavior, or any
repo-scale performance contract) and the executor-dispatch/PR-opening bar it
sets. This is the proof shape that gate requires:

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
change's local proof. PRs MUST NOT be accepted on the expectation that a
rewrite is faster; the number and the equivalence MUST be shown.

### Diagnosis Is A Theory Too

Prove-The-Theory-First is usually read as covering performance work before
implementation. It also covers WHY something broke. A diagnosis written with the
confidence of a finding gets built on: an executor is dispatched, a fix is
written, and the fix does nothing because the cause was never established.

Rules:

- State a cause only alongside the observation that establishes it. "The node is
  present in all four runs and the edge is absent in three" is an observation.
  "The two stages race" is a conclusion, and without the observation it is a
  guess.
- Label an unproven cause as unproven in the sentence itself, not in a caveat
  further down. A reader cannot recover the difference from confident phrasing.
- Do NOT put an unproven hypothesis into a subagent's prompt. A dispatched agent
  looks for confirmation of whatever it was handed. Give it the symptom and the
  evidence, and let it find the cause.
- Check the evidence already on disk before theorizing. Logs, prior gate runs,
  and captured output routinely contain the answer while a theory is being built
  around them.
- Re-state the confidence when a claim is repeated. A guess restated reads as
  established fact unless it is re-labelled.

**One passing sample is not proof of a behavior.** Sampling once and reporting
the result as the behavior cannot distinguish "works" from "works sometimes".
Where a result could be intermittent, sample until the rate is known and report
the rate. A feature reported as working on one green run turned out to work in
one run of four (#5717); the green run was the outlier.

Evidence docs that assert a cause carry a `Root-Cause Evidence:` marker naming
the observation, checked by `scripts/verify-root-cause-evidence.sh`. That gate
verifies the evidence was written down, never that it is sound, and it cannot
see a cause asserted in a PR description or a chat message. The rules above are
the substance; the gate is the part a script can reach.

### Evidence Must Come From The Failing Run

Do not reason about a failure using state captured from a different run, even on
the same commit. A run that succeeded proves nothing about the one that failed.

Investigating an intermittent golden-corpus assertion (#5717), three observations
— edge present, assertion passes by hand, every work item succeeded — all came
from stacks belonging to runs that had NOT failed; the failing runs were torn
down on exit with their evidence. An hour of confident reasoning followed, and
its conclusion was unsupported.

When a harness destroys state on exit, capture what you need DURING the failing
run (`--keep`, a poller, a dump) or accept that you have none. Before citing an
artifact, confirm which run produced it.

### Do Not Defer, And Verify The Wiring

"Deserves a fresh start" and "leave it for later" are how a known defect becomes
someone else's surprise. Fix it now, or name what blocks it.

A function nobody calls is dead code, and a green unit test does not prove the
feature runs. A formatter with passing tests and no caller shipped here; only a
full-package build caught it.

### Delegate An Undecided Design, Do Not Escalate It

Establish ownership, design intent, performance contract, and verification
requirements from the task and available evidence. Ask if those remain
unsettled; dispatching another agent cannot authorize an unowned decision.

Once those are settled, remaining technical uncertainty is a research task.
Investigate directly or use a bounded specialist when independent reasoning
would help resolve contradictory evidence. Select capability through the active
runtime per [Agent Orchestration Model](agent-orchestration.md#roles-models-and-tools).
Product and business trade-offs that evidence cannot settle go to the owner.

Give it the symptom and raw observations, never your hypothesis. Twice in one
session a Deep-tier investigation rejected the framing it was handed and found
the real mechanism — which worked only because the framing was labelled a guess.

### Test Filters Fail Silently

`-run` and its equivalents are case-sensitive and match zero tests on a typo,
which reads exactly like a pass. `-run 'IacInventory'` matched nothing where the
test was `IaCInventory`, and "tests pass" was reported on an empty run. Count the
tests that ran.

### Diff-Scoped Gates Default To HEAD~1

Gates scoped to "the diff" compute it against `HEAD~1` unless given a base, so
a multi-commit branch shows only its last commit. Export
`ESHU_{PARSER_RELATIONSHIP_KIT,PERFORMANCE_EVIDENCE,MEASUREMENT_CITATIONS}_BASE=origin/main` before `make pre-pr`.

These fail in different directions: `parser-relationship-kit` false-FAILS
loudly; `verify-performance-evidence` and `verify-measurement-citations`
false-PASS silently, examining nothing. Assume other members exist.

### Evidence Capture Pitfalls

Two rules about how proof is captured, both learned from real false greens:

- **Capture exit codes directly.** `cmd; echo $?` — NEVER `$?` after a pipe.
  `cmd | tail; echo $?` reports **tail's** status, not `cmd`'s, so a failing gate
  reads as exit 0. This shipped a security-gate "exit 0" claim into a commit
  message when `npm audit` had actually exited 1. When output must be trimmed,
  redirect first (`cmd >out.txt 2>&1; echo $?`) and read the file afterward.
- **Cited verification MUST postdate the final edit.** Re-run the full selected
  set after the LAST change, not the subset judged relevant to it. A config value
  changed late in a review cycle broke the test asserting that value; only the
  package touched most recently was re-run, and the commit message reported the
  untouched suite green. If a claim was measured before the last edit, it is not
  evidence — it is a memory.

### Live-Gate Serialization And Contention

`verify-golden-corpus-gate.sh` binds fixed host ports (Postgres, api, mcp) and a
compose project derived from the worktree name. Two runs from different
worktrees do not fail cleanly on a port bind — they contend for CPU and Docker
I/O, and the loser surfaces as a drain that never reaches terminal
(`fact_work_items_residual: residual=1 (dead_letter=1)`), which reads exactly
like a real reducer defect and costs a long investigation in the wrong place.
The script now holds a cross-worktree mutex (`scripts/lib/live-gate-lock.sh`)
and refuses to start alongside a live run. That mutex covers THAT script,
within one clone: port disjointness is NOT safety, since the contention is CPU
and Docker I/O and another Docker-heavy gate starves it even on different
ports, and a second clone of the repo has its own lock. When dispatching a
fleet, serialize the live gates and hand the machine over explicitly rather
than letting agents self-schedule — this is why subagents/teams MUST NOT each
run `make pre-pr`.

Before declaring an intermittent gate failure a flake, MUST rule out resource
contention first: check the load average and what else is running
(`pgrep -f 'make pre-pr|verify-golden'`). Contention shows up as false
FAILURES, not as false passes — so a gate that failed under load has proven
nothing, while one that passed under load is usually trustworthy (the
exception being an assertion whose own timing budget the load inflated).
Re-running without changing the conditions is not evidence.

### Duplicate-Work And Formatter-Drift Guards

Before starting work on a newly filed issue, MUST check whether it is already
being fixed: search open PRs and recently merged commits for the same root
cause. An entire dependency migration was rebuilt in this repo while an
equivalent fix was already in flight.

When a change touches files carrying **pre-existing formatter drift**, isolate
the reformat in its own commit. The staged-file format hooks only inspect what
is staged, so old drift stays invisible until an unrelated change touches those
files — and then a one-line-per-file edit arrives as thousands of reformatted
lines. Do NOT run the formatter across the whole changed set and commit it as
one blob; commit the pure reformat first (stating that it is formatting-only
and verifiable with `--list-different`), then the real change on top. That
keeps the reviewable diff reviewable. Never `--no-verify` past a format hook.

## Measurement Ledger

Cite a `docs/internal/measurements.jsonl` row id (`ledger:<id>`) instead of
restating a number; `scripts/verify-measurement-citations.sh` enforces it.
See [Measurement Ledger](measurement-ledger.md) for schema and gate details.

## API, MCP, And Query Reads

Potentially expensive reads must be scoped, cancellable, observable, and cheap
to fail:

- Resolve canonical scope first.
- Require `limit`, timeout, deterministic ordering, and `truncated`.
- Run a cheap local MCP preflight before graph-backed calls.
- Prefer summary/count/handles first, payload second.
- Keep high-volume metadata out of graph hot paths unless measured.
- Classify slow calls before retrying.

Runtime modes with different performance profiles require explicit opt-in.

## Concurrency

Before changing workers, leases, retries, queues, transactions, or shared graph
writes, describe:

- shared state and conflict domains
- lock or claim ordering
- transaction scope
- retry scope
- idempotency keys
- starvation and write-amplification risks
- dead-letter behavior

Do not ship serialization as a concurrency fix. Worker-count reductions,
single-threaded drains, disabled concurrent writers, or batch size `1` are
diagnostics unless architecture-owner approval and tracked evidence prove a
permanent serial path is within the performance contract.

## Bootstrap And Correlation Truth

The bootstrap-index orchestrator runs a facts-first pipeline:

```text
Phase 1 - Collection + first-pass reduction
Phase 2 - Backfill relationship evidence
Phase 3 - Reopen deployment_mapping
Phase 4 - Second-pass consumers of resolved_relationships
```

Any domain that consumes `resolved_relationships` needs a post-Phase-3 reopen
or re-trigger mechanism.

Correlation and materialization changes must prove:

- raw evidence -> candidate -> admission -> projection row -> graph write ->
  query surface
- positive, negative, and ambiguous cases
- what materializes and what remains provenance-only
- utility, controller, deployment, and ambiguous multi-unit repositories
- fresh rebuild/restart before blaming timing

Namespace, folder, or repo-name heuristics must not invent environment or
platform truth without explicit environment aliases or stronger deployment
evidence.

## Observability

Every runtime-affecting code change must include telemetry or a
`No-Observability-Change:` marker naming existing signals.

| Change type | Required telemetry |
| --- | --- |
| New pipeline stage or worker | OTEL span, duration histogram, success/failure counter |
| New Postgres or graph query | Duration histogram and error counter |
| New queue consumer | Claim duration, processing duration, depth gauge |
| New retry/skip path | Counter with reason label and structured log |
| Memory/resource tuning | Observable configured-limit gauge |
| Batch processing | Batch-size histogram and committed counter |

Metrics live in `go/internal/telemetry/instruments.go`; names use the
`eshu_dp_` prefix. New dimensions and span/log names go in
`go/internal/telemetry/contract.go`. High-cardinality values belong in spans or
logs, not metric labels.

## Cypher And NornicDB

For non-trivial Cypher work, read the current NornicDB hot-path cookbook,
failing query shapes, and relevant `pkg/cypher/*hotpath*_test.go` files before
proposing a change. State the production executor path or fast path and how the
change engages it.

When Eshu hits a NornicDB incompatibility, check upstream source before
guessing. If NornicDB supports the behavior, fix Eshu. If it needs a
workaround, use a documented backend seam. If NornicDB must be patched, land an
evidence-backed fix in the maintained fork, rebuild, and pin the binary until
upstream absorbs it.

Speculative NornicDB throughput patches must be reverted.

## Documentation Workflow

Every changed Go package under `go/internal` or `go/cmd` must carry `doc.go`,
`README.md`, and package-local `AGENTS.md`.

- `doc.go` is the godoc contract.
- `README.md` is human architecture and operational context.
- `AGENTS.md` is harness-loaded scoped instruction for agents working in that
  directory tree.

Use `eshu-folder-doc-keeper` when code moves and package docs drift. The package
docs gate is:

```bash
scripts/test-verify-package-docs.sh
scripts/verify-package-docs.sh
```

Collector authoring changes also need:

```bash
scripts/test-verify-collector-authoring-gate.sh
scripts/verify-collector-authoring-gate.sh
```

## Remote Build Hygiene

When rebuilding Go projects over non-interactive SSH, do not assume the remote
shell loads the same `PATH`. Check `command -v go` and common absolute paths.
Keep hostnames, IPs, private key paths, and machine-specific repo paths out of
open-source docs.
