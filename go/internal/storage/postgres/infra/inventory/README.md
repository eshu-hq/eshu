# Infra Inventory

## Purpose

This package owns `infra_resource_entities`, the Postgres read model behind
`/api/v0/infra/resources/count` and `/api/v0/infra/resources/inventory` and
their MCP tools (#6793). Every aggregate over the infra labels in the graph is
a full label scan on NornicDB, so on a large corpus the routes ran into the
graph-read budget and returned 504. The table turns the aggregate into a scan of a narrow,
cache-resident heap.

## Ownership boundary

The package owns the table's SQL: the derive step, and the whole-repository
re-derive the backfill uses. It does not decide when to derive. The content
writer (`storage/postgres.ContentWriter.Write`) calls `MirrorPaths` for every
path a Write touched, after the content statements commit. The migration that
creates the table is `storage/postgres/migrations/109_infra_resource_entities.sql`.

## Exported surface

- `Labels` — the entity-derived labels mirrored into the table
- `MirrorPaths` — re-derive some paths of one repository after a content Write
- `MirrorRepo` — re-derive a whole repository (backfill)
- `Target`, `Stats` — derive input and row counts

See `doc.go` for the contract.

## Dependencies

- `storage/postgres/db` — `ExecQueryer`, `Beginner`, `Transaction`
- `storage/postgres/pgarray` — text array arguments

The package must not import the parent `postgres` package. The parent imports
this one.

## Telemetry

The content writer logs the derive as stage `derive_infra_inventory` with
`path_count`, `rows_deleted`, `rows_inserted`, and `duration_seconds`, next to
the existing `upsert_entities` and `reap_stale_entities` stages.

## Gotchas / invariants

- The table mirrors `content_entities`, not facts. `content_entities` matches
  the graph for every label in `Labels`, including repositories whose newest
  generation failed or is pending. Active-generation facts drop those.
- `TerraformModule` and `TerraformOutput` are not in `Labels`. The Terraform
  state projector also writes nodes under those labels, with no content row,
  so a content-derived copy would undercount them.
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
