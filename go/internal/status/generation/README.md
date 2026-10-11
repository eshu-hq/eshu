# Generation Lifecycle Status

## Purpose

`internal/status/generation` owns the scope-generation lifecycle family of
the status report: recent lifecycle transitions for the operator status
surface, and the bounded, filterable drilldown page the freshness generation
query surface (`internal/query/freshness`) reads through. It exists so the
root `internal/status` package (which aggregates every family into
`RawSnapshot` and `Report`) has one place that owns "what happened to this
scope's generations, recently and in detail."

## Ownership boundary

This package owns generation-transition and lifecycle-drilldown row shape,
filtering, and rendering. It does not own `GenerationHistorySnapshot` (the
aggregate generation history counts in root's `history.go`) or the queue's
own lifecycle counts beyond the per-generation rollup carried in
`LifecycleRecord.QueueStatus`.

The root aggregates every family leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves
and root may import `generation`; `generation` may import neither the root
nor a sibling leaf.

## Exported surface

- `TransitionSnapshot` — one recent scope-generation lifecycle row
- `CloneTransitions`, `TransitionsText`, `TransitionsJSON` — defensive copy
  and text/JSON rendering for a transition slice
- `LifecycleFilter` — bounds a drilldown to scope/repository/collector/
  source-system/generation/status, with `Normalize` and `HasScopeSelector`
- `LifecycleRecord`, `QueueStatus`, `LatestFailure` — one drilldown row, its
  per-generation queue rollup, and its most recent failure
- `LifecyclePage` — one bounded, ordered drilldown page
- `LifecycleTimestamp` — RFC3339 UTC formatting shared with the drilldown
  contract
- `MaxLifecycleLimit`, `DefaultLifecycleLimit` — the page-size cap and
  default

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`strings`, `time`). This package has no dependency on
`internal/status/shared` — its rendering is self-contained.

## Telemetry

None. This package performs no I/O; the Postgres query that sources and
bounds the raw rows lives in the freshness generation query surface and the
status reader, and this package only filters, joins, and renders what it is
given.

## Gotchas / invariants

- `LifecycleFilter.Normalize` clamps `Limit` into `[1, MaxLifecycleLimit]`,
  defaulting a non-positive value to `DefaultLifecycleLimit`. A caller that
  skips `Normalize` and passes an unclamped `Limit` to the storage reader
  bypasses the drilldown's payload-size contract.
- `LifecycleFilter.Scoped`, `AllowedRepositoryIDs`, and `AllowedScopeIDs`
  carry a caller's scoped grant; they are bound directly in the storage
  query rather than checked against a selector, because the filter's five
  other fields already reach rows on their own.
- `LifecyclePage.Truncated` is the caller's signal to narrow the filter or
  page again — a truncated page is not a complete answer even though
  `Records` is non-empty.
- `LatestFailure` is `nil` on a `LifecycleRecord` when no work item for that
  generation recorded a failure class; do not treat a nil pointer as "failed
  with no detail."
- `cloneGenerationTransitions`, `generationTransitionsText`, and
  `generationTransitionsJSON` are called directly from root `status.go`/
  `json.go` and are unexported today (`CloneTransitions`, `TransitionsText`,
  `TransitionsJSON` above are their post-move exported names). All three
  must be exported on the move.
- `LifecycleTimestamp` and the `changedsince` package's own timestamp helper
  are documented as staying in lockstep (same RFC3339-UTC-or-empty shape).
  Changing one without the other breaks that promise even though nothing
  enforces it at compile time.

## Evidence

No-Regression Evidence (#6949 batch 4, generation family): this change moves
the ten `generation.*` compat entries' Go importers (`GenerationTransitionSnapshot`,
`GenerationLifecycleFilter`, `GenerationLifecycleRecord`, `GenerationLifecyclePage`,
`GenerationQueueStatus`, `GenerationLatestFailure`, `GenerationPriorFailure`,
`MaxGenerationLifecycleLimit`, `DefaultGenerationLifecycleLimit`, and
`GenerationLifecycleTimestamp`, reached as both `status.*` and `statuspkg.*`)
off the transitional compat spellings onto `generation.TransitionSnapshot`,
`generation.LifecycleFilter`, `generation.LifecycleRecord`, `generation.LifecyclePage`,
`generation.QueueStatus`, `generation.LatestFailure`, `generation.PriorFailure`,
`generation.MaxLifecycleLimit`, `generation.DefaultLifecycleLimit`, and
`generation.LifecycleTimestamp`, and deletes the emptied `compat_generation.go`.
No type shape, wire field, SQL text, or executable statement changes:
across 18 code files, every production hunk requalifies an identifier or
import path only (40 changed non-import production lines per side verified
identical modulo the mapping; the other 8 changed lines are import
adds/removes),
every other hunk is a package-doc rewording, a ledger row, or the
compat file's own deletion, and the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6949-generation-batch4-before, ledger:6949-generation-batch4-after). The command is `go test -count=1` over
the 6 affected package targets (`./internal/status/...`,
`./internal/query/...`, `./internal/storage/postgres/...`,
`./internal/mcp/...`, `./cmd/admin-status/...`, `./cmd/eshu/...`) on baseline
`9394b424e9` vs measurement commit `9e64b12f5d` (this Evidence section,
the two ledger rows, a doc-comment fix, and content-identical rebases
tracking main are the only later changes): 158 packages ok, 0 fail on both sides, with the
ok-package set byte-identical after timing strip. `go test -list`
inventory is identical on both sides (11175 tests). Backend/version:
go1.26.2 linux/amd64, in-memory; no backend touched. Input shape: n/a
(no runtime input). Terminal queue/row counts: none — no queue, lease,
Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-openapi.sh` (261/261 routes), `verify-contracttest.sh`, and
`verify-package-docs.sh`. The change is safe because it cannot alter
runtime behavior: the compiler resolves the same types through their
new paths, and the compat deletion is compile-enforced total — any
missed caller would fail the build.

No-Observability-Change (#6949 batch 4, generation family): this package
carries no instrumentation (see Telemetry above) and the move adds,
removes, or renames no metric, span, structured log, or status field in
any touched package. The status readers, query handlers, freshness
routes, and postgres readers that use these types are untouched;
operator signals are identical before and after.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
