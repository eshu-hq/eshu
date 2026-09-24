# Postgres AWS claim-fencing store

## Purpose

This package owns two durable AWS collector tables: paginated-scan resume
markers (`aws_scan_pagination_checkpoints`) and per-tuple scan status
(`aws_scan_status`). Both exist so an AWS collector worker that dies mid-scan
can resume or be superseded safely instead of silently losing or duplicating
progress.

## Ownership boundary

- `AWSPaginationCheckpointStore` owns `aws_scan_pagination_checkpoints`: it
  implements `checkpoint.Store`
  (`internal/collector/awscloud/checkpoint`) so `awsruntime` scanners can
  `Load`/`Save`/`Complete` a resume marker and `ExpireStale` prior-generation
  checkpoints for one claim boundary.
- `AWSScanStatusStore` owns `aws_scan_status`: `StartAWSScan`,
  `ObserveAWSScan`, and `CommitAWSScan` record the running/succeeded/failed
  lifecycle and commit outcome the `/admin/status` surface reads.

Both stores are independent: they share no type, table, or private helper
with each other, only the same `db.ExecQueryer` contract and claim-fencing
convention (a `(generation_id, fencing_token)` pair a stale worker cannot
override).

## Exported surface

- `AWSPaginationCheckpointStore` with `NewAWSPaginationCheckpointStore(database db.ExecQueryer)`.
  `Load`, `Save`, `Complete`, `ExpireStale`, `EnsureSchema`.
  `AWSPaginationCheckpointSchemaSQL()` returns the DDL.
- `AWSScanStatusStore` with `NewAWSScanStatusStore(database db.ExecQueryer)`.
  `StartAWSScan`, `ObserveAWSScan`, `CommitAWSScan`, `EnsureSchema`.
  `AWSScanStatusSchemaSQL()` returns the DDL.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer` contract.
- `internal/collector/awscloud` and `internal/collector/awscloud/checkpoint`
  for the domain types (`Boundary`, `checkpoint.Key`/`Scope`/`Checkpoint`)
  these stores persist.
- `internal/telemetry` (`AWSPaginationCheckpointStore` only) for the
  checkpoint-event counter.

## Telemetry

`AWSPaginationCheckpointStore.Instruments`, when set, records
`eshu_dp_aws_pagination_checkpoint_events_total` for load, save, complete,
resume, and expiry events, tagged by service/account/region/operation and
result. `AWSScanStatusStore` emits no metric of its own; its rows are read
directly by the `/admin/status` surface.

No-Observability-Change: this extraction moves both stores unchanged; no
metric, span, log field, worker, queue, lease, retry, or durable write shape
changed.

## Gotchas / invariants

- `AWSPaginationCheckpointStore.Save`/`Complete`/`ExpireStale` must keep
  their `fencing_token <=` conflict guards: a stale AWS worker must not
  overwrite page state from a newer claim.
- `AWSScanStatusStore`'s `startAWSScanStatusQuery` widens its conflict guard
  across workflow generations only when the prior row is terminal
  (`succeeded`/`partial`/`failed`/`credential_failed` with a committed or
  failed commit status), a terminal permission-denied gap, or an orphaned
  `last_started_at` that never advanced -- the last case is issue #612's
  fix, letting a new generation reclaim a slot an old worker abandoned
  between `StartAWSScan` and `ObserveAWSScan`. `ObserveAWSScan`/
  `CommitAWSScan` keep an exact `(generation_id, fencing_token)` match so a
  stale collector that wakes up cannot clobber the new owner's row.
- Do not import the parent `postgres` package: that is an import cycle.
  `cmd/collector-aws-cloud/service.go` imports this package directly instead.

No-Regression Evidence: `go test ./internal/storage/postgres/cloud/aws/...
-count=1` covers both stores' schema, fencing, and event-recording
contracts.
