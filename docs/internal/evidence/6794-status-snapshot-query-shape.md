# 6794 Status Snapshot Query Shape

Issue #6794 said the status, readiness, and multi-scope routes were slow
because of a per-scope N+1 that resolved generations one scope at a time. The
live evidence does not support that for the status family. This note records
what the evidence shows, the query-shape fix, and how it was measured.

Source binding: "before" is the SQL at `af4971189`. "After" is the SQL on
branch `perf/6794-status-story-read-paths` (the status and story statements are
byte-identical to the development branch the measurements ran on). Both sets
of statements came from the same recording queryer driving
`StatusStore.ReadStatusSnapshotFiltered` with `FullSnapshotSelection()`.

## Diagnosis

The routes `/api/v0/collectors`, `/status/collectors`, `/collector-readiness`,
`/status/collector-readiness`, `/index-status`, `/status/index`, `/ingesters`,
`/status/freshness-causality`, and `/evidence/bundle` each load one status
snapshot through `StatusStore.ReadStatusSnapshotFiltered`. One full snapshot
issues 29 SQL statements in sequence. None of them runs once per scope.

Every statement was run live with `EXPLAIN (ANALYZE, BUFFERS)` on a
production-scale instance (Postgres 18.3). They took about 26s in total.

The Postgres container was at its CPU limit throughout, so all of these
figures are inflated. For example, an in-memory index probe took about 0.15ms,
where microseconds would be normal. That makes the before/after figures below
same-environment relative comparisons, not absolute latency targets.

The expensive statements:

| Statement | Live time | Cause |
| --- | --- | --- |
| `domainBacklogQuery` | 13.1s | The shared-projection half builds a domain list with a `UNION` over every intent row, then `LEFT JOIN`s every intent row back to it. That is two full passes over hundreds of thousands of mostly completed intents, about 11.7s. |
| `stageCountsQuery` | 4.6s | `activeFactWorkItemsCTE` joins `ingestion_scopes` and the active `scope_generations` row once per work item. The planner underestimates the `(scope_id, generation_id)` join by more than two orders of magnitude and chooses per-row index nested loops. |
| `queueSnapshotQuery` | 3.4s | Same CTE. |
| `collectorFactEvidenceQuery` | 1.9s | `active_scopes` is estimated at one row, so the inlined `workflow_instances` `DISTINCT ON` runs again for every summary row. |
| blockages, latest failure | about 1s each | Same CTE. |

Every snapshot evaluates the CTE five times.

`pg_stat_activity` showed that the `SELECT EXISTS (... ingestion_scopes ...)`
and `latest_generations DISTINCT ON` shapes quoted in the issue come from the
reducer and ingester, not from the API. They add to the CPU saturation, but
they are not per-request API work. The per-scope N+1 hypothesis for the status
read path is rejected.

Before any code change, a cheap shim tested the plan theory. Forcing
`enable_nestloop=off` took the stage-counts evaluation from 4.1–5.0s to
0.8–1.6s.

## Change

- **Scope-state CTE.** `activeFactWorkItemsCTE` now resolves each scope's
  active generation once, in `active_fact_work_items_scope_state AS
  MATERIALIZED`. The lookup is a `LEFT JOIN LATERAL ... LIMIT 1`, so it costs
  one primary-key probe per scope, and the work rows hash-join to the result.
  Both markers matter:
  - Without `MATERIALIZED`, the planner inlined the subquery and went back to
    the same nested loops.
  - Without `LIMIT 1`, the planner flattened the lateral lookup into a
    `scope_generations` seq scan, which fails the #4446 plan guard.
- **Pending intents only.** The shared-projection half of `domainBacklogQuery`
  aggregates pending intents (`completed_at IS NULL`) in one pass, then does a
  `FULL OUTER JOIN` to the live leases.
- **Intentional truth fix: phantom lease-only intent.** A domain with a live
  lease and no intent rows at all used to report one outstanding intent. The
  old `LEFT JOIN` produced a null-extended row for it, and
  `COUNT(*) FILTER (WHERE intents.completed_at IS NULL)` counted that row as a
  pending intent. The new query reports zero for such a domain; the domain
  still appears through its in-flight lease. Test sequence:
  - `TestStatusActiveWorkQueriesPreserveSemantics` expects `leaseonly|0|1|...`.
  - It failed on the parity-preserving SQL, which still returned 1.
  - It passes now.

  Downstream effects, all intended: a lease with no pending intent is idle
  worker activity, not backlog. The existing `sharedProjectionBacklog`
  comment in `internal/status/health.go` already treats lease-only
  rows that way. So for a domain whose only signal is a live lease:
  - hosted readiness `shared_projection`
    (`internal/query/status_hosted_readiness.go`) can move from fail to pass;
  - status health can move from progressing to healthy when that lease was the
    only outstanding signal;
  - the `eshu_runtime_domain_outstanding` gauge (`internal/runtime/metrics.go`)
    drops by the phantom 1 for that domain.
