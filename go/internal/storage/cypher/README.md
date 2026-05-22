# storage/cypher

## Purpose

`storage/cypher` owns backend-neutral graph write contracts for Eshu. It builds
Cypher `Statement` values, canonical node and edge writers, execution wrappers,
and write instrumentation that both Neo4j and NornicDB must satisfy through the
same `Executor` seam.

## Ownership boundary

This package owns source-local write planning, canonical node writes,
reducer-owned shared projection edges, semantic entity writes, statement
metadata, batching, retry/timeout wrappers, and graph-write telemetry.

It does not own driver sessions, backend startup, schema DDL, runtime backend
selection, or query behavior. Runtime wiring and storage adapter packages own
those seams. Writers and callers must not branch on `ESHU_GRAPH_BACKEND`.

## Exported surface

Use `go doc ./internal/storage/cypher` for the complete exported contract. The
main roles are:

- Planning: `Statement`, `Plan`, `BuildPlan`, and `Operation`.
- Canonical nodes: `CanonicalNodeWriter` and `BuildCanonical*` builders.
- Shared edges: `EdgeWriter`, domain row mapping, and `DefaultBatchSize`.
- Semantic entities: `SemanticEntityWriter` constructors.
- Execution seams: `Executor`, `GroupExecutor`, `PhaseGroupExecutor`, and
  `ExecuteOnlyExecutor`.
- Wrappers and diagnostics: `TimeoutExecutor`, `RetryingExecutor`,
  `InstrumentedExecutor`, `GraphWriteTimeoutError`, and retry helpers.
- Reads/checks: `CypherReader` and `CanonicalNodeChecker`.

## Dependencies

- `internal/graph` supplies source-local materialization records.
- `internal/projector` supplies canonical materialization rows.
- `internal/reducer` supplies reducer domain constants and shared projection
  rows.
- `internal/telemetry` supplies instruments, spans, and bounded attributes.

## Telemetry

Operators diagnose this package through write spans, phase duration metrics,
batch-size metrics, retry counters, grouped shared-edge metrics, code-call edge
batch metrics, and canonical projection/retract metrics. Keep high-cardinality
values such as paths, symbols, fact IDs, and raw query text out of metric
labels.

Structured logs must name phase, domain, evidence source, execution mode, row
counts, route count, statement count, batch size, duration, failure class, and
bounded statement summaries where useful.

## Gotchas / invariants

- `CanonicalNodeWriter.Write` phase order is a correctness contract:
  retractions, repository cleanup, repositories, directories, files, entities,
  entity retractions, containment, Terraform state, OCI registry, package
  registry, modules, then structural edges.
- Repository cleanup must finish before repository `MERGE`; directories before
  nested files; files before entity containment; current entity upserts before
  stale entity cleanup.
- Hot-path writes must be idempotent and retry-safe. Use `MERGE` for identity
  and split mutable properties into `SET`.
- Do not serialize workers to hide graph races. Fix idempotency, retry
  commit-time conflicts, or redesign the conflict domain.
- Dynamic endpoint labels must come from package-owned allowlists.
- Non-repository collectors must not issue repo-bound file, directory, or
  entity cleanup.
- OCI and package registry rows keep digest/uid-backed identity. Mutable tags
  and source repository hints stay weak evidence until reducer correlation
  admits stronger truth.
- Code-call rows can write `CALLS`, `REFERENCES`, or `USES_METACLASS`; Go and
  TypeScript type references must stay `REFERENCES`.
- SQL trigger rows must keep both `TRIGGERS` and `EXECUTES` write/retract
  support so trigger-bound stored routines are reachable for dead-code checks.

## Evidence kept here

Performance Evidence: the 2026-05-21 OCI registry graph-write investigation
showed multi-label OCI node `MERGE` paths were fast while relationship writes
timed out against the populated NornicDB graph. The canonical OCI writer keeps
digest-backed image-family node identity and skips OCI relationship writes until
a measured relationship writer exists.

No-Regression Evidence: `go test ./internal/storage/cypher -count=1` covers
phase order, repository cleanup, file shapes, typed code-call and SQL endpoints,
OCI/package/Terraform rows, retry classification, timeout wrapping, and
instrumentation contracts.

Observability Evidence: existing canonical phase metrics, projector stage
metrics, workflow/fact work-item rows, and structured projection-failure logs
expose phase, source system, generation, failure class, timeout hints, and
backend error text for slow or mis-scoped canonical writes.

## Focused tests

```bash
go test ./internal/storage/cypher -count=1
go doc ./internal/storage/cypher
```

## Related docs

- `go/internal/storage/cypher/AGENTS.md`
- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/nornicdb-pitfalls.md`
- `docs/public/reference/nornicdb-tuning.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/projector/README.md`
- `go/internal/reducer/README.md`
