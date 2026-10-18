# #8247: corpus completeness-fence gauges

## Problem

`deployable_unit_correlation`, `workload_materialization`, and
`service_catalog_correlation` defer behind the global relationship
completeness fence (`incompleteScopeRelationshipGenerationsPredicate`).
The deferral is non-counting and never dead-letters, so a consumer can wait
for weeks (21.6 days measured in QA) with no operator signal naming how
many scopes hold the fence or how old the oldest waiter is.

## Change (telemetry only)

The fence predicate and claim behavior are unchanged. Three observable
gauges are served by the reducer from its Postgres gauge snapshot
(gauge `reducer_corpus_fence`):

- `eshu_dp_corpus_fence_holders`: active scopes matching the fence's own
  predicate; zero means the fence is open.
- `eshu_dp_corpus_fence_deferred_consumers`: per closed `domain`,
  `retrying` rows in the consumers' non-counting resolution-not-ready
  classes on the active generation. Includes consumers waiting on
  their own relationship generation, not only the global fence.
- `eshu_dp_corpus_fence_oldest_deferred_age_seconds`: per closed
  `domain`, `now - MIN(created_at)` over the same rows (own-generation
  waiters included).

One statement (`corpusFenceGaugesQuery`) feeds all three. It runs only
from the background snapshot refresher at
`ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL`, never on the claim or scrape
path, so its cost is independent of claim rate.

## No-Regression Evidence:

Baseline: no fence-gauge statement existed; the claim path evaluated the
fence predicate per pass and still does, untouched. The new statement
therefore adds cost only at refresh cadence, never per item.

After measurement (`TestQueueObserverStoreCorpusFenceCountsRunOnPostgres`,
disposable local `postgres:16-alpine`, production bootstrap schema in an
isolated schema, enrolled in the live-postgres-readiness runner):

- Input shape: 2 scopes (1 holder, 1 open), 5 seeded consumer rows
  (1 deferred deployable at 48h, 1 deferred workload at 2h, plus
  pending, stale-generation, and other-class rows that must not count),
  and 20,000 succeeded filler `fact_work_items`.
- Result exactness: holders = 1; deferred = 1 per domain with the
  pending, stale-generation, and other-class rows excluded; oldest
  deployable age = 172,800s (48h) exactly.
- `EXPLAIN (ANALYZE, BUFFERS)`: execution time 0.256 ms, 23 shared
  buffers for the outer join; the fence `EXISTS` probes
  `fact_work_items_stage_domain_status_idx` and the deferred probe
  scans `fact_work_items_status_idx` on `status = 'retrying'` — no
  sequential scan of `fact_work_items` at 20k rows. Whole test
  (isolated-schema bootstrap included) took 3.89s.

Safety: the holder count reuses the fence predicate verbatim, so the
gauge cannot disagree with the gate. A failed refresh publishes nothing,
so the previous snapshot ages instead of a false open fence appearing.
No index, schema, lock, or queue change is made.

## Observability Evidence:

The three gauges above are the signal; tests prove emission through the
production path: `TestRegisterCorpusFenceObservableGaugesReportsFenceState`
(holders 3, deferred 782, oldest age 1,866,240s observed on the meter)
and `TestPostgresBackedGaugesServeCorpusFenceSnapshot` (scrapes serve
the refreshed snapshot with no extra store read per scrape). Refresh
health rides the existing `eshu_dp_gauge_snapshot_*` family under
`gauge="reducer_corpus_fence"`.

## No-Regression Evidence (#8511):

Baseline: three gauge callbacks each called `CorpusFenceCounts` once per
scrape, so every scrape read the snapshot three times.

After measurement
(`TestRegisterCorpusFenceObservableGaugesReadsObserverOncePerScrape`,
in-memory fake observer plus OTEL Go SDK `ManualReader` v1.45.0):

- Input shape: holders 3, deferred
  `{deployable_unit_correlation: 782}`, oldest
  `{deployable_unit_correlation: 417}`.
- Result: exactly 1 `CorpusFenceCounts` call per `Collect`, over two
  consecutive scrapes (1 then 2 cumulative). The same triple is
  observed, so values are identical and share one refresh generation.

Safety: the single read returns the same holders/deferred/oldest
triple the three reads returned, only more consistent (one
generation, never mixed). The map iterations moved into one closure
unchanged. Production still serves the background Postgres snapshot,
so a scrape still never queries Postgres.

## Observability Evidence (#8511):

No new instruments and no gauge name or label change, so dashboards
and alerts are unaffected; only description text was refined.
`scripts/verify-telemetry-coverage.sh` passes: coverage docs and
`instruments.go` agree with no new untracked stages.
