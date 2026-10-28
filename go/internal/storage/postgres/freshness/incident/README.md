# Postgres incident freshness trigger store

## Purpose

This package persists PagerDuty and Jira webhook wake-up triggers
(`incident_freshness_triggers`) so the workflow coordinator can claim, hand
off, or fail them without losing a trigger to a crash between claim and
completion. It is the `freshness/incident/` leaf of the storage/postgres
split (#6693).

## Ownership boundary

This package owns the DDL, upsert, claim, and completion SQL for
`incident_freshness_triggers`, and the `IncidentFreshnessStore` type that
runs them. `cmd/webhook-listener` builds the store to normalize and coalesce
inbound PagerDuty/Jira events; `cmd/workflow-coordinator` builds it to claim
queued triggers, hand them off to a planned workflow, or fail them.
`internal/webhook` owns the `IncidentFreshnessTrigger`/`TriggerStatus`
domain types this package persists and reads back.

This is one of the three `freshness/{aws,gcp,incident}/` trigger stores that
share a `StoreTrigger`/`ClaimQueuedTriggers`/`MarkTriggersHandedOff`/
`MarkTriggersFailed` lifecycle the coordinator and webhook listener build
(#6693 decision N4). `vulnerability/`'s source-state store has none of that
lifecycle and did not follow them into this split.

## Exported surface

- `IncidentFreshnessStore` with
  `NewIncidentFreshnessStore(database db.ExecQueryer)`.
- `EnsureSchema`, `StoreIncidentFreshnessTrigger`, `ClaimQueuedTriggers`,
  `ReapExpiredTriggerClaims`, `MarkTriggersHandedOff`, `MarkTriggersFailed`.
- `IncidentFreshnessSchemaSQL()` returns the DDL.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows`
  contracts.
- `internal/webhook` for the `IncidentFreshnessTrigger`/`TriggerStatus`
  domain types this package persists and scans.

## Telemetry

None of its own. `cmd/webhook-listener` and `cmd/workflow-coordinator` wrap
the injected `db.ExecQueryer` in their own `InstrumentedDB`
(`StoreName: "incident_freshness_triggers"`) before constructing this store,
so call-level tracing and metrics are the caller's responsibility, not this
package's.

Observability Evidence: the coordinator records one
`eshu_dp_workflow_coordinator_incident_freshness_reap_total` /
`eshu_dp_workflow_coordinator_incident_freshness_reap_duration_seconds`
pass and the `eshu_dp_workflow_coordinator_incident_freshness_stuck_claimed`
gauge per reap (`RecordIncidentFreshnessReap`), and logs a WARN with
`reclaimed_count` whenever a pass reclaims rows. The store itself still owns
no instruments: callers wrap the injected `db.ExecQueryer` in their own
`InstrumentedDB` (`StoreName: "incident_freshness_triggers"`).

No-Regression Evidence: the claim predicate and order are unchanged from the
pre-lease statement (`status = 'queued'` ordered by `received_at`,
`trigger_id` with `FOR UPDATE SKIP LOCKED`), so the existing
`incident_freshness_triggers_status_received_idx` keeps serving claims; only
the `SET` clause gains the lease assignment and token bump. Live recovery
proof against PostgreSQL 18.3 (isolated schema, dropped after):
`TestIncidentFreshnessStoreReapExpiredTriggerClaimsLive` (2 rows: 1
expired-lease reclaimed to `queued`, 1 live-lease untouched, second pass a
no-op; 0.15s), `...StaleHolderCannotCompleteReapedClaimLive` (stale
hand-off and stale failure each affect 0 rows, redelivery coalesces with
`duplicate_count` 1 and status still `claimed`, new-owner hand-off lands;
0.12s), `...ConcurrentSafety` (10 rows, 5 concurrent reapers, each row
reclaimed exactly once; 0.11s). Row counts are terminal-state assertions,
not samples: every claimed row is reclaimed exactly once and no stale write
lands.

Performance Evidence: `EXPLAIN` of the reap predicate on PostgreSQL 18.3
uses `Index Scan using incident_freshness_triggers_claimed_lease_idx`
with the `(claim_expires_at IS NOT NULL, claim_expires_at < now())` index
condition and presorted `(claim_expires_at, trigger_id)` order, so a reap
pass reads only the claimed set however much `handed_off` history the table
holds. Migration 238's `ADD COLUMN` statements are catalog-only (constant
`DEFAULT 0`, `NULL` lease) and migration 239's backfill `UPDATE` touches
only rows in `status = 'claimed'` (#7961 DDL/DML split); both are
`IF NOT EXISTS` no-ops where the listener already ran `EnsureSchema`. Baseline: before this change a crash between
claim and commit stranded rows at `claimed` permanently (no lease column,
no reap); after, the same crash is reclaimed on the next tick and fenced
against dual completion, with no new hot-path index beyond the
claimed-only partial one.

## Gotchas / invariants

- `ClaimQueuedTriggers` sets a `claim_expires_at` lease
  (`claimedAt+leaseDuration`) so a mid-batch handoff abort or coordinator
  crash cannot strand a row at `claimed` forever.
- Every claim bumps `claim_fencing_token`, and `MarkTriggersHandedOff`/
  `MarkTriggersFailed` only complete a row whose current token still matches
  the token the caller received from `ClaimQueuedTriggers`: a stale claimant
  whose lease was reaped and re-claimed by a different owner cannot complete
  a claim it no longer holds.
- `StoreIncidentFreshnessTrigger`'s `ON CONFLICT (freshness_key)` upsert
  never overwrites a `claimed` row's ownership fields; it only bumps
  `duplicate_count` and lets a genuinely new (non-claimed) trigger's fields
  replace the stored ones.
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
