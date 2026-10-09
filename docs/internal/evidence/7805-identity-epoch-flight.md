# Identity epoch cache: shared flight, active-set epoch, ordered page

Root-Cause Evidence: In the production environment the `container_image_identity`
domain finished one item every 10.7 minutes after a maintenance reopen of about
130 items. Three defects combined (#7805).

1. `IdentityEpochCache.get` made waiters of an in-flight load recurse into
   `get`, re-probe, and queue behind a new serial load. A load whose epoch moved
   mid-load was returned uncached to its leader only. The waiters never received
   the leader's rows.
2. The epoch probe counted identity facts over all generations. Retention
   deletes of superseded-generation rows moved the count every few minutes, which
   is shorter than one load, so nearly every load was discarded.
3. The page query joined `ingestion_scopes` and `scope_generations` to
   `fact_records`. The planner drove it from the roughly 1,400 active scopes,
   read the whole active identity set, and top-N sorted it for every 500-row
   page, so one full load was quadratic.

Shim: throwaway native PostgreSQL 18 cluster, schema from the repo migrations,
1,400 active scopes (200 OCI registry, 60 cloud account, 1,140 git), 2.95M
`fact_records` rows, 1,013,982 identity facts of which 546,562 are in active
generations. The load order and the active set size match the production
observations (production: 1,027,363 identity facts, a 618 to 640 s load).
Host load average during the runs was 7 to 17 (shared laptop), so absolute
seconds carry that noise. Before and after ran on the same cluster and corpus.

Raw captures: the after-load and after-drain runs are saved beside the
report. The before-load run (494.9 s and 504.9 s, digest 9875824362cef277)
was recorded in session output only; its raw capture is NOT_CHECKED until the
base-predicate run is repeated and saved.

Performance Evidence: One full load of the active identity set
(`FactStore.loadIdentityFactsUncached`, 500-row keyset pages, 546,562 rows):

| Page query | Run 1 | Run 2 | Rows | Ordered fact-id digest |
| --- | --- | --- | --- | --- |
| JOIN form (before) | 494.9 s (8 min 15 s) | 504.9 s (8 min 25 s) | 546,562 | 9875824362cef277 |
| Hashed SubPlan form (after) | 4.99 s | 4.77 s | 546,562 | 9875824362cef277 |

That is about 100 times faster, with an identical ordered row set. The
per-page `EXPLAIN (ANALYZE, BUFFERS)` on the same shim, first page and a page
at row 270,000: JOIN form 634 ms and 359 ms (1,676,069 and 844,240 shared
buffer hits, `Sort` over 546,562 and 274,261 rows); hashed SubPlan form 2.5 ms
and 2.5 ms (`Index Scan using fact_records_identity_epoch_idx_v2`, no `Sort`,
500 rows after removing 428 and 407 superseded rows by filter). A rewrite as
`EXISTS` or `IN (subquery)` is pulled up into the same semi-join and keeps the
slow plan. A `= ANY(ARRAY(subquery))` filter chose a bitmap scan and was slower
(821 ms and 3,135 ms). Only the `OR FALSE` form, which blocks the sublink
pull-up, keeps the ordered index scan.

Epoch probe, `EXPLAIN (ANALYZE, BUFFERS)` on the same shim:

| Probe | Time | Shared buffer hits | Notes |
| --- | --- | --- | --- |
| All generations (before) | 59 to 65 ms | 446,946 | Index-only scan, 1,013,982 rows counted |
| Active set, JOIN form | 550 to 593 ms | 1,676,082 | Scope-driven nested loop, rejected |
| Active set, hashed SubPlan (after) | 120 to 135 ms | 51,015 | Parallel bitmap heap scan, 546,562 rows counted |

The after probe is about twice the before probe and needs no new index. It
reads the heap for `scope_id` and `generation_id` because the partial index
holds only `(observed_at, fact_id)`. An index that includes `generation_id`
would bring it back to an index-only scan; that needs a migration and is not
part of this change.

Drain of 130 items, 8 workers, 2 s of handler work per item, with the new
code before the in-flight retry was added (no epoch move happened during the
run, so the retry path was not exercised), a superseded-generation row deleted every 10 s during the run (four
deletes) on the same shim: 42.9 s total (about 10,900 items per hour), one
load started (`reload_total` 1), 122 cache hits, 7 waiters served by the shared
flight, no discarded load. Measured. The before drain was not run: with a 495 to
505 s load and an epoch that moves every few minutes the cache never populates,
so each load serves one item. That gives about 130 loads of about 8.4 minutes
each (about 18 hours at the shim load time), consistent with the production
backlog projection of more than 12 hours. This before figure is an estimate from
the measured single-load time and the production observation, not a measured
drain.

Concurrency: one flight serves every caller that joined it. Arbiter ruling
(clarification of "every waiter gets the flight rows"): a waiter joins an
in-flight load only if its trigger committed before the leader's probe
snapshot, which the code checks as "the waiter's own epoch probe equals the
flight's start epoch". Otherwise the waiter waits for the flight and then
starts or joins a fresh load, so it never decides on a set that predates its own
trigger. `TestIdentityEpochCacheLateCallerDoesNotJoinStaleFlight` pins it.

The paged load is more than a thousand READ COMMITTED statements, so a
generation flip between pages can tear the set (old-generation rows after the
cursor drop out, new-generation rows before the cursor are never read). The
post-load probe detects the flip. The flight then loads once more from the moved
epoch (at most two attempts, `eshu_dp_identity_cache_load_retry_total`). If the
epoch is still moving after the second attempt, or the post-load probe fails,
the leader keeps its last set uncached and every waiter re-probes
(`flight_waiter_total{outcome="torn_set"}`), so a possibly mixed set is never
delivered to a waiter. A single long REPEATABLE READ snapshot was refused: it
pins the vacuum horizon and blocks `CREATE INDEX CONCURRENTLY`. Snapshot
consistent paging without a long snapshot is a follow-up, not part of this
change. Unit tests
`TestIdentityEpochCacheRetriesInsideFlightWhenEpochMovesDuringLoad`,
`TestIdentityEpochCacheNeverDeliversTornSetToWaiters`,
`TestIdentityEpochCacheSharedFlightServesEveryWaiterWhenEpochStable`,
`...LeaderCancelDoesNotFailWaiters`, `...LoadErrorIsSharedWithWaiters`, and
`...LeaderPanicReleasesWaiters` cover these paths under `-race`.

Plan guard: the page SQL is Postgres, and `internal/queryplan` pins graph
(Cypher) reads only, so it has no entry for this query. The guard is
`TestIdentityPageQueryPlanRidesOrderedIndexLive`: on a private schema with the
production tables and indexes it runs `EXPLAIN (FORMAT JSON)` of the page query
and asserts an Index Scan on `fact_records_identity_epoch_idx_v2`, a hashed
SubPlan filter, no Sort, and no Seq Scan on `fact_records`. The rewrite depends
on planner behavior verified on PostgreSQL 18; that test pins it, and the
text-shape asserts only stop the `OR FALSE` from being deleted.

Correctness: `TestIdentityEpochIgnoresSupersededGenerationRowsLive` shows a
delete on a superseded generation leaves the epoch unchanged and an insert or
delete on the active generation moves it (before the change the first assertion
failed with count 1,013,982 then 1,013,981).
`TestIdentityPageQueryServesOnlyActiveGenerationsLive` serves only a
non-tombstone identity fact of a scope's active generation whose generation row
is `active`.

Observability Evidence: `eshu_dp_identity_cache_reload_total` counts loads
started. `eshu_dp_identity_cache_passthrough_total` gained a `reason` label
(`epoch_moved`, `cap_exceeded`, `size_unknown`, `probe_error`) and records every
discarded load. The new `eshu_dp_identity_cache_flight_waiter_total` has an
`outcome` label (`shared`, `shared_error`, `stale_epoch`, `leader_canceled`, `torn_set`) and
counts callers that joined or queued behind a load. The new
`eshu_dp_identity_cache_load_retry_total` counts loads repeated inside a flight.
`eshu_dp_identity_cache_reload_duration_seconds` now records every load, not
only cached ones. At 3 AM: many `shared` per `reload_total` is healthy; a high
`passthrough_total{reason="epoch_moved"}` or `stale_epoch` count means the
active set moves faster than a load.

Claim fairness (`reducer_queue_batch_query.go`, the `locked` CTE ordering) is
not changed by this work. NOT_CHECKED: no RED/GREEN for a one-line ordering
change that gives held `deployable_unit_correlation` rows a slot.