- **Collector fact evidence.** `collectorFactEvidenceQuery` now materializes
  `workflow_instances`.
- **One active-work evaluation per snapshot.** The stage counts, domain
  backlog, queue snapshot, conflict blockages, and latest failure used to be
  five statements. Each embedded `activeFactWorkItemsCTE` and evaluated the
  active set again. `activeWorkSummaryQuery` evaluates it once, as a single
  `MATERIALIZED` CTE that holds only the columns the sections read. Each
  section is the original read's unchanged `SELECT`, numbered with
  `ROW_NUMBER()` over that read's own `ORDER BY` and returned as
  `(section, ordinal, to_jsonb(row))`. Postgres still decides row order,
  collation, and the blockage and failure limits. Round trips per snapshot
  drop from 29 to 25. The decoder is strict: a missing or mistyped key is an
  error, as the old positional scan was, and JSON null is accepted only for
  `updated_at`. An SQL-level failure now reports "read active work summary"
  instead of naming one of the five reads. Decode errors still name the
  section.
- **Stage-counts cache retired.** The 2s stage-counts cache (#4446) saved no
  work once the counts arrived in the same statement as everything else, and
  on a hit it replaced fresh counts with counts up to 2s old. It is removed,
  along with `eshu_dp_status_stage_counts_cache_total`. Every snapshot serves
  the counts its own statement returned.
- **Selective probes keep the per-row filter.** The materialized scope-state
  CTE builds one row per ingestion scope before any match. The drain EXISTS
  (`activeReducerGraphWorkQuery`, polled by the code-call quiescence loop) and
  the producer write-backpressure count (`reducerGraphWriteTimeoutDepthQuery`)
  only need a few rows, so they use `activeFactWorkItemsPerRowCTE`, the
  pre-change per-row form with the same row semantics. Both statements are
  byte-identical to main.

## Performance Evidence

Performance Evidence: live `EXPLAIN ANALYZE` before (`af4971189` SQL) and
after (this branch), interleaved on the same saturated Postgres 18.3 instance,
with identical result rows in one snapshot; details below.

All three measurement sets below compare the `af4971189` SQL ("before")
with this branch ("after"), interleaved and on the same Postgres config.

### Live instance, Postgres at 8 CPU

Per-request work for the five `activeFactWorkItemsCTE` consumers. Timings are
plain execution with `\timing` and output discarded; buffers come from
`EXPLAIN (ANALYZE, BUFFERS)`. Medians of 3 interleaved rounds:

| Query | Before ms | After ms | Before shared buffers | After shared buffers |
| --- | --- | --- | --- | --- |
| stage counts | 224 | 33 | 90.5k | 4.0k |
| domain backlog | 451 | 97 | 94.1k | 33.2k |
| queue snapshot | 225 | 41 | 97.0k | 10.5k |
| blockages | 86 | 38 | 16.6k | 7.8k |
| latest failure | 26 | 19 | 6.0k | 3.2k |

In-DB time for a full status snapshot. All 29 statements were plain-timed in
the pod, 3 interleaved rounds, and the per-statement medians summed:

- before: 1164ms (per-round totals 1.15–1.36s)
- after: 334ms (per-round totals 0.33–0.61s)
- about 25ms from `terraformStateRecentWarningsQuery` is included in neither
  total; that statement did not change

Those figures predate the dedupe below, which cuts round trips per
snapshot from 29 to 25.

### Idle local Postgres 18 fixture

The fixture is shaped like the live instance: on the order of a thousand
scopes and generations, tens of thousands of work items, and hundreds of
thousands of shared-projection intents, nearly all of them pending.

| Measure | Before | After |
| --- | --- | --- |
| Five CTE consumers | about 545ms, 219k shared buffers | about 42ms, 17.6k shared buffers |
| Domain backlog alone | 422–599ms | 24–38ms |

`ReadStatusSnapshotFiltered` ran from both trees against the fixture with a
fixed `asOf`, and the JSON output was diffed. The only difference is the
intentional one: a lease-only domain's `Outstanding` goes from 1 to 0.

### Output parity and correctness proofs

Output parity at 2 CPU: each changed statement ran old and new SQL inside one
`REPEATABLE READ READ ONLY` transaction on the live instance. The result rows
were identical.

`TestStatusActiveFactWorkItemsCTEUsesGenerationIndex` still passes on the
local #4446 fixture (Postgres 18; thousands of scopes with tens of generations
each; tens of thousands of work items). It finds no seq scan on `scope_generations`, and the stage-counts plan
runs in 23ms.

