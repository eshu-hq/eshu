# #7547 reachability loader: completeness gate and candidate restructure

## Scope

`LoadPendingCodeReachabilityInputs` (`go/internal/storage/postgres/code/reachability`)
picks the next batch of repository runs for the reducer's reachability
projection. The runner polls on `ESHU_SHARED_PROJECTION_POLL_INTERVAL`
(default 500 ms; its own 5 s default applies only when the interval is zero or
negative). It re-polls without waiting while a batch returns inputs, so the
statement runs nearly back to back. Its `LIMIT` is
`ESHU_SHARED_PROJECTION_BATCH_LIMIT` (default 100). This change does two
things to its candidate statement and nothing else in the loader:

1. **Completeness gate.** A run is scheduled only when its code-edge set is
   provably complete. That is the same check the dead-code query's `run_gate`
   applies (`deadCodeIncomingBoundQuery` in `go/internal/query`): the
   generation is not a delta, both `code_call_materialization` and
   `inheritance_materialization` reducer work items succeeded and none is in
   another status, and no `code_calls` or `inheritance_edges` intent of the
   generation for the repository is still pending. Before, the loader built a
   snapshot from whatever intents had completed, so a delta generation (only
   changed files) or a run whose materialization was still running produced a
   snapshot of a partial edge set.
2. **Restructure.** The statement now narrows acceptance rows to complete
   active runs first (`ready` CTE), then reads one `max(completed_at)` per run
   with a `LATERAL` subquery and joins the watermark once per run. The old
   statement joined every completed intent row, probed the watermark once per
   intent row, and grouped afterwards.

