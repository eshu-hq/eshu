# Postgres GCP freshness trigger store

## Purpose

This package persists GCP asset-inventory event-driven refresh triggers
(`gcp_freshness_triggers`) so the workflow coordinator can claim, hand off,
or fail them without losing a trigger to a crash between claim and
completion. It is the `freshness/gcp/` leaf of the storage/postgres split
(#6693).

## Ownership boundary

This package owns the DDL, upsert, claim, reap, and completion SQL for
`gcp_freshness_triggers`, and the `GCPFreshnessStore` type that runs them.
`cmd/webhook-listener` builds the store to normalize and coalesce inbound GCP
events; `cmd/workflow-coordinator` builds it to claim queued triggers, hand
them off to a planned workflow, or fail them. `internal/collector/gcpcloud/freshness`
owns the `Trigger`/`StoredTrigger`/`TriggerStatus` domain types this package
persists and reads back.

This is one of the three `freshness/{aws,gcp,incident}/` trigger stores that
share a `StoreTrigger`/`ClaimQueuedTriggers`/`ReapExpiredTriggerClaims`/
`MarkTriggersHandedOff`/`MarkTriggersFailed` lifecycle the coordinator and
webhook listener build (#6693 decision N4). `vulnerability/`'s source-state
store has none of that lifecycle and did not follow them into this split.

## Exported surface

- `GCPFreshnessStore` with `NewGCPFreshnessStore(database db.ExecQueryer)`.
- `EnsureSchema`, `StoreTrigger`, `ClaimQueuedTriggers`,
  `ReapExpiredTriggerClaims`, `MarkTriggersHandedOff`, `MarkTriggersFailed`.
- `GCPFreshnessSchemaSQL()` returns the DDL.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows`
  contracts.
- `internal/collector/gcpcloud/freshness` for the `Trigger`/`StoredTrigger`/
  `TriggerStatus`/`EventKind` domain types this package persists and scans.

## Telemetry

None of its own. `cmd/webhook-listener` and `cmd/workflow-coordinator` wrap
the injected `db.ExecQueryer` in their own `InstrumentedDB`
(`StoreName: "gcp_freshness_triggers"`) before constructing this store, so
call-level tracing and metrics are the caller's responsibility, not this
package's.

No-Observability-Change: this extraction moves only the GCP freshness
trigger store, its SQL text, and its DDL. The upsert, claim, reap, and
completion statements are byte-identical, both callers construct the same
store through their own instrumentation wrapper, and no metric, span, or log
name changes.

No-Regression Evidence: focused `internal/storage/postgres/...` tests
(including this package) run green on the moved tree with `-race`; the SQL
text is unchanged so no plan or lifecycle proof is re-owed.

## Gotchas / invariants

- `ClaimQueuedTriggers` sets a `claim_expires_at` lease
  (`claimedAt+leaseDuration`) so a mid-batch handoff abort or coordinator
  crash cannot strand a row at `claimed` forever; `ReapExpiredTriggerClaims`
  requeues it once the lease expires (#4576).
- Every claim bumps `claim_fencing_token`, and `MarkTriggersHandedOff`/
  `MarkTriggersFailed` only complete a row whose current token still matches
  the token the caller received from `ClaimQueuedTriggers`: a stale claimant
  whose lease was reaped and re-claimed by a different owner cannot complete
  a claim it no longer holds (#4576, raised in PR #4682 review).
- `StoreTrigger`'s `ON CONFLICT (freshness_key)` upsert never overwrites a
  `claimed` row's ownership fields; it only bumps `duplicate_count` and lets
  a genuinely new (non-claimed) trigger's fields replace the stored ones.
- Do not import the parent `postgres` package: that is an import cycle.
  `cmd/webhook-listener` and `cmd/workflow-coordinator` import this package
  directly instead.

## Related docs

- [Postgres storage](../../README.md)
- [storage/postgres target tree](../../../../../../docs/internal/design/6693-postgres-target-tree.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -race -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