`TestStatusActiveWorkQueriesPreserveSemantics` seeds every stale-generation
branch and every shared-projection backlog branch. It compares the results
against expected rows derived by hand. It passes on both the old and the new
SQL, except for the lease-only phantom count, which the old SQL reports as 1
(see Change). It fails for each of these seeded mutations:

- flipping the `generation_id` tie-break
- dropping the scope check in the lateral lookup
- widening the hidden-status set

`TestActiveWorkSummaryMatchesStandaloneReads` uses the same fixture plus more
fenced conflict keys than the blockage limit (with age ties) and several
failure candidates (with an `updated_at` tie). It requires the one summary
statement to decode `reflect.DeepEqual` to the five pre-change statements.
Those statements are kept byte-identical in a test oracle with their original
scanners. It fails for each of these mutations:

- blockage limit 11
- blockage limit 0
- reversed blockage order
- reversed failure order
- reversed backlog order
- a wrong age column

`TestActiveFactWorkItemsFormsSelectTheSameRows` keeps the materialized and
per-row active-work forms selecting the same rows. It fails when the per-row
tie-break is flipped.

These three live tests run in the reducer contention CI gate against
Postgres.

### Other consumers of the active-work filter

On the live instance at 8 CPU (3 interleaved rounds, `EXPLAIN (ANALYZE,
BUFFERS)`), the four observer depth/age gauges kept the materialized form:

| Gauge | Before buffers | After buffers | Timing (ms) |
| --- | --- | --- | --- |
| queue depth | 6.7–7.6k | 4.0–4.9k | noisy both sides, 14–49 |
| queue oldest age | 6.7–7.6k | 4.0–5.0k | 14–43 |
| source queue depth | 9.3–10.3k | 6.7–7.6k | 20–35 |
| source queue oldest | 9.4–10.3k | 6.7–7.7k | 21–63 |

On a 5,000-scope Postgres 18 fixture the same four gauges were neutral to
slightly faster, at about half the buffers.

The two selective probes regressed under the materialized form, which is
why they use the per-row form. On the 5,000-scope fixture:

| Probe | Case | Before | Materialized form (rejected) |
| --- | --- | --- | --- |
| drain EXISTS | one qualifying row | 0.09ms, 12 buffers | 5.0ms, 15k buffers |
| drain EXISTS | no qualifying row | 0.05ms | 0.1ms |
| drain EXISTS | only hidden stale rows | 3.9ms | 6.9ms |
| drain EXISTS | 2,000 qualifying rows | 4.1ms | 6.2ms |
| backpressure count | 50 qualifying rows | 0.6ms, 442 buffers | 4.8ms, 10.2k buffers |

With the per-row form both statements are byte-identical to main, so they
match the "before" column.

### Earlier run at 2 CPU

Before the resize, the same comparison ran on Postgres pinned at 2 CPU.

- The five consumers took 16.9–18.1s before and 4.1–4.6s after.
- That inflation came from CPU saturation, so the 8-CPU numbers above are the
  ones to use.

### Dedupe: one active-work statement

The summary statement was compared with the five standalone statements it
replaces, run interleaved on the live instance at 8 CPU with plain timing:

| Measure | Five separate statements | One summary statement |
| --- | --- | --- |
| Time (5 rounds) | 223–359ms | 147–160ms |
| Shared buffers | 61.8k | 46.0k |
| Time on the idle fixture (3 rounds) | 60–66ms | 51–56ms |

The materialized active set held about 10MB in memory before the column
subset was applied.

Parity, live, inside one `REPEATABLE READ` snapshot: every section matched the
standalone statement's rows, compared as JSON (stage 14 rows, backlog 13,
queue 1, blockage 0, failure 1).

Parity, fixture: `TestActiveWorkSummaryMatchesStandaloneReads` requires the
decoded summary to `reflect.DeepEqual` the pre-change statements, which are
kept byte-identical in a test oracle and still run through their original
scanners. It fails when any of these seeded mutations is applied:

- reversed backlog ordinal
- blockage limit set to 0
- blockage age read from the wrong column

