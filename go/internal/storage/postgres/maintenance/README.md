# Postgres maintenance-request store

## Purpose

This package owns the operator-triggered ingester requests persisted in the
`runtime_ingester_control` table: the scan request lifecycle (request, claim,
and complete transitions), the fleet reindex watermark (request only), and
point reads of both. It also owns the per-repository reindex watermarks in
`repository_reindex_requests` (#7620): one row per git scope, request and bulk
read only.

## Ownership boundary

This package owns the `runtime_ingester_control` DDL text (kept for
documentation and the schema-column test; the embedded migration in
`internal/storage/postgres/migrations` is the actual bootstrap source of
truth), the request/claim/complete statements, and the `StatusRequestStore`
type. The parent `postgres` package keeps the schema bootstrap registry
(`BootstrapDefinitions`) that other domains, including this one's migration,
register against. `internal/runtime` owns the `StatusRequestStore` interface,
the `ScanRequest`/`ReindexRequest` domain types, and the
`StatusRequestHandler` that drives this store. `cmd/api` owns the write
wiring: it constructs the store and passes it to the handler. `cmd/ingester`
constructs the store and reads it only through `GetReindexState`, once per
sync cycle, for the reindex watermark. `cmd/api` writes per-repository
watermarks through `RepositoryReindexStore.RequestRepositoryReindex`, and
`cmd/ingester` hands the same store to the git selectors, which call
`RepositoryReindexWatermarks` once per sync cycle.

## Exported surface

- `StatusRequestStore` with `NewStatusRequestStore(database db.ExecQueryer)`.
- `RequestScan`, `ClaimScanRequest`, `CompleteScanRequest`, `RequestReindex`,
  `GetScanState`, `GetReindexState`.
- `RepositoryReindexStore` with `NewRepositoryReindexStore(database
  db.ExecQueryer)`, `RequestRepositoryReindex`, and
  `RepositoryReindexWatermarks`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer` contract.
- `internal/runtime` for the `ScanRequest`/`ReindexRequest`/`RequestState`
  domain types and the `StatusRequestStore` interface this type implements.

## Telemetry

None. This package executes bounded SQL through the injected database
handle; the callers own any operator-facing signal: `cmd/api`, which wires it
into `internal/runtime`'s `StatusRequestHandler`, and the git collector, which
logs the watermark read through `cmd/ingester`'s reader.

## Gotchas / invariants

- Each request type (scan, reindex) has its own status/timestamp/error
  column set on the same `runtime_ingester_control` row; every statement only
  touches its own request type's columns.
- `ClaimScanRequest` only transitions a `pending` scan row to `running`; a row
  not currently pending returns an explicit "no pending scan request" error
  rather than silently doing nothing.
- `CompleteScanRequest` only transitions a `running` scan row; the resulting
  status is `completed` when the passed error string is empty, `failed`
  otherwise.
- `GetScanState`/`GetReindexState` return an idle, zero-value request rather
  than an error when no row exists yet for the ingester.
- `RequestReindex` is the fleet reindex watermark (#7620). It stamps
  `reindex_request_requested_at` from the database clock (`now()`), never moves
  it backward (`GREATEST` over the stored value, so a transaction that started
  earlier but commits later cannot lower it), and returns the stored value with
  `RETURNING`. The git ingesters read it through `GetReindexState`; nothing
  claims or completes a reindex request. `GetReindexState` still reports the
  `reindex_request_status`, `_claimed_at`, `_completed_at`, and `_error`
  columns for rows that older binaries claimed.

- `RequestRepositoryReindex` (#7620) upserts one row per distinct scope ID in
  one statement. Go sorts and deduplicates the IDs, and the SQL feeds them
  through `SELECT DISTINCT unnest($1::text[]) ... ORDER BY 1`, so concurrent
  overlapping requests take row locks in one order and cannot deadlock, and a
  duplicate never reaches `ON CONFLICT` twice (which Postgres rejects with
  21000). `GREATEST` keeps each row monotonic, and `RETURNING` reports the
  stored value. Rows are never claimed or deleted. A refinalize writes the same
  rows for delta-active git scopes (#7797) with `reset.RequestReindexQuery`,
  which `TestRequestRepositoryReindexQueryMatchesRefinalize` pins
  byte-identical to this statement.
- `RepositoryReindexWatermarks` returns the rows later than the given fleet
  watermark. The table has no secondary index: it holds one row per
  repository ever requested, and the read is a sequential scan.
- `RepositoryReindexRequestedAt` returns the stored watermark of exactly the
  named scopes for the progress endpoint (#7620). It is a point read on the
  primary key (`repository_reindex_requests_pkey`), bounded by the request's
  at most 100 scope IDs, so unlike the bulk read above it never scans the
  table. It only reads.

- Non-test code does not import the parent `postgres` package, so root can
  import this leaf later without a cycle. The test imports root only to check
  `BootstrapDefinitions`.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
