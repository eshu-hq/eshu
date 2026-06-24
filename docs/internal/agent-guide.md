# Agent Engineering Guide

This maintainer-only guide expands the mandatory root `AGENTS.md` and
`CLAUDE.md` rules. Keep root guidance mirrored; put detailed workflow guidance
here or in scoped package docs.

This guide is mandatory for agents. It is not optional background reading.

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
- `eshu-diagnostic-rigor` for runtime proof, reducer throughput, queue behavior,
  or performance evidence
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

For non-trivial Cypher work, read the current NornicDB-New hot-path cookbook,
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