The #4446 plan guard now also checks the summary statement, which must not
seq-scan `scope_generations`.

### DB time per request through the local harness

Two local APIs ran against the live instance, interleaved over 6 rounds. The
base binary was built from `af4971189`; the patched one from this branch.
`lsof` confirmed which binary served each side.

A temporary, uncommitted wrapper recorded, for each status snapshot:

- the number of queries
- the summed query time, measured until each query's rows closed
- a `SELECT 1` ping, used to estimate port-forward RTT

Estimated DB time is the summed query time minus queries × ping.

| Route | Queries per snapshot (base → patched) | Est. DB ms p50 (base → patched) | Est. DB ms p95 (base → patched) |
| --- | --- | --- | --- |
| `/collectors` | 29 → 25 | 1181 → 404 | 3536 → 765 |
| `/status/collectors` | 29 → 25 | 1269 → 271 | 3327 → 737 |
| `/collector-readiness` | 29 → 25 | 1122 → 282 | 2873 → 814 |
| `/index-status` | 25 → 21 | 929 → 166 | 3558 → 857 |
| `/ingesters` | 25 → 21 | 852 → 179 | 2794 → 474 |
| `/status/freshness-causality` | 29 → 25 | 1005 → 282 | 2450 → 880 |
| `/status/pipeline` | 29 → 25 | 1060 → 298 | 2700 → 883 |
| `/status/operator-control-plane` | 29 → 25 | 1065 → 322 | 1949 → 533 |

`/index-status` and `/ingesters` read a filtered snapshot, so they issue 4
fewer queries. When the base's stage-counts cache hit, it issued 28.

Response bodies, with volatile fields excluded (keys ending `_at`, `as_of`,
ages, `_seconds`, `version`, durations, timestamps, and the `oldest=` age
text inside `flow_summaries[].backlog`):

- identical in 6/6 rounds on `/collectors`, `/status/collectors`,
  `/collector-readiness`, and `/status/freshness-causality`
- elsewhere, the only differences are live queue counters moving by about 1,
  and the operator control plane's latest failure changing between the two
  requests of a round, both caused by the busy reducer

`/evidence/bundle` returns 404 on the local API, so the harness does not
cover it.

### Pod-level wall-clock A/B happens after merge

The harness pays about 30ms of port-forward RTT per query, so its wall clock
is not the deployed latency. The deployed-pod comparison runs after merge,
once the deployed image carries this change. It is measured against the
pre-change 8-CPU baseline sweep, where these routes took 0.74–0.98s on the
pod.

### Load amplification

Every collector pod and the ingester run the same status snapshot on their
hosted status and metrics surfaces. Each of those runs now costs the reduced
amount and makes 4 fewer round trips. Their polling cadence is unchanged.

### What remains

- The count of pending intents behind the domain backlog, about 97ms at
  8 CPU, is proportional to the real backlog.
- The live instance's p95 DB time is still 0.5–0.9s under the reducer's
  current load.

## Story target support

`/repositories/{id}/story` took 11.8–16.9s at 8 CPU. About 12s of that was
the `target_support` stage, which runs two statements:

- `buildServiceStoryTargetSupportSQL`
- `buildServiceStoryTargetSupportSourceOnlySQL`, which runs when the first
  returns no facts

Both joined `fact_records` to the active scope/generation pairs and filtered
`fact_kind = ANY(<work_item.* and incident_routing.* kinds>)`.

### Diagnosis

On the live instance, Postgres estimated 1 row for the (scope, generation)
join. There were several hundred active pairs.

It also estimated 1 fact per pair, against thousands of real rows. As a result
it picked `fact_records_scope_generation_keyset_idx`, whose index condition
covers only `(scope_id, generation_id)`. The support kinds were then filtered
in the heap, which touched every fact of every active generation:

- 6.2–6.6s per statement
- millions of shared buffers per statement

The planner never chose `fact_records_scope_generation_idx`
`(scope_id, generation_id, fact_kind, observed_at DESC)`, which could have
used the kind as an index condition.

### Rejected hypothesis: extended statistics

`CREATE STATISTICS (ndistinct, dependencies) ON scope_id, generation_id` was
tested on a Postgres 18 fixture with the same shape: a few million facts,
several hundred active generations with thousands of facts each. It
reproduced the same plan.

- Restriction estimates improved: `scope_id='s5' AND generation_id='ga5'`
  went from 1 row to thousands, matching the fixture.