The predicate text lives once, in `reachabilitystore.CompleteRunGateSQL`.
This change adds no DDL, no epoch change, and no change to the worker count,
batch size, or poll interval. It is based on `main` after the epoch 3 to 4
bump (#7576, `docs/internal/evidence/7547-reachability-epoch-4-bump.md`), so
`$2` is 4 in production.

What the gate means for a delta generation: an active delta generation
activated after deploy gets no snapshot. Reachability for that repository
waits for the next full generation, and dead-code reads for its entities use
the legacy one-hop read. The snapshot reader trusts any active-generation
rows without a gate, so a delta generation the pre-#7547 loader already
projected before deploy keeps its partial snapshot until the next generation
replaces it. That effect is conservative: snapshot rows only add reachability
evidence, and the legacy read still covers every entity they do not answer. The git collector emits both materialization follow-up facts on
every full generation, unconditionally (`fact_builder.go`, after the delta
early return), so a full run is never starved for lack of a work item.

## Accuracy proof

Live tests run against a throwaway `postgres:18-alpine` (PostgreSQL 18.6) with
the full bootstrap schema (`ESHU_POSTGRES_DSN`), in
`store_route_liveness_live_test.go` with fixtures in
`loader_gate_fixture_test.go`:

- `TestCodeReachabilityLoaderGateSkipsIncompleteRuns`: seven incomplete runs
  (delta generation; code-call work item failed; succeeded plus a second
  pending code-call item; code-call item missing; inheritance item missing;
  pending `code_calls` intent; pending `inheritance_edges` intent) and one
  complete run. The test first checks that the old statement schedules all
  eight runs, so the fixtures are not vacuous.
- `TestCodeReachabilityLoaderGateMatchesLegacyOnCompleteRuns`: a row-set
  differential against the old statement text, kept test-only as
  `legacyListPendingCodeReachabilityInputsSQL`. Complete runs only: missing
  watermark, older watermark, newer watermark at a stale epoch, newer
  watermark at the current epoch, a superseded generation with a newer
  intent, an intent of another run in the active generation, and a
  `completed_at` tie. Epochs 0, current, and current+1, with limits 100000
  and 3. Every `(scope_id, repository_id, source_run_id, generation_id,
  completed_at)` row must match, in order.
- `TestCodeReachabilityLoaderGateEachPredicateIsLoadBearing`: removes one gate
  predicate at a time from the production statement. Each mutation must
  schedule exactly the incomplete runs that only that predicate holds back.

RED against the old statement (first test, before the change): every one of
the seven incomplete runs was scheduled (`incomplete run scheduled: {ScopeID:scope-delta-...}`
and six more), then `want only the complete run ..., got [complete delta calls-failed calls-retrying calls-missing inheritance-missing calls-pending inheritance-pending]`.
GREEN after: all three tests pass.

Why the row set is unchanged for complete runs: `shared_projection_acceptance`
is unique on `(scope_id, acceptance_unit_id, source_run_id)` and
`code_reachability_repository_watermarks` on `(scope_id, generation_id,
repository_id)`. The old `GROUP BY` over the four acceptance columns kept one
group per acceptance row, and `max()` over a unique watermark returned that
one row. The per-run form computes the same values.

Drift guard: `TestDeadCodeRunGateMatchesReachabilityLoaderGate` in
`go/internal/query` cuts the `bool_and` body from the statement
`deadCodeIncomingBoundQuery` builds, maps its aliases onto the loader's
(`active.is_delta` to `generation.is_delta`, `active.` to `acceptance.`, `$1`
to `acceptance.acceptance_unit_id`), and requires it to equal
`CompleteRunGateSQL` after whitespace normalization. Seeded mutations on each
side (the loader dropping the pending `repository_id` predicate; the dead-code
query changing `<> 'succeeded'` to `NOT IN ('succeeded')`) each turned it RED.

The upgrade-backfill live fixtures in
`code_reachability_upgrade_backfill_live_test.go` now seed the two succeeded
work items a real full run carries. Without them all three upgrade-backfill
tests went RED under the gate, which was expected.

## Cost

Performance Evidence: the QA replica figures below were measured by the
#7547 work: read-only, one data copy, PostgreSQL 18.3, label verified. `LIMIT 10`,
epoch 3, three runs (cold, warm, warm). The old statement took 8,385 / 4,698 /
4,667 ms. It joined 497,431 intent rows, probed the watermark once per intent
row (2.08M buffers), and did a 112 MB external sort before grouping. The
restructured statement with the gate took 922 / 725 / 733 ms. Without the gate
block it took 668 / 461 / 459 ms, so the gate costs about 0.27 s warm. Its plan
did no sequential scan on `shared_projection_intents` or `fact_work_items`. It
used `fact_work_items_scope_generation_idx`,
`shared_projection_intents_generation_pending_idx` (migration 108), and
`shared_projection_intents_acceptance_lookup_idx`. Two variants were rejected:
gate predicates inside the old join ran past 30 s and were cancelled twice,
and a materialized gate CTE feeding the old join and aggregate took 5.0 to
5.6 s. The old and new candidate sets had identical sha256 on QA (0 rows at
epoch 3, 795 at epoch 4). The gate removed nothing on QA, because every QA
run was complete at the time. The fixture tests above prove what the gate
does on incomplete runs.

The figures above were taken on the measurement shim, which used different
aliases. The exact shipped statement was then measured on the QA replica by
the #7547 work on 2026-10-04 (label verified): PostgreSQL 18.3, read-only,
`statement_timeout` 10 s, `jit` off, a prepared statement, `LIMIT 10`.
EXPLAIN without ANALYZE came first. Its plan has no sequential scan on
`shared_projection_intents` or `fact_work_items`. It uses index scans on
`fact_work_items_scope_generation_idx`,
`shared_projection_intents_generation_pending_idx`,
`shared_projection_intents_acceptance_lookup_idx`, and the watermark primary
key. The only sequential scan is on `ingestion_scopes` (819 rows). Then
EXPLAIN (ANALYZE, BUFFERS) gave:

| Shipped statement on QA | Execution time | Notes |
| --- | ---: | --- |
| First execution (cold) | 5,266 ms | 111,016 shared reads |
| Epoch 3 | 702.7 ms | warm |
| Epoch 4 parameter | 702.2 ms | warm |

Planning took 8 to 12.6 ms. These were custom plans, with the epoch
inlined as a literal (`< 3` or `< 4`). The generic plan PostgreSQL may switch
to after the fifth execution was not measured, and the reducer's pgx
statement cache can reuse prepared statements. The plan shape is expected to
stay the same, because `$2` only filters the final candidate rows, but that
is not measured. All of these used `LIMIT 10`, while production uses the
batch limit (default 100). The cost sits in the per-run aggregate over every
ready run before the top-N sort, so the limit should matter little, but
`LIMIT 100` was not measured on QA. For comparison, the earlier
QA runs of the pre-#7547 statement took 8,385 ms cold and 4,698 and 4,667 ms
warm.

Replica replay lag read 0.06 s before and 1.7 s after the three executions
(0.6 s on a later read). That is not evidence that a read held replay:
`now() - pg_last_xact_replay_timestamp()` also grows while the primary is
idle. The loader runs on the primary in production, not on the replica.
Still, a cold read on the replica can in principle hold replay back, up to
`max_standby_streaming_delay`. The practice for replica measurements is now: a
`statement_timeout` of 10 s or less, EXPLAIN before EXPLAIN ANALYZE, and a lag
check before and after.

Fixture-scale figures come from a throwaway PostgreSQL 18.6 container
(`shared_buffers=512MB`, `work_mem=16MB`, `jit=off`, warm cache, three runs
each). The fixture has 800 repositories with one active and one superseded
generation each, 507,996 active intents (repository i has 70000/i, at most
70,000), 202,963 superseded-generation intents, 24,000 fixture work items (15 reducer
domains per generation, plus one bootstrap-seeded global row), and watermarks at epoch 3 newer than every intent.
The tables were analyzed after seeding.

| Statement | Epoch | Execution time (3 runs) | Shared buffer hits | Spill |
| --- | ---: | --- | ---: | --- |
| old | 3 | 1,075 / 1,106 / 1,070 ms | 3,158,244 | external merge sort, 33 MB |
| new | 3 | 158 / 153 / 152 ms | 123,365 | none (top-N heapsort) |
| old | 4 | 1,079 / 1,070 / 1,071 ms | 3,158,244 | external merge sort, 33 MB |
| new | 4 | 149 / 153 / 151 ms | 123,365 | none (top-N heapsort) |

Neither plan scans `shared_projection_intents` or `fact_work_items`
sequentially. The only sequential scans are on
`shared_projection_acceptance` (1,600 rows) and `ingestion_scopes` (801
rows), the same as on QA. In the new plan, the gate's four probes use
`fact_work_items_scope_generation_idx` and
`shared_projection_intents_generation_pending_idx`, 800 loops each. The
per-run `max(completed_at)` uses
`shared_projection_intents_acceptance_lookup_idx` (110,230 buffers), and the
watermark is probed 800 times instead of 507,996. At 100000 limit, the old
and new row sets had the same count and md5: 0 rows at epoch 3, and 800 rows
at epoch 4 (`5a1402575b7eeded15a95bdc9f2442dc`). The fixture is smaller than
QA in payload bytes and cache pressure, so its absolute times are lower than
QA. Only the ratio and the plan shape carry over.

Rerun after rebasing onto the epoch bump (merged SQL, epoch 4 is the
production value). This used a fresh container with the same settings and a
re-seeded fixture; the timestamps are new, so the md5 differs from the first
run. At limit 100000, old and new returned 0 rows at epoch 3, and 800 rows
with the same md5 at epoch 4 (`b1c6e5b26ecb0d287607115d9156a85b`). EXPLAIN
(ANALYZE, BUFFERS) at `LIMIT 10`, epoch 4, three warm runs: old took 1,007 /
995 / 994 ms with 3,158,238 to 3,158,244 shared hits and a 33 MB external
merge sort. New took 128 / 125 / 124 ms with 123,365 shared hits and no
spill. Neither plan scans `shared_projection_intents` or `fact_work_items`
sequentially. The bump's own `TestCodeReachabilityPendingInputsPlanAtEpochBump`
now seeds the two succeeded work items per run. Without them, the gate would
hold back all 800 runs and the epoch-4 plan would return no rows. With them,
its epoch-4 plan returns `rows=100` at `LIMIT 100`, and it passes.

## Interaction with the epoch-4 drain

The bump's drain census counts active watermarks below epoch 4, and the
drain is called complete when that count is 0. The gate changes what can
drain. A watermark stamped at epoch 3 by the pre-#7547 loader may belong to a
run that the gate now rejects: a delta generation, a work item that failed or
is retrying, or a pending intent. The loader never schedules such a run, so
its watermark stays below epoch 4 until a later full generation replaces it.
The census residual can therefore include gated-out runs. A nonzero residual
is not, on its own, a stuck drain. This split census (10 s statement timeout,
replica) tells the two apart:

```sql
SELECT count(*) AS residual,
       count(*) FILTER (WHERE NOT complete) AS gated_out
FROM (
  SELECT w.scope_id,
         coalesce(bool_or(<CompleteRunGateSQL>), false) AS complete
  FROM code_reachability_repository_watermarks w
  JOIN ingestion_scopes s
    ON s.scope_id = w.scope_id AND s.active_generation_id = w.generation_id
  JOIN scope_generations AS generation
    ON generation.generation_id = w.generation_id
  LEFT JOIN shared_projection_acceptance AS acceptance
    ON acceptance.scope_id = w.scope_id
   AND acceptance.generation_id = w.generation_id
   AND acceptance.acceptance_unit_id = w.repository_id
  WHERE w.verdict_schema_epoch < 4
  GROUP BY w.scope_id, w.generation_id, w.repository_id
) per_watermark;
```

Replace `<CompleteRunGateSQL>` with the constant's text; its aliases
`acceptance` and `generation` match this query. The drain is complete when
`residual` equals `gated_out`. A watermark with no active acceptance row
counts as gated out. On the fixture with every run complete, this returned
800 and 0. After one code-call work item was set to failed and one generation
to delta, it returned 800 and 2. It was not run on QA. Per the measurement
agent's QA census, no active run there failed the gate, so the residual and
the plain census should agree at deploy time.

Concurrency: the candidate statement is a read-only `SELECT`. It takes no row
lock, claim, lease, or queue write. `CodeReachabilityProjectionRunner.ProcessOnce`
calls `LoadPendingCodeReachabilityInputs` outside any transaction or advisory
lock. It then partitions the loaded inputs by conflict key
(`partitionInputsByConflictKey`) and writes each partition. That partitioning
and the write path are unchanged, so the concurrency model is the same as
before.

No-Observability-Change: no metric, span, log key, status field, worker, queue
domain, or runtime knob is added. The runner's existing per-cycle duration and
`InputsProcessed` result, and its completion log, still describe each poll.
A run the gate holds back is simply not loaded. That is the same signal an
operator sees today for a run whose intents have not completed yet. A
"skipped as incomplete" counter would need a second query per poll, and
nothing in the existing pattern asks for it.
