# Reducer fact-write package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/factwrite/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- Remain a leaf below `internal/reducer`. Never import the parent reducer
  package or a family subpackage.
- `DedupeRowsByFactID` is required for correctness. Two rows sharing a fact ID
  in one batch collide on the `ON CONFLICT` target and fail the whole chunk.
  Removing or weakening it turns a benign duplicate into a failed generation.
- Deduplication is last-write-wins. A caller correcting a row later in the same
  batch expects the correction to win; reversing it silently reinstates stale
  values.
- `BatchSize` bounds statement size. Raising it requires a measurement against
  the real driver and server limits, not a guess.
- `Execer` stays minimal. Widening it to a full `*sql.DB` breaks callers that
  pass a transaction.

## Common changes

Adding a column to the unversioned path: update `BatchInsertPrefix`,
`BatchInsertSource`, and `BatchInsertConflict` (batch_insert.go), the `Row`
shape, and `ChunkArgs` together. Adding a column to the versioned path: update
`BatchInsertVersionedQuery` (batch_insert_versioned.go, one monolithic
statement literal with no separate fragments), the `VersionedRow` shape, and
`execVersionedChunk`'s inline array builder together. In both cases the
argument count and the placeholder count must agree, and nothing in the type
system enforces that — a mismatch is a runtime error on the first non-empty
batch.

`ChunkArgs`' 16-array return is also a positional dependency the reducer root
leans on, at three call sites in `container_image_identity_writer_atomic.go`.
Two (:301-306 and :327-332) append four more bind values ($17..$20 —
`legacyFactIDs`, `scopeID`, `generationID`, `fencingToken`) after
`reducerFactChunkArgs(...)` to build `containerImageIdentityPublishAndLegacyCleanupQuery`
(:36). The third (:364-376) appends six ($17..$22 — the same four plus
`workItemID` and `claimEpoch`) to build the completed-cutover statements,
`containerImageIdentityCompletedCutoverWriteQuery` (:49) and
`…PublishOnlyQuery` (:94). Renumbering `ChunkArgs`' columns without updating
any of these offsets breaks that caller silently.

## Failure modes

- A `ChunkArgs` / placeholder mismatch fails only when a batch is non-empty, so
  an empty-batch test will not catch it.
- Serializing the writer to avoid a conflict is not a fix. The repo's canon
  refuses worker-count reductions and batch-size-1 as remedies for a write that
  is not idempotent; partition by conflict key or make the write idempotent.
