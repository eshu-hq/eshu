# Durable Incremental Function Summaries

## Purpose

`summary` builds content-versioned function summaries and recomposes them
incrementally: when one function's facts change, only the summaries that
transitively depend on it are recomputed. It is the third layer of the value-flow
taint engine (epic #2705) and the piece reference designs (GitNexus) modeled but
left unwired — callee-version recomposition. A summary is the Sharir-Pnueli
abstraction of a function's taint behavior (TITO), keyed by a durable,
generation-independent identity.

## Ownership boundary

This package owns the summary data model, the content-version hashing, the
strongly-connected-component condensation that makes the version fixpoint
terminate under recursion, and the in-memory `Store` with its durable `Snapshot`.
It does NOT derive a function's `Effects` from source — a language analysis (the
CFG/taint passes) produces effects and feeds them to `Upsert`. It does not own a
database; `Snapshot`/`Load` give a serializable form a reducer can persist.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `FunctionID`, `NewFunctionID` — durable, generation-independent identity.
- `Effects`, `ParamSink`, `CallArgFlow`, `Summary` — the summary model (TITO).
- `Store`, `NewStore`, `Upsert`, `Version`, `Summary`, `IDs` — the incremental
  store. `Upsert` returns the sorted IDs it recomputed.
- `Snapshot`, `SnapshotFunction`, `Load` — the durable, reloadable form.

## Dependencies

Standard library only (`crypto/sha256`, `encoding/hex`, `sort`, `strconv`,
`strings`). No storage, graph, or telemetry dependencies.

## Telemetry

None. The Store is a pure data structure; a reducer that drives it owns metrics.

## Gotchas / invariants

- **Versions exclude same-SCC callee versions.** Under mutual recursion, hashing
  a callee's version into the caller would never converge. The content version
  uses only the versions of callees *outside* the function's strongly-connected
  component, so the fixpoint terminates. Do not hash intra-SCC versions.
- **Identity must stay generation-independent.** `NewFunctionID` must never take a
  commit or generation. If it does, every run churns every summary.
- **Recompute only the affected set.** `Upsert` seeds dirt from structurally
  changed functions and propagates through callers in reverse-topological order.
  Unrelated functions must not be recomputed (proven by the delta test).
- **Determinism.** Node and edge iteration in the SCC pass is sorted; `Snapshot`
  is ordered by ID. Reloading must reproduce versions exactly.
- **Not concurrency-safe.** Serialize or shard by conflict key.

## Related docs

- Epic #2705 (value-flow taint engine), child issue #2729 (this pass).
- Consumes the taint effects from `internal/parser/taint` (#2728); feeds the
  interprocedural fixpoint (#2730).
