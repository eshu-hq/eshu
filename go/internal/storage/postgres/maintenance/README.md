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
`StatusRequestHandler` that drives this store. `cmd/api` owns wiring: it
constructs the store and passes it to the handler.

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
handle; the caller (`cmd/api`, which wires it into `internal/runtime`'s
`StatusRequestHandler`) owns any
operator-facing signal.

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
- Do not import the parent `postgres` package: that is an import cycle.

No-Observability-Change: this extraction moves only the maintenance-request
store, its SQL text, and its DDL constant. The request, claim, and complete
statements are byte-identical, `cmd/api` constructs the same store type
through the new import path, and no metric, span, or log name changes.

No-Regression Evidence: focused package tests cover every transition
(request, claim success/failure, complete, idle read) against the shared
`fake.ExecQueryer` double; the SQL text is unchanged so no query-plan proof
is re-owed.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
