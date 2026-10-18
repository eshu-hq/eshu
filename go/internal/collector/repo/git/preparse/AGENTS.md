# AGENTS.md — internal/collector/repo/git/preparse guidance

## Read first

1. `README.md` — purpose, boundary, telemetry, and invariants.
2. `doc.go` — the contract.
3. `identity.go`, `decision.go`, `keys.go` (pure), then `run.go` (side effects).

## Invariants

- Accuracy first: a skip keeps the active generation's facts, so every input
  that changes emitted facts must change `Identity`. When you add an input to
  the snapshot, add it to `Config`, fold it in `writeConfig`, and add a cell to
  `TestIdentityChangesWithEveryFoldedInput`.
- When you add an env key under `go/internal/collector/repo` or
  `go/internal/parser`, classify it in `keys.go`; the guard test fails until
  you do.
- Keep `identity.go`, `decision.go`, and `keys.go` pure (no I/O, clock, or
  git) so the decision stays table-testable. Side effects belong in `run.go`:
  the store call through `Reprojector`, the counter, the span event, and the
  logs.
- Only `interval_elapsed` and `graph_dirty` may skip (ruling a9-143 D1). Do
  not widen the set without an arbiter ruling: reindex and stale-request
  reasons must store a fresh parse.
- A new outcome is a new label value: add it to `Outcomes`, to the metric
  reference, to the sweep reference table, and to the telemetry coverage row.
- Bump `IdentityPrefix` when the identity's inputs or encoding change.
- `BinaryStat` carries the indexer's content digest, never filesystem
  metadata (path, size, mtime): a rebuild that changes no indexer output must
  keep `Identity` (#8132).
