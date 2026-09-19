# Postgres webhook trigger store

## Purpose

This package persists provider webhook intake decisions for later targeted
repository refresh handoff: dedupe by refresh key, ordered claiming with
`FOR UPDATE SKIP LOCKED`, and handoff/failure recording.

## Ownership boundary

This package owns the `webhook_refresh_triggers` table, its DDL, and the
claim/handoff/failure lifecycle on those rows. `internal/webhook` owns the
trigger domain types and intake decisions. The parent `postgres` package,
`cmd/webhook-listener`, `cmd/ingester`, and `cmd/collector-git` own wiring:
they construct the store and call it. `internal/query` and the collectors
own what happens after handoff.

## Exported surface

- `WebhookTriggerStore` with `NewWebhookTriggerStore(database db.ExecQueryer)`.
- `EnsureSchema`, `StoreTrigger`, `ClaimQueuedTriggers`,
  `MarkTriggersHandedOff`, `MarkTriggersFailed`.
- `WebhookTriggerSchemaSQL()` returns the DDL.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows` contracts.
- `internal/facts` and `internal/webhook` for domain types.

## Telemetry

None. This package executes bounded SQL through the injected database handle;
the instrumented handle and the callers own observability.

## Gotchas / invariants

- Storing a trigger never marks graph or repository truth fresh.
- Claim order is `(status, received_at ASC, trigger_id ASC)`; the claim holds
  `FOR UPDATE SKIP LOCKED` so concurrent claimants never double-deliver.
- A nil database fails fast with an explicit error, never a nil panic.
- Do not import the parent `postgres` package: that is an import cycle.
  Root tests exercise this package through its exported constructors.

No-Observability-Change: this extraction moves only the webhook trigger
store, its SQL text, and its DDL. The claim, handoff, and failure statements
are byte-identical, the callers construct the same store, and no metric,
span, or log name changes.

No-Regression Evidence: focused `postgres` package tests plus the webhook
claim-order and dedupe tests run green on the moved tree; the SQL text is
unchanged so no plan or lifecycle proof is re-owed.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
