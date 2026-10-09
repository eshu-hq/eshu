# #7471: Neo4j marked-write bound and oldest-marker signal

## Problem

`neo4jCanonicalWriteTimeout` returned 0 unless `ESHU_CANONICAL_WRITE_TIMEOUT`
was set, so a Neo4j deployment that never configured the budget ran canonical
graph writes with no transaction timeout: a hung write with a live heartbeat
froze the scope. Separately, no consumer read `projection_write_started_at`,
so nothing signaled how long a marked write had been outstanding.

## Change

- All four graph writers (projector, ingester, reducer, bootstrap-index) share
  byte-identical `neo4j_write_timeout.go` copies. An unset or invalid
  `ESHU_CANONICAL_WRITE_TIMEOUT` now falls back to a 300s default; only an
  explicit non-positive duration returns zero (the deliberate unbounded
  opt-out). The bound reuses the existing `WithTxTimeout` +
  `TimeoutExecutor` machinery, so a write that outlives it ends as a retryable
  `graph_write_timeout` that requeues, exactly like NornicDB.
- The `graph.write_timeout.unbounded` startup WARN is retained and now fires
  only when the effective timeout is zero, i.e. on explicit opt-out.
- New `eshu_dp_projector_marked_write_oldest_age_seconds` gauge: the age of
  the oldest set marker on a non-retired generation with open projector work,
  served by the reducer from its Postgres gauge snapshot. The marker is
  monotonic and never cleared in production, so the query excludes retired
  (`superseded`, `completed`) generations and requires an open projector work
  row, or stale markers would alarm forever.

## Benchmark Evidence:

Full-write timing on a local `neo4j:2026-community` container (single-node,
empty store, canonical writer path; `TestScratch7471FullWriteTiming`, since
removed), marker rows per write:

| markers | wall time |
|--------:|----------:|
| 1,000   | 4.2s      |
| 5,000   | 4.4s      |
| 10,000  | 5.9s      |
| 25,000  | 13.9s     |
| 50,000  | 19.6s     |
| 25,000 (ops-qa shape repro) | 14.4s |

The 300s default is ~15x over the largest locally measured full write
(19.6s at 50k markers) and matches the value ops-qa already sets explicitly,
so no deployment that sets the variable changes behavior. The arbiter (Muse
Spark) confirmed the 300s default on these numbers.

NOT_CHECKED: the issue asks for the largest full write in the reference
corpus; this machine cannot reach it, so the reference-corpus measurement was
not taken. Residual risk (a legitimate write over 300s aborting and
requeuing) is mitigated by configurability: raise
`ESHU_CANONICAL_WRITE_TIMEOUT`, or set an explicit non-positive duration to
opt out (keeps the startup WARN).

Gauge query cost (`EXPLAIN (ANALYZE, BUFFERS)`, local `postgres:18`):

- 90 generations / 60 work rows: 0.226 ms, 122 shared buffers.
- 2,003 generations / 200,003 work rows (2,000 retired with stale markers, 3
  live marked): 21.3 ms, 4,188 shared buffers per 60s observable-gauge
  collection (nested loop over a parallel scan of the live-status rows). No
  new index: the `EXISTS` probe uses the
  `fact_work_items_scope_generation_idx` prefix, and the cost is in line with
  the neighboring claim-fence gauge (8.6 ms / 1,393 buffers at 800k rows).

## No-Regression Evidence:

- `TestLiveNeo4jIngesterCanonicalWriteTimeoutRequeues` and
  `TestLiveNeo4jCanonicalWriteTimeoutAbortsBlockedWrite` (lock wait, long
  statement, operator kill) pass against local `neo4j:2026-community` with
  the change: explicit configured timeouts behave exactly as before, and the
  explicit-zero operator-kill path stays unwrapped.
- The per-package `BoundsNeo4jWritesLikeNornicDB` parity tests still pin
  retryable `graph_write_timeout` on both backends for configured timeouts.
- Heartbeat/lease behavior is unchanged: queue claim TTLs stay 60s with
  heartbeat renewal, the bound sits 5x above that and 2x below the 10m
  partition-lease TTLs, and writes under 300s see identical lease handling
  before and after.

## Observability Evidence:

- New gauge `eshu_dp_projector_marked_write_oldest_age_seconds` (no labels),
  registered when the queue observer implements
  `telemetry.ProjectorMarkedWriteObserver`; in the reducer it is served from
  the `reducer_projector_marked_write` snapshot (millis key, converted back
  to seconds), reports nothing before the first refresh, and never publishes
  a false zero on refresh failure. Documented in
  `docs/public/reference/telemetry/metrics.md`.
- Retained WARN `graph.write_timeout.unbounded` (event, `graph_backend`,
  `env_var` attrs unchanged; message now states the explicit opt-out).
- No new span or counter. A timed-out write surfaces through the existing
  `failure_class=graph_write_timeout` retry path.

## Rollout

Behavior change for Neo4j deployments that never set
`ESHU_CANONICAL_WRITE_TIMEOUT`: writes are now bounded at 300s instead of
unbounded, and the startup WARN no longer fires for them (it fires only on
explicit opt-out). Deployments that set the variable (including ops-qa at
300s) see no change. If a deployment has legitimate writes over 300s, raise
the variable or opt out explicitly with `0s` before upgrading.

## Arbiter verdict (Muse Spark)

Finite Neo4j default reusing `WithTxTimeout` + `TimeoutExecutor`; 300s
approved on the local measurements with the reference corpus marked
NOT_CHECKED; oldest-marker-age gauge via the queue-observer pattern with
contract/docs; 2x lease arithmetic stated; warn-log retention and this
rollout note required.
