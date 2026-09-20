# Writer shape marker

## Purpose

This package owns the `graph_writer_shape` marker row that lets a deployment
retire stale graph generations exactly once per writer-semantics upgrade
(issue #6868). Ordinary operation never reopens completed work, so without
the retirement a graph that persisted stale writer output would keep serving
it after an upgrade until an operator refinalizes.

## Ownership boundary

Owns only the marker row: schema, applied-version read, atomic claim,
applied-version mark, and claim release. The upgrade sequence (compare,
claim, all-scopes refinalize, mark) lives in `recovery.EnsureGraphWriterShape`;
the version constant lives in `storage/cypher`; the startup call lives in
`cmd/reducer`. This package never refinalizes, drains, or writes graph data.

## Exported surface

- `Store` — Postgres-backed marker store over `db.ExecQueryer`
- `NewStore` — constructor
- `SchemaSQL` — marker DDL
- `(Store).EnsureSchema`, `AppliedVersion`, `ClaimVersion`,
  `MarkAppliedVersion`, `ReleaseClaim` — marker operations

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/recovery` — `WriterShapeStore` interface this store implements
- `go/internal/storage/postgres/db` — `ExecQueryer`/`Rows` contracts

## Telemetry

This package emits no metrics, spans, or logs. Upgrade progress is logged by
`recovery.EnsureGraphWriterShape` (claim won/lost, refinalize counts).

## Gotchas / invariants

- DDL is `CREATE TABLE IF NOT EXISTS`; every mutation is an `UPDATE` or an
  `INSERT ... ON CONFLICT DO NOTHING` — never a bare `INSERT`.
- `applied_version` moves forward only (`applied_version < $2` guard); the
  mark runs only after the upgrade refinalize succeeds.
- A newer version supersedes a stale same-row claim immediately; a
  same-version retry waits out the lease.
- No new tables or indexes without an `EXPLAIN ANALYZE`-backed reason.

## Related docs

- `docs/internal/evidence/6782-unwind-missing-row-key-audit.md` — the stale
  writer output this retirement heals
