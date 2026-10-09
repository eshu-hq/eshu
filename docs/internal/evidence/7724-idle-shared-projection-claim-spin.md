# #7724: idle shared-projection runners burn ~5 writer cores

Fixes the idle claim spin (1A + 1B + 2A per the binding arbiter verdict
quoted below): batched acceptance/readiness prefetches, a readiness-only
cross-round cache, and per-partition backoff with the global
`BlockedReadiness` pin removed.

Source: branch `fix/7724-idle-claim-spin` on base `01ceb1dd05`
(`worktrees/fix-7724-idle-claim-spin`, uncommitted at measurement time;
rebased through `195337b97d` and `4a509716f6` since).
Probe container `eshu-pg18-ws1b11bd5`, DB `shim7724`, same seed and
partition for every run below.

## Binding arbiter verdict (arbiter-eshu, quoted)

> - **1A: APPROVE (required).** True batched prefetches, one UNNEST/ANY JOIN
>   query per round per partition for acceptance AND readiness.
> - **1B: APPROVE WITH MODIFICATION — readiness-only cache.** Cross-round
>   answer cache within one `SelectPartitionBatch` call is safe for
>   readiness ... **acceptance answers MUST NOT be cached across rounds**
>   ... **The #7121 `drainSupersededBlockedRows` readiness re-read MUST
>   bypass the cache** with a fresh batch read ... Non-negotiable.
> - **1C: REJECT as specified.** ... Widening stays exactly as-is.
> - **2A REQUIRED. 2B REJECTED as the complete fix.** ... remove the global
>   `BlockedReadiness` pin ... Keep the global `consecutiveEmpty` back-off
>   ... Per-(domain,partition) state ... grace **K=2** ... `T_max` default
>   30s, configurable, hard cap 5min ... Fixed 88-key map, mutex-guarded ...
> - **3C SATISFIES; no wake required. 3A and 3B REJECTED.** ... no
>   LISTEN/NOTIFY ...
>
> Per cycle: partitions visited vs skipped (by reason: backed-off vs
> lease-held), current global back-off interval, count of partitions at
> T_max. Per partition visit: idle claim attempts ..., selection widen
> rounds, prefetch keys requested / queries issued / rows returned / cache
> hits, per-prefetch duration. Back-off state ... exported to at least a
> debug/status surface. Existing `BlockedReadiness` /
> `blocked_intent_wait_seconds` stall signals retained, distinct from
> back-off state.

Full text: `/tmp/7724-arbiter-verdict.txt` (session-local; not committed).

## What changed

- **1A** (`go/internal/storage/postgres/`): `LookupBatch` on
  `SharedProjectionAcceptanceStore` and `GraphProjectionPhaseStateStore`.
  One UNNEST/JOIN query per chunk of ≤1000 distinct keys; keys normalized
  (TrimSpace) before the query and compared on normalized keys; batch
  error fails the selection; no prefetch signature changes; per-key
  found/not-found semantics preserved (differential-tested against the
  per-key `Lookup` the old loop called).
- **1B** (`go/internal/reducer/intents/shared/worker/`):
  `roundReadinessCache` serves readiness answers across widen rounds
  within one `SelectPartitionBatch` call and queries only the per-round
  delta. Acceptance is re-queried fresh every round, and
  `drainSupersededBlockedRows` receives the raw prefetch (cache bypass).
  Widening is byte-identical (1600→3200→6400→10000, no early stop).
- **2A** (`worker/runner.go`, `worker/backoff.go`, `worker/config.go`):
  the global `BlockedReadiness` pin is removed; the global
  `consecutiveEmpty` back-off engages on `ProcessedIntents == 0`
  regardless of blocked counts. Per-(domain, partition) cells hold a
  consecutive-unproductive counter + `nextEligibleAt`: K=2 grace visits,
  then `min(poll*2^(n-K), T_max)`; `T_max` defaults to 30s via
  `ESHU_SHARED_PROJECTION_PARTITION_BACKOFF_MAX`, hard-capped at 5m.
  Productive = `ProcessedIntents > 0` (any completion kind resets);
  lease-not-acquired and error visits hold the counter. Fixed 88-key map,
  mutex-guarded, shared by the sequential and concurrent cycle paths.
- Telemetry: 10 new instruments (visits by outcome, per-partition and
  global backoff gauges, at-max gauge, prefetch keys/queries/rows/hits,
  prefetch durations, selection rounds), a per-cycle backoff summary log
  line, and `Runner.BackoffState()` as the debug API. Stall signals
  (`blocked_count`, `blocked_intent_wait_seconds`) are untouched and stay
  distinct from backoff.

## Before/after (stuck load)

Seed: 20K pending `sql_relationships` rows / 500 distinct keys + 500
acceptance rows + zero phase rows; one `SelectPartitionBatch` visit,
partition 3/8, batch limit 100 (throwaway probe `go/tmp7724probe/select`,
deleted before handoff; same metric boundaries every run).

