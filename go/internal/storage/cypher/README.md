# storage/cypher

## Purpose

`internal/storage/cypher` owns backend-neutral graph write contracts for Eshu.
It builds Cypher `Statement` values, canonical node and edge writers,
execution wrappers, and write instrumentation that Neo4j and NornicDB must
satisfy through the same executor seams.

## Ownership boundary

This package owns source-local write planning, canonical node writes,
reducer-owned shared projection edges, semantic entity writes, statement
metadata, batching, retry/timeout wrappers, and graph-write telemetry.

It does not own driver sessions, backend startup, schema DDL, runtime backend
selection, or query behavior. Runtime wiring and storage adapter packages own
those seams. Writers and callers must not branch on `ESHU_GRAPH_BACKEND`.

## Exported surface

See `doc.go` for the godoc contract. The main surfaces are `Statement`,
`Plan`, `BuildPlan`, `Operation`, `CanonicalNodeWriter`, `EdgeWriter`,
semantic entity writers, `Executor`, `GroupExecutor`, `PhaseGroupExecutor`,
`ExecuteOnlyExecutor`, `TimeoutExecutor`, `RetryingExecutor`,
`InstrumentedExecutor`, `GraphWriteTimeoutError`, `CypherReader`, and
`CanonicalNodeChecker`.

## Dependencies

`internal/graph` supplies source-local materialization records,
`internal/projector` supplies canonical rows, `internal/reducer` supplies
domain constants and shared projection rows, and `internal/telemetry` supplies
write instruments, spans, and bounded attributes.

## Telemetry

Operators diagnose this package through canonical write spans, phase duration
metrics, batch-size metrics, retry counters, grouped shared-edge metrics,
code-call edge batch metrics, canonical projection/retract metrics, and
structured projection-failure logs. Paths, symbols, fact IDs, raw query text,
and backend error text do not belong in metric labels.

## Gotchas / invariants

- `CanonicalNodeWriter.Write` phase order is a correctness contract:
  retractions, repository cleanup, repositories, directories, files, entities,
  entity retractions, containment, Terraform state, OCI registry, package
  registry, modules, then structural edges.
- Repository cleanup precedes repository `MERGE`; directories precede nested
  files; current entity upserts precede stale entity cleanup.
- Hot-path writes must be idempotent and retry-safe. Use `MERGE` for identity
  and split mutable properties into `SET`.
- Do not serialize workers to hide graph races. Fix idempotency, retry
  commit-time conflicts, or redesign the conflict domain.
- OCI and package registry rows keep digest/uid-backed identity; mutable tags
  and source hints stay weak evidence until reducer correlation admits truth.
- Code-call rows can write `CALLS`, `REFERENCES`, or `USES_METACLASS`; Go and
  TypeScript type references stay `REFERENCES`.
- SQL trigger rows keep both `TRIGGERS` and `EXECUTES` write/retract support.

## Focused tests

```bash
cd go
go test ./internal/storage/cypher -count=1
go doc ./internal/storage/cypher
go run ./cmd/eshu docs verify ../go/internal/storage/cypher --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related docs

- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/nornicdb-pitfalls.md`
- `docs/public/reference/nornicdb-tuning.md`
- `docs/public/reference/telemetry/index.md`
- `go/internal/projector/README.md`
- `go/internal/reducer/README.md`
