# Postgres maintenance-request store

## Purpose

This package owns the operator-triggered scan and reindex request lifecycle
for an ingester: request, claim, and complete transitions persisted in the
`runtime_ingester_control` table, plus point reads of the current state.

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
constructs a read-only store to read the reindex watermark each sync cycle.

## Exported surface

- `StatusRequestStore` with `NewStatusRequestStore(database db.ExecQueryer)`.
- `RequestScan`, `ClaimScanRequest`, `CompleteScanRequest`, `RequestReindex`,
  `ClaimReindexRequest`, `CompleteReindexRequest`, `GetScanState`,
  `GetReindexState`.

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
  column set on the same `runtime_ingester_control` row; claim and complete
  statements only ever touch their own request type's columns.
- `ClaimScanRequest`/`ClaimReindexRequest` only transition a `pending` row to
  `running`; a row not currently pending returns an explicit "no pending ...
  request" error rather than silently doing nothing.
- `CompleteScanRequest`/`CompleteReindexRequest` only transition a `running`
  row; the resulting status is `completed` when the passed error string is
  empty, `failed` otherwise.
- `GetScanState`/`GetReindexState` return an idle, zero-value request rather
  than an error when no row exists yet for the ingester.
- `RequestReindex` is the fleet reindex watermark (#7620). It stamps
  `reindex_request_requested_at` from the database clock (`now()`), never moves
  it backward (`GREATEST` over the stored value, so a transaction that started
  earlier but commits later cannot lower it), and returns the stored value with
  `RETURNING`. The git ingesters read it through `GetReindexState` and never
  claim or complete it; `ClaimReindexRequest`/`CompleteReindexRequest` have no
  production caller.

Performance Evidence (#7620): the request is one primary-key upsert
(`Conflict Arbiter Indexes: runtime_ingester_control_pkey`, about 0.5 ms on
Postgres 18) and the per-cycle read touches one buffer. The live test
`TestStatusRequestStoreRequestReindexWatermarkMonotonicLive` proves the
watermark stays monotonic under 16 concurrent requests. With `GREATEST`
removed, that test fails.
- Non-test code does not import the parent `postgres` package, so root can
  import this leaf later without a cycle. The test imports root only to check
  `BootstrapDefinitions`.

No-Observability-Change: the store emits no signal of its own. The watermark's
operator signals live in the git collector (`git_reindex_watermark_*` logs and
the `reindex_requested` reason on
`eshu_dp_collector_reconciliation_full_snapshots_total`).

No-Regression Evidence: focused package tests cover every transition
(request, claim success/failure, complete, idle read) against the shared
`fake.ExecQueryer` double, and the live test above covers the reindex request
against real Postgres.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
