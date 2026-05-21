# Agent Engineering Guide

This maintainer-only guide carries the detailed rules behind the root
`AGENTS.md` and `CLAUDE.md` entrypoints. Keep the root files short and mirrored;
put expanded workflow detail here or in package READMEs.

## Communication

Talk to the repo owner like a peer. Use direct, plain language. Define jargon
the first time it matters. Lead status updates with the result, then numbers,
then caveats. Keep code comments and docs precise, not chatty.

## Life Motto Enforcement

Accuracy, performance, and concurrency are mandatory for runtime work.

- Accuracy proof comes first: fixture intent, persisted truth, graph truth, and
  API/MCP/CLI truth must agree for the touched behavior.
- Performance proof comes second: capture before/after or no-regression
  measurements on the same shape, and record evidence in a versioned repo file
  when a hot path is touched.
- Concurrency proof comes third: validate idempotency, retry boundaries, claim
  ordering, transaction scope, conflict keys, dead-letter behavior, and that no
  shipped worker knob hides the real design problem.

Agents MUST use the correct project skill for each of those dimensions:
`eshu-correlation-truth` for truth, `eshu-diagnostic-rigor` for performance and
runtime proof, `cypher-query-rigor` for Cypher/graph paths, and
`concurrency-deadlock-rigor` for workers, leases, queues, retries, or shared
writes.

## Service Boundaries

Do not collapse ownership boundaries casually.

| Area | Owns |
| --- | --- |
| `go/internal/collector/` | Git collection, discovery, snapshotting, parsing inputs |
| `go/internal/parser/` | Parser registry, adapters, language behavior, SCIP support |
| `go/internal/facts/` | Durable fact models and queue contracts |
| `go/internal/storage/postgres/` | Facts, queue, status, content, recovery, decisions |
| `go/internal/storage/cypher/` | Backend-neutral Cypher write contracts, canonical writers, edge helpers, instrumentation |
| `go/internal/storage/neo4j/` | Neo4j-specific graph adapters |
| `go/internal/projector/` | Source-local projection stages |
| `go/internal/reducer/` | Cross-domain materialization and shared projection |
| `go/internal/relationships/` | Terraform, Helm, Kustomize, Argo extraction |
| `go/internal/query/` | HTTP handlers, OpenAPI, query/read surfaces |
| `go/internal/runtime/` | Admin, status, probes, retry policy, lifecycle |
| `go/internal/status/` | Pipeline and request lifecycle reporting |
| `go/internal/telemetry/` | OTEL tracing, metrics, structured logs |
| `go/internal/truth/` | Canonical truth contracts |

Handlers depend on ports such as `GraphQuery` and `GraphWrite`, not concrete
backend implementations. Backend dialect differences belong only in documented
seams such as schema DDL, runtime settings, retry classification, and query
builders.

## Runtime Contract

| Runtime | Responsibility | Command | Kubernetes shape |
| --- | --- | --- | --- |
| API | HTTP API, admin/query reads | `eshu api start --host 0.0.0.0 --port 8080` | `Deployment` |
| MCP Server | MCP tool server | `eshu mcp start` | `Deployment` or sidecar |
| Ingester | Repo sync, parse, fact emission | `/usr/local/bin/eshu-ingester` | `StatefulSet` + PVC |
| Reducer | Queue drain, graph projection, repair flows | `/usr/local/bin/eshu-reducer` | `Deployment` |
| Bootstrap Index | One-shot initial indexing | `/usr/local/bin/eshu-bootstrap-index` | job / init step |

For local runtime validation that executes local binaries, rebuild first:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

`eshu graph start` discovers helper binaries through `PATH`.

## TDD And Debugging

For bugs:

1. Write a failing test that reproduces the exact failure.
2. Run the focused test and confirm the expected failure.
3. Fix the right ownership boundary.
4. Rerun the focused test.
5. Add edge-case coverage for retries, ordering, idempotency, or concurrency
   when relevant.
6. Run the smallest package or integration gate that proves the contract.

Root-cause workflow:

1. Gather evidence.
2. Form hypotheses.
3. Prove or disprove likely causes.
4. Fix the actual failure mode.
5. Add regression coverage and telemetry when runtime behavior changed.

## Performance

