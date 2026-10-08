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

## Evidence

No-Regression Evidence (#6950 batch 1, code family): this change moves the
seven `code.*` fact-kind constants' Go importers off the transitional
`facts.Code*` compat spellings and deletes the emptied `compat_code.go`. No
fact-kind string, payload shape, registry entry, or executable statement
changes: across 44 files, every production hunk requalifies an identifier or
import path only, every other hunk is a package-doc rewording or the compat
file's own deletion, and the build resolves with no dangling reference.
Measurement: `go test -count=1` over the 13 changed Go packages on baseline
`57167b009` vs after `49be8f597` (the measurement commit; this Evidence
section is the only later change) gives identical outcomes — the same 11
packages ok on both sides, with the single failing package
(`internal/collector/repo/git`, `TestFetchChurnZombiesDrainedByReaper`)
failing identically before and after (it fails on the clean baseline on this
host too; its test file and the git constructor it exercises are untouched by
this diff, and CI is green on main). `go test -list` inventory is identical
on both sides. Backend/version: n/a (compile-time-only; no backend touched).
Input shape: n/a (no runtime input). Terminal queue/row counts: none — no
queue, lease, Cypher, or SQL path is touched. Contract gates green on the
branch: `verify-fact-kind-registry.sh`, `verify-factschema-diff.sh`, and
`verify-payload-usage-manifest.sh` (the `code.*` kinds carry no schema
version and are not in `specs/fact-kind-registry.v1.yaml`, so registry
output is unchanged by construction). The change is safe because it cannot
alter runtime behavior: the compiler resolves the same constants through
their new paths.

No-Observability-Change (#6950 batch 1, code family): this package carries no
instrumentation (see Telemetry above) and the move adds, removes, or renames
no metric, span, structured log, or status field in any touched package. The
collector, reducer, and projector telemetry that reads and writes facts of
these kinds is untouched; operator signals are identical before and after.

## Related docs

- `docs/internal/naming.md`
- `docs/public/reference/fact-schema-versioning.md`
