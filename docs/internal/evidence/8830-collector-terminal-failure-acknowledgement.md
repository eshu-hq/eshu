# Collector terminal failure acknowledgement (#8830)

Evidence for the operator acknowledgement of a collector terminal failure,
built on the collector terminal-failure readiness read (#8803, #8906, #8960).
The reference is `docs/public/reference/http-api/collector-terminal-failure.md`.

## Decision: what "a newer failure" means

`workflow_work_items` keeps one row per collector target per cycle (see
`docs/public/reference/telemetry/workflow-work-item-retention.md`). A target
that is still configured and keeps failing writes a new `failed_terminal` row
with a later `updated_at` every cycle. A removed target stops failing and ages
out of the 3 x `scan_interval` window on its own, so the case an operator needs
help with is the target that is still planned and fails every cycle: a deleted
repository still on an allowlist, a credential revoked on purpose.

Two readings of AC2 ("a newer terminal failure on that scope is reported
again") were weighed:

- A watermark on the acknowledged row's `updated_at`. Any later row on the scope
  reports again, so the next cycle undoes the acknowledgement for exactly the
  case it exists for.
- An episode. The acknowledgement covers the acknowledged failure and every
  later failure on the same scope with the same `last_failure_class`. It stops
  covering when a completed item lands on the scope after the acknowledged
  failure (a recovery, so the next failure is new) or when the class changes
  (a different failure).

The episode reading is implemented. The team lead was asked to confirm it
before the work finished (see the PR body). The acknowledgement stores the
failure it names (class, `failure_at`), never a work-item id, so retention
(#8829) can delete the failed row without touching it.

Acknowledged health reading (AC3): an acknowledged failure no longer degrades
the instance or fails its promotion proof, and it is not a blocker, but the
failure fields stay on the row with `last_failure_acknowledged`, its time, and
its reason, the readiness next action says it is acknowledged, and the
terminal-failure gauge moves it to its own `..._acknowledged_instances` series.
It is therefore never indistinguishable from an instance that did not fail.

## Data and read shape

Migration 231 adds `collector_terminal_failure_acknowledgements`, primary key
`(collector_kind, collector_instance_id, scope_id)`. One acknowledgement per
target; acknowledging a later episode replaces it.

A failed row `t` is covered by acknowledgement `k` of the same target when
`t.updated_at < k.failure_at` (history the acknowledged failure superseded) or
when `t` has the acknowledged class and no completed row of the target is newer
than `k.failure_at`. `workflowTerminalFailureAckCovers` is the one definition,
used by both status laterals and by the write's "already covered" check.

The status read returns the newest unresolved failure that no acknowledgement
covers (the `u` lateral). Only when there is none does the `a` lateral return
the newest covered one, so an acknowledged failure never hides an
unacknowledged failure on a sibling scope. The `a` lateral sits behind a
One-Time Filter on `u` finding nothing.

## Performance Evidence

PostgreSQL 18.6, 542,800 work items: one instance (`hot`) with 276,000
`failed_terminal` rows across 460 scopes and 600 cycles, and one instance
(`other`) with alternating `failed_terminal` and `completed` rows (266,800), 15
minute cycles, `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)`, warm, five runs, last
run reported. Both instances are read in one statement.

| Case | Execution time | Shared buffers hit |
| --- | --- | --- |
| Before (query on main), no acknowledgement | 24.2 ms | 14,408 |
| After, no acknowledgement | 25.0 ms | 14,410 |
| After, 1 scope of `hot` acknowledged | 25.2 ms | 14,440 |
| After, all 460 scopes of `hot` acknowledged (worst case) | 208.9 ms | 110,337 |
| After, all 460 acknowledged, then a completed row on each scope | 23.4 ms | 14,548 |

The unacknowledged path is unchanged within noise (24.2 ms to 25.0 ms, +2
buffers). Each acknowledged scope with failures inside the window adds about
0.45 ms: the covering predicate probes the completed rows of that scope, and the
existing per-candidate "no later completed row" probe has the same cost shape
(`workflow_work_items_collector_scope_generation_updated_idx` has
`generation_id` between `scope_id` and `updated_at`, so the `updated_at` bound is
a filter inside the scope's index range). The worst case acknowledges every
failing scope of a 460-scope instance, which no operator workflow produces; ten
acknowledged scopes cost about 5 ms.

A candidate fix was measured and not shipped: a partial index
`(collector_kind, collector_instance_id, scope_id, updated_at DESC) WHERE
status = 'completed'` takes the no-acknowledgement read from 24.2 ms to 3.9 ms
and the 460-acknowledged case from 208.9 ms to 47.7 ms. It adds an index entry to every
completed work item, and its write cost was not measured, so it is left for the
owner to decide (reported in the PR body) rather than added to this change.

The plan test `TestCollectorTerminalFailureReadUsesPartialIndexLive` pins the
shape: the unacknowledged lateral seeks
`workflow_work_items_failed_terminal_instance_updated_idx` with no Sort, and the
acknowledged lateral is gated by `One-Time Filter: (t.updated_at IS NULL)`.

## Concurrency and idempotency proof

All under `ESHU_POSTGRES_DSN`, `-race`:

- `TestTerminalFailureAckStoreConcurrentLive`: 16 concurrent acknowledgements of
  one failure all succeed and leave one row (`ON CONFLICT DO UPDATE` on the
  primary key; the later writer's reason and actor win, both are valid).
- `TestTerminalFailureAckStoreNeverSuppressesUnseenClassLive`: eight rounds race
  an acknowledgement of `auth_denied` against a `not_found` failure landing on
  the same scope. In every interleaving the `not_found` failure reads
  unacknowledged, because the acknowledgement stores the class it read.
- `TestTerminalFailureAckStoreRecordsAndSuppressesLive`: a repeat, even from
  another actor, is `Unchanged` and keeps the first reason and actor; a
  different class reports again; acknowledging it replaces the earlier row.
- `TestTerminalFailureAckStoreRefusalsLive`: unknown, disabled, and deactivated
  instances, a scope that never failed, a scope a completed item resolved, a
  changed or missing class, and blank or oversized input all refuse with no row
  written.
- `TestCollectorTerminalFailureAcknowledgementSemanticsLive`: the covering rule
  against sibling scopes, twin instances on one scope id, another collector
  kind, history older than the acknowledged failure, recovery, and class change.

The write is two autocommit statements (read the failure, upsert the row it
saw). A failure landing between them is the same episode (still covered) or a
different class (reported); neither is ever suppressed unseen. A client cancel
after the upsert committed leaves the acknowledgement durable and a repeat
answers `duplicate`.

Rollback: the migration only adds a table. Dropping the table makes the status
read fail, so roll the binary back first. No existing data changes.

## Seeded-violation proofs

Each mutation of the shipped SQL or store was applied, the live tests run, and
the change reverted. Every one turned a live test red:

| Mutation | Failing live tests |
| --- | --- |
| Drop the class equality from the covering predicate | `RecordsAndSuppresses`, `AcknowledgementSemantics` |
| Drop the completed-row probe | `AcknowledgementSemantics` |
| Drop the older-history arm | `RecordsAndSuppresses`, `AcknowledgementSemantics` |
| Make the unacknowledged lateral ignore acknowledgements | `RecordsAndSuppresses`, `Concurrent`, `AcknowledgementSemantics` |
| Skip the store's class refusal | `Refusals`, `NeverSuppressesUnseenClass` |
| Make the store always rewrite instead of returning `Unchanged` | `RecordsAndSuppresses` |

## Observability Evidence

- Counter `eshu_dp_collector_terminal_failure_acknowledgements_total{outcome}`
  (`recorded`, `unchanged`, `refused`, `not_found`, `class_changed`, `failed`),
  one event per request, emitted by the admin handler.
- Gauge `eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances`
  by `collector_kind` and `failure_class`, the same bounded labels as the
  existing gauge, which now counts only unacknowledged failures.
- One `admin_recovery_action` governance audit event per decision with the
  bounded reason code and hashed actor; the operator's reason text is not in
  the event.
- INFO log `collector terminal failure acknowledged` with the bounded kind,
  instance, and class.

At 3 AM an operator sees the instance without `promotion_state=failed` and with
`last_failure_acknowledged=true` on collector readiness, the acknowledged gauge
non-zero, the counter's `recorded` event, and the audit event for who did it.

## Known limits

- One call acknowledges one instance and one scope. An instance whose every
  target failed needs one call per scope.
- The acknowledgement reason is plain text on the row and in status payloads
  for callers that may see instance identity. The docs say not to put secrets in
  it.
- Acknowledged rows are never deleted. They are bounded by the number of
  (instance, scope) pairs ever acknowledged.
