# Recovery

## Purpose

`recovery` provides the operator-facing replay and refinalize operations for
the facts-first write plane. `Handler.ReplayFailed` resets dead-lettered or
failed projector or reducer work items back to pending so they re-enter the
queue. `Handler.Refinalize` re-enqueues projector work for an explicit list of
scopes so their active generations are projected again without rebuilding the
graph from scratch.

## Ownership boundary

Owns the replay and refinalize value contracts (`ReplayFilter`,
`RefinalizeFilter`, `ReplayResult`, `RefinalizeResult`) and the `Handler` that
drives them through a `ReplayStore`. The store is implemented in
`internal/storage/postgres`. This package never touches Postgres, the graph
backend, or any network connection directly.

## Exported surface

- `Stage` — string alias for the pipeline stage enum.
- Stage constants: `StageProjector` (`"projector"`), `StageReducer`
  (`"reducer"`).
- `ReplayFilter` — filter for failed-item replay: `Stage` (required),
  `ScopeIDs` (empty means all), `FailureClass`, `Limit`.
- `ReplayResult` — outcome of a replay call: `Stage`, `Replayed` count,
  `WorkItemIDs`.
- `RefinalizeFilter` — filter for scope re-projection: `ScopeIDs` (required,
  non-empty).
- `RefinalizeResult` — outcome of a refinalize call: `Enqueued` count,
  `ScopeIDs`.
- `ReplayStore` — interface the `Handler` calls: `ReplayFailedWorkItems` and
  `RefinalizeScopeProjections`.
- `Handler` — orchestrates recovery through the store; construct with
  `NewHandler`.
- `NewHandler(store ReplayStore) (*Handler, error)` — returns an error if
  `store` is nil.
- `Handler.ReplayFailed(ctx, filter)` — validates filter, delegates to
  `ReplayStore.ReplayFailedWorkItems`.
- `Handler.Refinalize(ctx, filter)` — validates filter, delegates to
  `ReplayStore.RefinalizeScopeProjections`.

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`context`, `errors`, `fmt`, `time`). The `ReplayStore`
interface is the only injection point; the Postgres adapter lives in
`internal/storage/postgres`.

## Telemetry

This package emits no metrics, spans, or logs. Observability around recovery
calls belongs in the `ReplayStore` implementation and the admin handler that
invokes `Handler`.

## Gotchas / invariants

- `ReplayFilter.Stage` must be `StageProjector` or `StageReducer`; any other
  value fails `Validate` and the call returns an error before touching the
  store.
- `RefinalizeFilter.ScopeIDs` must be non-empty. Refinalize is always scoped
  to explicit scope IDs; unbounded refinalize is not supported.
- `Handler.time()` returns UTC unconditionally. Test assertions against the
  `now` value passed to the store must use UTC.
- Recovery is queue replay, not direct graph mutation. After a replay, the
  projector or reducer re-runs the normal write pipeline including graph writes,
  phase-state publication, and content indexing. Domains that consume
  reducer-derived state still depend on the bootstrap-index phase ordering
  described in `CLAUDE.md` after a mass replay.

## Related docs

- `docs/docs/deployment/service-runtimes.md` — admin recovery entry points
- `docs/docs/architecture.md` — facts-first bootstrap ordering