- The parameterized join scan still estimated `rows=1`. It stayed on the
  keyset index and touched the same ~98k buffers, because extended statistics
  do not apply to join clauses.

No migration is needed.

### Fix

Each (active scope/generation, fact kind) pair is probed through a LATERAL
subquery:

```sql
CROSS JOIN (SELECT DISTINCT unnest($1::text[]) AS fact_kind) AS kind
CROSS JOIN LATERAL (
  SELECT ...
  WHERE scope_id = ...
    AND generation_id = ...
    AND fact_kind = kind.fact_kind
    ...
  OFFSET 0
)
```

- `fact_records_scope_generation_idx` now serves each probe, with the kind in
  its index condition.
- `OFFSET 0` is load-bearing. Without it the planner flattens the LATERAL
  back into the join, which is how an earlier attempt without it failed.
- `DISTINCT` keeps a repeated kind from double-counting, matching
  `fact_kind = ANY($1)`.

Performance Evidence (target support):

| Statement | Before | After | Buffers before | Buffers after |
| --- | --- | --- | --- | --- |
| Source-only, live (3 interleaved rounds) | 6.9–17.3s | 0.10–0.37s | millions | tens of thousands (about 100× fewer) |
| Target, live (3 interleaved rounds) | 6.1–12.9s | 0.13–0.34s | millions | tens of thousands (about 100× fewer) |
| Idle fixture | 402ms | 21ms | 98.5k | 36k |

The wide "before" range on live reflects the reducer's changing load.

### Parity

- **Fixture.** It seeds refs to the target, source-only facts, a tombstone,
  a superseded generation, a pending generation, an unrelated kind, and a tie
  on `observed_at`. Old and new results were identical:
  - source-only: `40|30|10`
  - target: 11 rows, including the tie order
- **Live.** Old and new SQL were identical inside one snapshot. Live
  incident-routing facts carry no refs, so both results are empty there.

### Local harness A/B

Story, 5 interleaved rounds, local main build vs patched build:

| Repository | Before p50 | After p50 |
| --- | --- | --- |
| Repository 1 | 13.7s | 1.5s |
| Repository 2 | 14.8s | 2.9s |

Response bodies were identical in 5/5 rounds for both repositories.

`/context` did not change. It does not run the target-support read; its
bottleneck is being diagnosed separately.

### Pre-existing accuracy gap (not changed here)

The source-only rollup's `NOT (jsonb_typeof(payload->'x') = 'array' AND ...)`
evaluates to NULL when one of the three ref keys is absent. So a fact missing
any of those keys is never counted as source-only. This change preserves that
behavior for parity, and the story semantics test pins it. The gap is
tracked in #6807.

## Observability Evidence

Observability Evidence: status snapshot reads on the API now emit
`postgres.query` spans and `store="status_snapshot"` duration samples. The
story target-support read keeps its existing span, and this change adds no
signal to it.

The API used to read the status snapshot through a raw `SQLQueryer`, so a 20s
status route produced no span and no histogram sample.

`newStatusQueryer` in `go/cmd/api` now wraps the reader in `InstrumentedDB`
with `store="status_snapshot"` whenever instruments are wired. Every snapshot
read then emits:

- a `postgres.query` span;
- an `eshu_dp_postgres_query_duration_seconds` sample with
  `operation="read"` and `store="status_snapshot"`.

`TestNewStatusQueryerInstrumentsStatusReads` covers this wiring.

Each status snapshot read is now attributable:

- **Read labels.** `StatusStore.read` hands every reader a queryer that
  labels its context with a bounded read name (`scope_counts`,
  `active_work_summary`, `coordinator`, and so on).
- **Duration metric.** Each read records one
  `eshu_dp_status_snapshot_read_duration_seconds{read, outcome}` sample when
  the reader returns; any query, scan, or decode failure is `outcome=error`.
  Every process whose status store carries instruments emits it.
- **Span attribute.** `InstrumentedDB` stamps the read name on the
  `postgres.query` span as `db.query.summary`.
- **Tests.** `TestReadStatusSnapshotLabelsEveryRead` and
  `TestInstrumentedDBStampsQuerySummary` cover this, as does
  `TestReadStatusSnapshotServesFreshStageCounts` for the retired cache. All
  three fail on the pre-change commit.

Two limits:

- `eshu_dp_postgres_query_duration_seconds` is recorded when `QueryContext`
  returns, not when the rows close, so it is not a full round-trip time.
- The MCP server's status reads carry the per-read metric but no
  `postgres.query` span; its status queryer is not wrapped in
  `InstrumentedDB`.
