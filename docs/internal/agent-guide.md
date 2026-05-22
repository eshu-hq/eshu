# Agent Engineering Guide

This maintainer-only guide expands the mandatory root `AGENTS.md` and
`CLAUDE.md` rules. Keep the root files short and mirrored; put detailed
workflow guidance here or in scoped package docs.

This guide is mandatory for agents. It is not optional background reading.

## Operating Standard

Talk to the repo owner like a peer: direct, plain, and specific. Lead with the
result, then the numbers, then caveats. Define jargon the first time it matters.
Keep code comments and docs precise.

For runtime work, the order is fixed:

1. **Accuracy:** persisted facts, graph truth, API/MCP/CLI truth, and fixture
   intent agree.
2. **Performance:** the correct path has a before/after or no-regression
   measurement on the same input shape.
3. **Concurrency:** idempotency, retry boundaries, claim ordering, transaction
   scope, conflict keys, and dead-letter behavior hold under intended worker
   counts.

Use the project skill that matches the surface:

- `eshu-correlation-truth` for materialization, deployment tracing, or query
  truth
- `eshu-diagnostic-rigor` for runtime proof, reducer throughput, queue behavior,
  or performance evidence
- `cypher-query-rigor` for graph query/write/index or backend dialect work
- `concurrency-deadlock-rigor` for workers, leases, retries, queues, or shared
  writes
- `golang-engineering` for Go code edits and Go tests

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
`ContentStore`, not concrete backend drivers. Backend-specific behavior belongs
only in documented seams: schema DDL, runtime settings, retry classification,
query builders, and measured adapters.

## Runtime Contract

| Runtime | Responsibility | Command |
| --- | --- | --- |
| API | HTTP API, admin/query reads | `eshu api start --host 0.0.0.0 --port 8080` |
| MCP Server | MCP tool server | `eshu mcp start` |
| Ingester | Repo sync, parsing, fact emission | `/usr/local/bin/eshu-ingester` |
| Reducer | Queue drain, graph projection, repair flows | `/usr/local/bin/eshu-reducer` |
| Bootstrap Index | One-shot initial indexing | `/usr/local/bin/eshu-bootstrap-index` |

For local runtime validation that executes local binaries, rebuild first:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

`eshu graph start` discovers helper binaries through `PATH`.

## Code Change Workflow

For bugs, use TDD:

1. Write the failing regression test.
2. Run the focused test and confirm the expected failure.
3. Fix the right ownership boundary.
4. Rerun the focused test.
5. Add edge-case coverage for retries, ordering, idempotency, concurrency, or
   rollback when relevant.
6. Run the smallest package or integration gate that proves the contract.

For root cause:

1. Gather evidence.
2. Form hypotheses.
3. Prove or disprove likely causes.
4. Fix the actual failure mode.
5. Add regression coverage and telemetry when runtime behavior changed.

## Performance And Evidence

Performance work needs a written impact declaration before implementation. Name
the stage, cardinality, hot path, baseline or known-normal timing, proof ladder,
and stop threshold.

Capture before/after data with the same benchmark, trace, metric sample, runtime
status report, or Compose proof. For full-corpus and remote proof, report these
separately:

- collector stream complete
- projection or bootstrap complete
- queue-zero

Hot-path changes that touch Cypher, graph writes, reducers, projectors, queues,
workers, leases, batching, runtime stages, collectors, Compose, Helm, pprof, or
NornicDB knobs must update a tracked repo file with one benchmark marker and one
observability marker:

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`
- `Observability Evidence:`
- `No-Observability-Change:`

PR text alone is not proof. Review acceptance requires the exact command,
runtime run, or measurement that proves the changed behavior.

## API, MCP, And Query Reads

Potentially expensive reads must be scoped, cancellable, observable, and cheap
to fail.

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
diagnostics unless a design record proves the serial path is permanent and
within the performance contract.

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
proposing a change. State which executor path or fast path the production query
uses and how the change engages it.

When Eshu hits a NornicDB incompatibility, check upstream source before
guessing. If NornicDB supports the behavior, fix Eshu. If it needs a workaround,
use a documented backend seam. If NornicDB must be patched, land an
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