| Stage | Queries/visit | Wall | Blocked |
|---|---|---|---|
| Baseline (N+1 loop) | 506 | 128ms (single run; task baseline: 90ms) | 2500 |
| After 1A (batched prefetches) | 10 | 59ms | 2500 |
| After 1A+1B (+ readiness cache) | 9 | 58ms (mean of 5: 55–62ms) | 2500 |

Breakdown after 1A+1B: 4 candidate queries + 4 fresh acceptance batches +
1 readiness batch (rounds 2–4 fully cache-hit). Per-query timing was not
split; the remaining wall is dominated by the 4 widen-round candidate
scans over the 20K-row backlog (next measured cost, out of scope: 1C
early-stop was rejected). Idle claim rate at saturation drops from 88
claims/cycle every 500ms to at most 88 claims per 30s (T_max), plus the
global backoff (5s max) pacing empty cycles — derived from the proven
schedule, not measured on a live reducer (see NOT_CHECKED below).

Performance Evidence: per-visit query count 506 → 9 and wall 128ms →
58ms on the issue's stuck load (probe runs above, same seed/container);
EXPLAIN plans below show PK nested-loop probes with no sequential scan;
the #7724 2A soak test proves ready-behind-blocked-head pickup within
T_max with a fake clock (no sleeps).

Observability Evidence: the 10 new `eshu_dp_shared_projection_*`
instruments plus the `shared projection cycle backoff summary` log line
and `Runner.BackoffState()` carry the verdict's telemetry minimums;
emission is pinned by `cycle_telemetry_test.go` (values, labels, and the
forbidden-label discipline).

## EXPLAIN proof (500 keys over 100K rows, `ANALYZE`d)

Committed live test
`go/internal/storage/postgres/prefetch_batch_plan_live_test.go`
(untagged, DSN skip-guard, enrolled postgres_ci in live-postgres-readiness; representative shape: 10 scopes × 10K units). Plans
from `ESHU_POSTGRES_TEST_DSN` against `eshu-pg18-ws1b11bd5`:

Acceptance batch:

```text
Nested Loop  (cost=0.42..3385.01 rows=1 width=32)
  ->  Function Scan on k  (cost=0.01..5.01 rows=500 width=96)
  ->  Index Scan using shared_projection_acceptance_pkey on shared_projection_acceptance a  (cost=0.42..6.76 rows=1 width=32)
        Index Cond: ((scope_id = k.scope_id) AND (acceptance_unit_id = k.acceptance_unit_id) AND (source_run_id = k.source_run_id))
```

Readiness batch:

```text
Nested Loop  (cost=0.43..3748.76 rows=1 width=50)
  ->  Function Scan on k  (cost=0.01..5.01 rows=500 width=160)
  ->  Index Only Scan using graph_projection_phase_state_pkey on graph_projection_phase_state a  (cost=0.42..7.49 rows=1 width=53)
        Index Cond: ((scope_id = k.scope_id) AND (acceptance_unit_id = k.acceptance_unit_id) AND (source_run_id = k.source_run_id) AND (generation_id = k.generation_id) AND (keyspace = k.keyspace) AND (phase = 'canonical_nodes_committed'::text))
```

Both plans: nested loop over PK probes, no `Seq Scan`. (An earlier
unrepresentative seed with one row per scope let the planner use the
narrower `shared_projection_acceptance_scope_idx`; production scopes
hold many units, so the committed test seeds 10K rows/scope and asserts
the `_pkey` probe by name.)

## Correctness proof

- Differential old-vs-new prefetch tests over found / not-found /
  wrong-phase / duplicate / invalid / never-queried fixtures
  (`accepted_generation_batch_test.go`,
  `projection_phase_batch_test.go`): the batch closure agrees with
  the per-key `Lookup` on every probe.
- Whitespace-key regression tests: padded keys resolve the trimmed row;
  the store receives only normalized keys.
- Query-count tests: 500 keys → 1 query; 2500 keys → exactly 3 queries
  of ≤1000 keys (both prefetches); empty input → 0 queries.
- Cross-round cache test: each distinct readiness key queried exactly
  once across 3 widen rounds. Acceptance-freshness test: an acceptance
  committed between rounds is seen by the same selection (round-two
  answer wins). Drain-bypass test: a publish between the first readiness
  read and the superseded lookup projects instead of draining.
- Backoff schedule tests: K=2 grace, doubling, T_max cap, reset on
  productive, hold on lease-miss/error, per-partition independence,
  8-worker race-detector concurrency proof.
- Starvation soak: all partitions pinned at T_max behind a blocked head
  still pick up an injected ready row within T_max at the cycle level
  (the soak drives runOneCycle directly; end-to-end pickup adds one
  global poll interval, at most 5s); the blocked head stays pending.
- `TestSharedProjectionRunnerBacksOffWhileReadinessBlocked` (updated from
  the pin-era `...UsesBasePollIntervalWhileReadinessBlocked`): blocked
  rows no longer pin the global interval; nothing completes while
  blocked.

NOT_CHECKED: live-reducer claims-per-second and writer-CPU before/after
(no live reducer harness in this session; the query-count and schedule
proofs above are the local evidence, and CI owns the end-to-end gates).
