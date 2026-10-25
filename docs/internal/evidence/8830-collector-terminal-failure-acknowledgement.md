# Collector terminal failure acknowledgement (#8830)

Evidence for the operator acknowledgement of a collector terminal failure, built
on the collector terminal-failure readiness read (#8803, #8906, #8960). The
reference is `docs/public/reference/http-api/collector-terminal-failure.md`. The
design follows the binding arbiter ruling arb-8830 (option B, episode
acknowledgement).

## Decision: what "a newer failure" means

`workflow_work_items` holds one row per collector target per cycle (see
`docs/public/reference/telemetry/workflow-work-item-retention.md`). A target that
is still configured and keeps failing writes a new `failed_terminal` row with a
later `updated_at` every cycle (planner work item ids are
`kind:instance:generation`, and the failed_terminal write sets `updated_at` from
the worker clock). A watermark on the acknowledged row's `updated_at` would lapse
within one cycle for exactly the case the issue describes, so it is rejected, and
a watermark on `now()` is rejected because it mixes the server clock with the
worker clock.

An acknowledgement covers an episode. Let `k` be the acknowledgement and `t` a
`failed_terminal` row of the same (kind, instance, scope). `t` is covered when
`t.updated_at <= k.failure_at`, or when `t.last_failure_class IS NOT DISTINCT FROM
NULLIF(k.failure_class, '')` and no completed row `c` of the target has
`k.failure_at < c.updated_at AND c.updated_at < t.updated_at`. It is reported
again after a recovery (a completed row, then a failure) or on a different raw
class. The first branch keeps older rows of the acknowledged scope from surfacing
once the newer ones are suppressed.

The predicate runs in SQL. A Go-side filter on the single `LIMIT 1` row the reader
used to return would hide an older unacknowledged failure on another scope of the
same instance, so the reader now returns up to two rows per instance: the newest
unresolved failure no acknowledgement covers, and the newest covered one.

Read model (AC3): an acknowledged failure is a distinct `AcknowledgedFailure`
sub-record. An instance whose only unresolved failures are acknowledged reads
health `partial` and `promotion_state=partial` with the blocker `terminal fetch
failure acknowledged: <class> (<operation>)`. It never reads healthy, registered,
observed, or implemented, and it is not kept failed. A real unacknowledged
failure on another scope still wins by health rank. The operator's reason is on no
status surface, MCP tool, metric, or audit event; it is returned only by
`POST /api/v0/admin/collector-failures/acknowledgements/query`.

Deviations from the ruling's wording, both from the repo's own rules: the route
lives in the leaf `go/internal/query/admin/acknowledgement` (the admin tree forbids
new route families in package `admin`; it uses the admin Store's ledger), and its
OpenAPI fragment is its own file because `paths/status/admin.go` is at the 500-line
cap. The acknowledgement names the newest unresolved failure at the time of the
write and stores its observed raw class; there is no request-side class guard.

## Plan proof (before the code)

Fixture, the #8906 shape: PostgreSQL 18.6 (`postgres:18-alpine`, aarch64),
`shared_buffers=256MB`, all migrations applied, 542,880 `workflow_work_items`.
One instance `sa-hot` (15m) fails 40 targets for 6,912 cycles (276,480
`failed_terminal` rows); `sa-recovered` (15m) fails every target each cycle and
completes it 5 minutes later for 2,880 cycles (115,200 failed, 115,200
completed); 18 `git` instances with 2,000 completed rows each. Generation ids are
per cycle. The statement is prepared with the 20 instances (two 45 minute cutoffs,
eighteen 30 minute cutoffs). `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)`, warm, fifth
consecutive run.

| Statement | State | Execution time | Sorts | Temp |
| --- | --- | --- | --- | --- |
| main (before) | none acknowledged | 1.70 ms | outer only | none |
| shipped | none acknowledged | 1.96 ms | outer only | none |
| shipped | hot instance's `scope-hot-0` acknowledged | 3.23 ms | outer only | none |
| shipped | all 40 hot scopes acknowledged | 2.22 ms | outer only | none |

The budget was warm within 2x of 1.88 ms (3.76 ms) with no inner Sort and no temp:
met. The acknowledged state costs two extra completed-probe passes over the hot
scope's 6,912 never-completed rows (about 1 ms each), which is why it is the worst
case here; with nothing acknowledged the added cost is the per-instance
One-Time-Filter probe (+0.25 ms).

Formulations measured and rejected (same fixture, hot scope acknowledged):

| Formulation | Time | Why rejected |
| --- | --- | --- |
| Both laterals as plain `NOT EXISTS`/`EXISTS` anti-joins | 4.0 ms | The planner runs the completed probe on the covered row before rejecting it; three hot-scope probes instead of one. |
| Cover test and completed check inside a `CASE` | 52 ms | The completed probe becomes a correlated SubPlan with a bitmap AND over 5,769 rows. |
| Subquery fence (`OFFSET 0`) before the completed probe | 46 ms, two Sorts | Sorts the whole window of every instance. |
| `LEFT JOIN` of the acknowledgement table | 7.4 ms, two Sorts | The completed probe still runs first. |
| Scope pre-filter on both laterals | 2.63 ms, inner Sort | The acknowledged lateral sorts the covered rows, violating the no-inner-Sort budget. |
| Scope pre-filter on the `u` lateral, plain join on the `a` lateral (shipped) | 2.9 to 3.2 ms, no inner Sort | Meets the budget. |

A partial index on completed rows would remove most of the probe cost (it cut an
earlier, different fixture from 24.2 ms to 3.9 ms), but it adds an index entry to
every completed work item and its write cost is unmeasured, so it is not part of
this change.

## Performance Evidence

Performance Evidence: on the 542,880-row fixture above, the collector terminal
failure status read takes 1.70 ms on main and 1.96 ms with this change when
nothing is acknowledged, and 3.23 ms with one hot scope acknowledged (budget 3.76
ms), with one outer Sort and no temp blocks. The live plan test
`TestCollectorTerminalFailureReadUsesPartialIndexLive` asserts the unacknowledged
and the acknowledged state keep the
`workflow_work_items_failed_terminal_instance_updated_idx` seek, with no Sort
beyond the outer instance sort.

## Failed-scope discovery (arb-8830b ruling-2)

The first ruling named `POST /api/v0/admin/work-items/query` as the way to find a
failed scope's id. That premise was wrong, and the ruling's VERIFIED item 17 is
withdrawn: that route reads `fact_work_items` (the projector and reducer queue),
not `workflow_work_items`, and has no instance filter. No route listed collector
work items by instance, and no status surface carries a scope id, so an operator
could not learn the scope id the acknowledge route needs.

`POST /api/v0/admin/collector-failures/query` (admin gate, one instance, keyset
paged by scope) and `eshu admin collector-failure list` close it. The status
surfaces keep no scope id: a scope id embeds an org, repository, or account
(`security-alert:github:eshu-hq/eshu`), and the "unscoped" status payload reaches
browser sessions and shared-key callers who are not behind the admin gate. The list
is built from the status read's window, the new shared
`workflowTerminalFailureResolvedBy`, and `workflowTerminalFailureAckCovers`, so what
it lists is what status counts. Its completed probe adds a window bound of its own:
without it the planner hashes every completed row of the instance.

Plan proof, same 542,880-row fixture, warm, fifth consecutive run, limit 100:

| Statement | State | Execution time | Index rows read |
| --- | --- | --- | --- |
| list, `sa-hot` | none acknowledged | 0.65 ms | 160 (in-window) |
| list, `sa-hot` | `scope-hot-0` acknowledged | 0.61 ms | 160 |
| list, `sa-hot` | all 40 scopes acknowledged | 0.77 ms | 160 |
| list, `sa-recovered` (115,200 completed rows) | none | 0.34 ms | 120 |
| list, `sa-recovered`, probe unbounded (rejected) | none | 35.8 ms | 120, plus a hash of every completed row |
| status read (unchanged) | none / hot scope / all 40 acknowledged | 1.84 / 2.95 / 2.14 ms | |

The list seeks `workflow_work_items_failed_terminal_instance_updated_idx` with the
window cutoff in the index condition, reads only in-window rows (about 40 scopes x 4
rows), sorts only those rows keyed on scope (never the failed history), and uses no
temp blocks. The status read gained no predicate; only the text of its completed
probe moved into the shared constant.

Mutations of the list, each turning a live test red: drop the window cutoff
(`SemanticsLive`, `UsesTheWindowIndexLive`); drop the completed anti-join
(`SemanticsLive`); drop the cover predicate (`SemanticsLive`, the discovery loop);
drop `DISTINCT ON` (`SemanticsLive`, `UsesTheWindowIndexLive`).

## Concurrency and idempotency proof

All under `ESHU_POSTGRES_DSN`, `-race`:

- `TestTerminalFailureAckStoreConcurrentLive`: 16 concurrent acknowledgements of
  one failure all succeed and leave one row (`ON CONFLICT DO UPDATE` on the
  primary key).
- `TestTerminalFailureAckStoreRacesFailureWritesLive`: eight rounds run an
  acknowledgement against `failed_terminal` writes of two classes for the same
  scope on other connections. No deadlock; after each round the status read
  equals an independent Go oracle of the predicate over the rows and the stored
  acknowledgement. The acknowledgement stores the failure it read (its class and
  `updated_at`), so a failure of another class newer than it is reported, never
  suppressed.
- Handler `TestAcknowledgeConcurrentSameKeyWritesOnce`: eight concurrent requests
  with one idempotency key reach the store once (the ledger claim serializes
  them); the others answer 200 duplicate or 409 in progress.
- The write is one READ COMMITTED transaction touching only the acknowledgement
  row. Failure writers lock `claimed` work item rows, so the sets are disjoint and
  there is no lock-order cycle.

Rollback: the migration only adds a table. Dropping it restores the previous
reader's behavior only after rolling the binary back, because the new reader joins
the table.

## Seeded-violation proofs

Each mutation of the shipped SQL or store was applied, the live tests run, and the
change reverted. Every one turned a live test red:

| Mutation | Failing live tests |
| --- | --- |
| Drop the class compare | `RecordsAndSuppresses`, `RacesFailureWrites`, `AcknowledgementSemantics` |
| Drop the completed probe | `AcknowledgementSemantics` |
| Drop the first branch (`t.updated_at <= k.failure_at`) | `RecordsAndSuppresses`, `RacesFailureWrites`, `AcknowledgementSemantics` |
| Collapse to one lateral (the acknowledged lateral returns nothing) | `RecordsAndSuppresses`, `Concurrent`, `RacesFailureWrites`, `AcknowledgementSemantics`, `ReadUsesPartialIndex` |
| Drop the `u` lateral's cover (suppress nothing) | `RecordsAndSuppresses`, `RacesFailureWrites`, `AcknowledgementSemantics`, `ReadUsesPartialIndex` |
| Drop the acknowledged lateral's unresolved anti-join (a recovered scope kept reading acknowledged) | `AcknowledgementSemantics` (the `resolved` case) |
| Ignore the window in the acknowledgement target read | `Refusals` |

## Observability Evidence

Observability Evidence: new counter
`eshu_dp_collector_terminal_failure_acknowledgements_total{outcome}`
(`acknowledged`, `duplicate`, `refused`), new gauge
`eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances` by
`service_name`, `collector_kind` and `failure_class` (the same 41-value bound as the existing
gauge, which now counts only unacknowledged failures), one governance audit event
per decision with a bounded reason code and the hashed actor, and one INFO log.
The route is covered by the request span.

At 3 AM an operator sees the instance `partial` with the acknowledged blocker and
the `acknowledged_failure` object on collector readiness, the acknowledged gauge
non-zero and the failure gauge quiet, the counter's `acknowledged` event, and the
audit event for who did it; the reason is one admin query away.

## Known limits

- One call acknowledges one instance and one scope.
- The acknowledgement has no expiry and no sweeper. A scope re-added later that
  fails with the same class and no success in between stays acknowledged, and
  still reads partial.
- The class compare is on the raw persisted class. A free-text class outside the
  bounded vocabulary that varies per cycle lapses the acknowledgement.
- A removed instance or purged scope leaves an inert row, bounded by the number of
  (instance, scope) pairs ever acknowledged.
- Clock skew between workers for `updated_at` is untouched (the completed probe
  already depends on it).
