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

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
