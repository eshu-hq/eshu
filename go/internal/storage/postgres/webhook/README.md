# Postgres webhook trigger store

## Purpose

This package persists provider webhook intake decisions for later targeted
repository refresh handoff: dedupe by refresh key, ordered claiming with
`FOR UPDATE SKIP LOCKED`, and handoff/failure recording.

## Ownership boundary

This package owns the `webhook_refresh_triggers` table, its DDL, and the
claim/handoff/failure lifecycle on those rows. `internal/webhook` owns the
trigger domain types and intake decisions. The parent `postgres` package,
`cmd/webhook-intake`, `cmd/ingester`, and `cmd/collector-git` own wiring:
they construct the store and call it. `internal/query` and the collectors
own what happens after handoff.

## Exported surface

- `WebhookTriggerStore` with `NewWebhookTriggerStore(database db.ExecQueryer)`.
- `EnsureSchema`, `StoreTrigger`, `ClaimQueuedTriggers`,
  `MarkTriggersHandedOff`, `MarkTriggersFailed`.
- `MarkIgnoredDecisionTriggersFailed`: the same-tick ignored-decision
  terminal. It keeps the `(trigger_id, fencing token, claimed)` fence and
  adds `AND decision <> 'accepted'` at UPDATE time, so a row flipped back
  to accepted under the live claim is skipped for the handoff instead of
  failed; it returns only the rows actually terminated (#7795).
- `ReapExpiredTriggerClaims`, `CountStaleClaims` (#7661 lease recovery;
  the reap also fails stale claims whose decision is not accepted with the
  `ignored_decision` class instead of requeueing them, #7795).
- `HasQueuedAcceptedTriggers`: the read-only queued probe the scheduled
  sweep consults before yielding to a trigger (#8123). It never claims;
  claiming stays with `ClaimQueuedTriggers`.
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
- The refresh-key upsert keeps status tracking the latest decision across
  the queued/ignored pair in both directions; `claimed`, `handed_off`,
  and `failed` rows stay sticky (#7795).
- A nil database fails fast with an explicit error, never a nil panic.
- The bootstrap migrations (`017_webhook_refresh_triggers.sql` plus
  `168_webhook_refresh_triggers_claim_fencing.sql`) must produce the same
  table as `EnsureSchema`. Only the serving satellite hosts run
  `EnsureSchema` — `cmd/webhook-intake` (the five trigger stores; the
  in-tree `cmd/webhook-listener` was removed in #8876); the ingester and
  `collector-git` claim, hand off,
  and reap on whatever the bootstrap built. Before #7777 a bootstrap-only database lacked
  `claim_fencing_token` and the `claimed_at` reap index, so every trigger
  write failed until a listener started. The live test
  `TestWebhookTriggerStoreRunsOnBootstrapSchemaLive` runs the lifecycle on a
  bootstrap-only schema. It fails if `EnsureSchema` would add any column or
  index there, or if the columns and indexes differ from the ones
  `EnsureSchema` builds alone on an empty schema.
- Do not import the parent `postgres` package: that is an import cycle.
  Root tests exercise this package through its exported constructors.

## Related docs

- [Postgres storage](../README.md)
- [Shared database contracts](../db/README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
