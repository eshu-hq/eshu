# Reducer fact write

## Purpose

`factwrite` owns how reducer-owned facts reach Postgres: the row shapes, the
batch INSERT statements, how a batch is chunked, and how duplicate fact IDs
inside one batch are resolved.

It sits below both the reducer root and the domain families for the same
import-direction reason as its sibling leaf packages — the root imports families
to construct their handlers, so a family cannot import the root back, and
families write facts (issue #6061, epic #6053).

## Ownership boundary

This package owns:

- `Row` and `VersionedRow` — the two row shapes, unversioned and versioned.
- `BatchInsertFacts` and `BatchInsertVersionedFacts` — the batched writes.
- `DedupeRowsByFactID` — last-write-wins deduplication by fact ID within a
  batch.
- `ExecChunk`, `ChunkArgs`, `BatchSize` and the statement fragments.
- `Execer` — the minimal `ExecContext` surface, so callers can pass a pool, a
  connection, or a transaction.
- `Now` and `CollectorKind` — the UTC write timestamp and the normalized
  collector-kind column value every fact writer stamps on its rows.

It does not own transaction scope, retry, or lease behavior. A caller decides
whether a batch runs inside a transaction and what happens when it fails.

## Exported surface

| symbol | role |
|---|---|
| `Row`, `VersionedRow` | the two row shapes, unversioned and versioned |
| `BatchInsertFacts`, `BatchInsertVersionedFacts` | batched writes, chunked at `BatchSize` |
| `DedupeRowsByFactID` | last-write-wins deduplication by fact ID within a batch |
| `ExecChunk`, `ChunkArgs`, `BatchSize` | the unversioned chunk executor, its positional-argument builder, and the chunk-size bound |
| `BatchInsertPrefix`, `BatchInsertSource`, `BatchInsertConflict`, `BatchInsertQuery` | the unversioned statement fragments and their composition |
| `BatchInsertVersionedQuery` | the schema_version-carrying sibling statement |
| `Execer` | the minimal `ExecContext` surface, so callers can pass a pool, a connection, or a transaction |
| `Now`, `CollectorKind` | the UTC-normalized write timestamp and the normalized collector-kind column value |

## Dependencies

`context`, `database/sql` (through `Execer`'s `sql.Result` return), `fmt`, and
`time`. It imports nothing from the reducer root and nothing from a
domain-family subpackage — that direction is what keeps a family from having to
import the root back.

## Telemetry

This package emits no metric of its own. Every chunk it sends executes on the
caller's `Execer`, which callers on the governed-fact and container-image paths
pass as the instrumented reducer pool, so each `ExecContext` call is timed by
`eshu_dp_postgres_query_duration_seconds`, and the owning reducer pass stays
covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. Rows are in
`docs/public/observability/telemetry-coverage.md`.

## Gotchas / invariants

- **Deduplication is correctness, not tuning.** Two rows with the same fact ID
  in one batch collide on the `ON CONFLICT` target and fail the entire chunk.
  `DedupeRowsByFactID` keeps the last occurrence, which makes a batch carrying
  a corrected row behave the way a caller expects. Removing it does not make
  writes faster; it makes them fail.
- **Chunking is correctness, not tuning.** `BatchSize` bounds the statement so
  a large scope generation cannot build one statement past what the driver and
  server accept. Raising it is a measured change, not a knob.
- **`ExecChunk` and `execVersionedChunk` stay separate on purpose.** The two
  statements do not share an arity — the unversioned insert binds 16 columns
  with `fencing_token` at `$16`; the versioned insert binds 17, interleaving
  `schema_version` at position 6 and shifting every later parameter by one, so
  `fencing_token` lands at `$17`. A shared argument builder would hide that
  mapping and risk column misplacement; see the doc comment on `ExecChunk` in
  `batch_insert.go`.
- **`fencing_token` defaults to 0 and that is deliberate.** Only a writer whose
  intent can be replayed by two workers holding different views of the
  evidence needs to set it; see `Row.FencingToken` and the `BatchInsertQuery`
  doc comment ("Why fencing_token is written here", #5847) for the full
  argument, including why the conflict clause is guarded rather than merged.
- **Deduping happens inside `BatchInsertFacts`/`BatchInsertVersionedFacts`, not
  in `ExecChunk`.** A caller invoking `ExecChunk` directly with duplicate fact
  IDs gets the Postgres "cannot affect row a second time" error rather than
  silent last-write-wins.

## Related docs

- `go/internal/reducer/factdecode/README.md` — the sibling leaf package this
  one's shape mirrors.
- `docs/public/observability/telemetry-coverage.md` — the rows covering the
  execer port and forwarders this package's compatibility shim exposes.
- `docs/internal/design/package-restructure.md` — the split this package is
  part of (#6061).

## Compatibility

The reducer root keeps type aliases and forwarders in `reducer_fact_write_compat.go`, so
root call sites are unchanged. `dedupeReducerFactRowsByFactID` forwards as a
generic function statement rather than a variable: a function-valued variable
cannot carry a type parameter, and a func statement stays inlinable.
