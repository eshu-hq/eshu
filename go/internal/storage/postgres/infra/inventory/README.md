# Infra Inventory

## Purpose

This package owns `infra_resource_entities`, the Postgres read model behind
`/api/v0/infra/resources/count` and `/api/v0/infra/resources/inventory` and
their MCP tools (#6793). Every aggregate over the infra labels in the graph is
a full label scan on NornicDB, so on a large corpus the routes ran into the
graph-read budget and returned 504. The table turns the aggregate into a scan of a narrow,
cache-resident heap.

## Ownership boundary

The package owns the table's SQL: the derive step, the whole-repository
re-derive and marker the backfill uses, the retention mirror, and the
aggregate reads. It does not decide when to derive or which store serves a
read. The query package (`GraphInfraResourceAggregateStore`) decides that. The content
writer (`storage/postgres.ContentWriter.Write`) calls `Mirror` with every
path and tombstoned entity id a Write touched, after the content statements commit. The migration that
creates the table is `storage/postgres/migrations/109_infra_resource_entities.sql`.

## Exported surface

- `Labels` — the entity-derived labels mirrored into the table
- `Mirror`, `Change` — apply one content Write: drop tombstoned entity ids,
  then re-derive every touched path
- `MirrorPaths` — re-derive some paths of one repository
- `MirrorRepo` — re-derive a whole repository (backfill)
- `Target`, `Stats` — derive input and row counts
- `LockRepositoriesForGenerations`, `DeleteOrphanedRows` — generation
  retention's lock-before-prune and orphan cleanup, run inside its transaction
- `Backfiller`, `BackfillResult`, `BackfillComplete`, `BackfillMarker` —
  populate every existing repository once, then record the marker readers
  gate on
- `Reader`, `Filter`, `Dimension`, `CountBucket`, `CountBuckets`,
  `DimensionBuckets` — the aggregate reads the query layer serves from
- `WriterSessionSQL`, `WriterConnectOption`, `OpenWriterDB`,
  `ReadModelReady` — the rolling-upgrade fence: derive-aware connections,
  and the readers' gate (marker present and no repository marked dirty)
- `ReconcileCycle`, `ReconcileRequest`, `ReconcileRepo`, `ClaimPage`,
  `LoadCursor`,
  `ReconcileBatch`, `RepoReconcile`,
  `ReconcileMatch`/`ReconcileSuspect`/`ReconcileRepaired`/`ReconcileFenced`/`ReconcileError` —
  the reducer's drift check and repair (`cmd/reducer`
  `infra_inventory_reconcile_wiring.go`)

See `doc.go` for the contract.

## Dependencies

- `storage/postgres/db` — `ExecQueryer`, `Beginner`, `Transaction`
- `storage/postgres/pgarray` — text array arguments

The package must not import the parent `postgres` package. The parent imports
this one.

## Telemetry

- The content writer logs the derive as stage `derive_infra_inventory` with
  `path_count`, `rows_deleted`, `rows_inserted`, and `duration_seconds`, next
  to the existing `upsert_entities` and `reap_stale_entities` stages.
- `Backfiller` logs `event_name=infra_inventory.backfill` (started, progress
  every 100 repositories, completed). The query package logs
  `infra_inventory.backfill.failed` when a background run fails.
- Every statement runs through the caller's `InstrumentedDB` where one is
  wired, so `eshu_dp_postgres_query_duration_seconds` times it.
- Retention reports `RowsPruned["infra_resource_entities"]`.
- The query layer counts reads by serving store with
  `eshu_dp_infra_inventory_reads_total{route,source}`.
- The reducer's reconcile runner counts repositories checked with
  `eshu_dp_infra_inventory_reconcile_total{outcome}` and logs each repair as
  `infra_inventory.reconcile.drift`. A repository is repaired only after it
  differs on two cycles in a row; one mismatch is only `suspect`.

## Gotchas / invariants

- The table mirrors `content_entities`, not facts. `content_entities` matches
  the graph for every label in `Labels`, including repositories whose newest
  generation failed or is pending. Active-generation facts drop those.
- `TerraformModule` and `TerraformOutput` have a second writer, the Terraform
  state projector, whose nodes have no content row. The table holds their
  content-derived nodes; the query layer adds the state projector's nodes from
  the graph through the `tf_module_evidence_source` /
  `tf_output_evidence_source` indexed seek.
- Dimensions are trimmed and a missing value is stored as the empty string.
  That matches the canonical node writer, which trims promoted metadata and
  drops empty strings.
- Lock first, then delete, then insert, in one transaction. Reordering or
  splitting the transaction reopens the stale-resurrection race the lock
  closes.
- Two generations of one scope never project concurrently: the projector
  claim holds a per-scope `NOT EXISTS` guard
  (`storage/postgres/projector_queue_claim_sql.go`). The per-repository lock
  still makes the derive safe against the backfill, which runs outside that
  claim.

## Related docs

- `docs/public/reference/http-api.md` (infra resource aggregate routes)
- `docs/public/reference/telemetry/index.md`

## Rolling-upgrade fence

A binary from before this table writes `content_entities` without deriving.
Migration 109 installs row triggers on `content_entities`: a write of an
infra-typed row from a connection that has not run `WriterSessionSQL` marks
the repository in `infra_resource_entity_dirty_repos` in the same statement.
`runtime.OpenPostgres` marks every connection of this binary, so only older
binaries and manual SQL trip it. Readers (`Reader.Ready`) trust the table only
while no repository is marked. Each reconcile cycle repairs marked
repositories first (`fenced`): lock, clear the mark, delete, insert, in one
transaction. The trigger upserts the mark, which holds its row lock until the
unaware write commits, so a repair waits for that write and re-derives its
rows (`TestWriterFenceLiveRepairWaitsForAnOpenUnawareWrite`).
