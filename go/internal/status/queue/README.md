# Queue Status

## Purpose

`internal/status/queue` owns the work-queue coordination family of the
status report: why eligible work is blocked behind a conflict-domain gate,
and what the newest queued-work failure looked like. It exists so the root
`internal/status` package (which aggregates every family into `RawSnapshot`
and `Report`) has one place that owns "is queued work stuck, and on what."

## Ownership boundary

This package owns blockage-row and failure-snapshot shape, normalization,
and plain-text rendering. It does not own the aggregate `QueueSnapshot`
lifecycle counts (pending/in-flight/retrying/succeeded/failed/dead-letter),
which stay in root because they are computed across every domain, not one
family; it does not own domain backlog depth (`DomainBacklog`, also root).

The root aggregates every family leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves
and root may import `queue`; `queue` may import neither the root nor a
sibling leaf.

## Exported surface

- `Blockage` — one conflict-domain-blocked queue row
- `CloneBlockages` — normalize and order blockage rows (biggest, oldest
  first), dropping blank-stage rows
- `RenderBlockageLines` — plain-text operator lines for blocked work
- `FailureSnapshot` — the newest queued-work failure metadata
- `CloneFailure` — defensive copy of a failure snapshot
- `FailureText` — one bounded operator line for a failure snapshot

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/status/shared` — `NonNegativeDuration`

## Telemetry

None. This package performs no I/O and emits no metrics, spans, or logs; it
normalizes and renders rows the status reader already gathered.

## Gotchas / invariants

- `FailureSnapshot` values render only in status text/JSON payloads. They
  must never be promoted to a metric label — `WorkItemID`, `ScopeID`, and
  `GenerationID` are unbounded-cardinality identifiers.
- `FailureText` truncates `FailureMessage` and `FailureDetails` independently
  at 240 characters each (`queueFailureTextLimit`), appending `...`. A
  caller that needs the untruncated value must read the `FailureSnapshot`
  fields directly, not the rendered text.
- `CloneBlockages` silently drops any row whose `Stage` is blank after
  trimming — an empty-stage row never reaches the rendered report.
- `CloneBlockages`'s sort order (blocked count descending, then oldest age
  descending, then stage/domain/conflict-key ascending) is the operator
  report's contract: the biggest, oldest blocker is always first. Do not
  re-sort downstream without a reason documented at the call site.
- `cloneQueueBlockages`, `renderQueueBlockageLines`, `cloneQueueFailure`, and
  `queueFailureText` are called directly from root `status.go` and are
  unexported today (`CloneBlockages`, `RenderBlockageLines`, `CloneFailure`,
  `FailureText` above are their post-move exported names). All four must be
  exported on the move or root's call sites will not compile.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
