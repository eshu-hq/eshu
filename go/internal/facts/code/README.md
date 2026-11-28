# Code

## Purpose

`code` declares the fact-kind constants for parser-emitted
code-intelligence evidence: the value-flow scan marker and per-function
dataflow record, the function-level taint source and summary facts the
collector emits from the parser's dataflow buckets, and the resolved
intraprocedural/interprocedural taint findings consumed by reducer graph
projection.

## Ownership boundary

Owns the fact-kind string constants and the `FlowReadFactKinds()` ordering
accessor only. Does not own the parser's dataflow analysis
(`internal/parser`), the collector's emission
(`internal/collector/repo/git`), reducer projection
(`internal/reducer/code/*`), materialization (`internal/projector/code/*`,
`internal/projector/runtime`), or the query read path
(`internal/query/codequery`, `internal/query/codemodel`) — those packages
hold the logic that produces and consumes facts of these kinds.

## Exported surface

- `DataflowScannedFactKind` — one scope generation in which the value-flow
  gate ran
- `DataflowFunctionFactKind` — one parser-emitted function-level dataflow
  record
- `FunctionSourceFactKind` — one function's param-level taint source
- `FunctionSummaryFactKind` — one function's durable value-flow summary
- `TaintEvidenceFactKind` — one resolved intraprocedural taint finding
- `InterprocEvidenceFactKind` — one resolved interprocedural taint finding
- `FlowReadFactKinds()` — the canonical, ordered subset
  (`TaintEvidenceFactKind`, `InterprocEvidenceFactKind`,
  `DataflowFunctionFactKind`) the cumulative-active code-flow read and its
  partial index cover

See `doc.go` for the full godoc contract.

## Dependencies

None. Go standard library is not even imported; every file declares one or
two untyped string constants.

## Depended on by

Every current caller — `go/internal/storage/postgres`,
`go/internal/projector/code/*`, `go/internal/projector/runtime`,
`go/internal/collector/repo/git`, `go/internal/query/codequery`,
`go/internal/query/codemodel`, `go/internal/reducer/code/*`, and the
`go/internal/reducer` root — references the destuttered `code.<Name>`
names directly. The pre-move `facts.Code*` spellings were retired with
the transitional `compat_code.go` in #6950 once the last caller moved.

## Telemetry

None. Fact-kind constants carry no instrumentation; the collector,
reducer, and projector packages that read and write facts of these kinds
own their own telemetry (see their READMEs).

## Gotchas / invariants

- These kinds carry NO schema version and are NOT in
  `specs/fact-kind-registry.v1.yaml` or the facts root's
  `schemaVersionFamilies` table. `facts.SchemaVersion`/
  `ClassifySchemaVersion` return `("", false)`/`CompatibilityUnknownKind`
  for every kind here. Adding a kind here does not add schema-version
  admission — that is a separate, deliberate registration step (see
  `AGENTS.md`).
- `FlowReadFactKinds()`'s return set is a contract, not incidental: two SQL
  sites (`codemodel.ListActiveCodeFlowFactsSQL`'s `fact_kind IN (...)` list,
  in `go/internal/query/codemodel/code_flow_postgres.go`, and the
  `fact_records_code_flow_repo_idx` partial index predicate) must name
  every kind this function returns, or the index silently stops covering a
  kind the read still queries.
- Every exported name here was destuttered from its pre-move `Code*` form
  (`docs/internal/naming.md` rule 4). The pre-move `facts.Code*` spellings
  no longer resolve; the transitional `compat_code.go` was deleted in
  #6950 after the last caller requalified to `code.<Name>`.

## Related docs

- `docs/internal/naming.md`
- `docs/public/reference/fact-schema-versioning.md`