Performance work must show measured value. Before implementation, write a
performance impact declaration naming the stage, cardinality, expected hot path,
baseline or known-normal timing, proof ladder, and stop threshold.

Capture before/after data with the same benchmark, trace, metric sample,
runtime status report, or Compose proof. For full-corpus or remote proof,
report collector stream complete, projection/bootstrap complete, and queue-zero
separately.

Hot-path changes that touch Cypher, graph writes, reducers, projectors, queues,
workers, leases, batching, runtime stages, collectors, Compose, Helm, pprof, or
NornicDB knobs must update a tracked repo file with one evidence marker and one
observability marker:

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`
- `Observability Evidence:`
- `No-Observability-Change:`

PR text alone is not enough.

Review acceptance requires proof, not confidence. For code changes, cite the
focused test or integration gate that proves the changed behavior works. For
runtime-affecting changes, cite performance evidence or no-regression evidence
from the same input shape before accepting the PR.

## MCP And API Reads

Potentially expensive reads must be scoped, cancellable, observable, and cheap
to fail:

- resolve canonical scope first
- require `limit`, timeout, deterministic ordering, and `truncated`
- run a cheap local MCP preflight before graph-backed calls
- prefer summary/count/handles first, payload second
- keep high-volume metadata out of graph hot paths unless measured
- classify slow calls before retrying

Runtime modes with different performance profiles require explicit opt-in.

## Concurrency

Before changing workers, leases, retries, queues, transactions, or shared graph
writes, describe shared state, lock/claim ordering, transaction scope, retry
boundaries, idempotency keys, conflict domains, starvation risks, write
amplification, and dead-letter behavior.

Research actual Postgres, Neo4j, NornicDB, or Go runtime behavior before
deciding. If a path must be concurrent, fix the design with idempotent writes,
conflict-key partitioning, or a measured redesign. Do not ship serialization as
a concurrency fix.

## Facts-First Bootstrap Ordering

The bootstrap-index orchestrator in `go/cmd/bootstrap-index/main.go` runs a
multi-pass pipeline:

```text
Phase 1 - Collection + First-Pass Reduction
Phase 2 - Backfill relationship evidence
Phase 3 - Reopen deployment_mapping
Phase 4 - Second-pass consumers of resolved_relationships
```

Any domain that consumes `resolved_relationships` needs a post-Phase-3 reopen
or re-trigger mechanism.

## Correlation Truth

Use `eshu-correlation-truth` whenever a change touches workload admission,
deployable-unit correlation, materialization, deployment tracing, or query truth
in reducer, query, graph, relationships, or correlation fixtures.

Required proof:

- raw evidence -> candidate -> admission -> projection row -> graph write ->
  query surface
- positive, negative, and ambiguous cases
- what materializes and what remains provenance-only
- utility, controller, deployment, and ambiguous multi-unit repositories
- fresh rebuild/restart before blaming timing
- fixture intent, reducer graph truth, and API/query truth agreement

Namespace, folder, or repo-name heuristics must not invent environment or
platform truth without explicit environment aliases or stronger deployment
evidence.

## Observability

Every runtime-affecting code change must include telemetry or a clear
`No-Observability-Change:` marker that names existing signals.

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
uses and how your change engages it.

When Eshu hits a NornicDB incompatibility, check upstream source before
guessing. If NornicDB supports the behavior, fix Eshu. If it needs a workaround,
use a documented backend seam. If NornicDB must be patched, land an
evidence-backed fix in the maintained fork, rebuild, and pin the binary until
upstream absorbs it.

Eshu maintainers may patch NornicDB only for:

- a correctness fix in NornicDB itself
- a measured NornicDB performance win that generalizes
- a measured Eshu runtime win proven by focused and corpus-level evidence

Speculative throughput patches must be reverted.

## Documentation Workflow

Every changed Go package under `go/internal` or `go/cmd` must carry `doc.go`,
`README.md`, and package-local `AGENTS.md`. The scoped `AGENTS.md` file is not
reader-facing prose; it is harness-loaded instruction for Codex and other coding
agents working inside that directory tree.

Use `eshu-folder-doc-keeper` when code moves and package docs drift. Use:

```bash
scripts/check-docs-stale.sh --all
scripts/verify-doc-claims.sh go/internal/<pkg>
```

The package docs gate is:

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
