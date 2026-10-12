# AGENTS.md — internal/collector/repo/git/preparse guidance

## Read first

1. `README.md` — purpose, boundary, and invariants.
2. `doc.go` — the contract.
3. `identity.go`, `decision.go`, `keys.go`.

## Invariants

- Accuracy first: a skip keeps the active generation's facts, so every input
  that changes emitted facts must change `Identity`. When you add an input to
  the snapshot, add it to `Config`, fold it in `writeConfig`, and add a cell to
  `TestIdentityChangesWithEveryFoldedInput`.
- When you add an env key under `go/internal/collector/repo` or
  `go/internal/parser`, classify it in `keys.go`; the guard test fails until
  you do.
- Keep the package pure (no I/O, clock, or git) so the decision is
  table-testable.
- Only `interval_elapsed` and `graph_dirty` may skip (ruling a9-143 D1). Do
  not widen the set without an arbiter ruling: reindex and stale-request
  reasons must store a fresh parse.
- Bump `IdentityPrefix` when the identity's inputs or encoding change.
