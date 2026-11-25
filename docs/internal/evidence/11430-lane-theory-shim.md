# #11430 lane-theory shim: snapshot lane predicate shows no scan shrink and +22-27% aggregate claim CPU — STOP, no implementation

Date: 2026-11-25. Lane: `drive/11430-drain-bottleneck` (leaf child).
Tree: branch `drive/11430-drain-bottleneck`; no production code changed.
Prior: the [concurrency-proof design](../design/11430-conflict-key-partitioned-parallelism.md)
(commit 9b5ad7df92) approved conflict-key-partitioned parallelism subject to
the arbiter's required proof in order, item 1 first: a theory shim of
EXPLAIN ANALYZE laned vs unlaned candidate SELECT at QA-scale backlog, with
STOP on aggregate L-lane claim CPU regression >10% or no per-lane scan
shrink (reopens the mechanism under a fresh ruling; never serialize lane
claimers on a mutex). This lane executes proof item 1 only.

## Verdict

STOP. Both stop thresholds fire on two independent runs: no per-lane scan
shrink on any of the L=4 lanes (identical 1274 shared buffers laned and
unlaned), and aggregate 4-lane claim CPU regresses +27.2% (run 1) and
+21.9% (run 2) over unlaned. No implementation follows; proof items 2-5
(concurrency tests, keyed-equals-full differential, bounded replay, lane
telemetry) do not start. The mechanism reopens only under a fresh arbiter
ruling — for example with an expression index on the lane function (a
migration with write-amplification cost, outside the approved mechanism) or
a different partition placement that the design's EvalPlanQual analysis
would need to re-clear.

## Shim setup (disposable PostgreSQL 18.7, local, removed after the runs)

- Fresh `initdb` cluster (18.7 binaries, port 55433): the shared :5432
  cluster runs an 18.6_1 binary whose `$libdir` no longer exists after the
  Homebrew 18.7 upgrade, so fresh template bootstraps fail loading
  `plpgsql.dylib` there (SQLSTATE 58P01). The shim used its own cluster to
  avoid touching the shared server; the data directory was removed after
  the second run.
- Migrated schema via the proof-template bootstrap (375 migrations,
  flavour public-without-content-search-indexes), seeded to QA backlog
  scale: 9000 `supply_chain_impact` rows (conflict_domain `scope`) + 1000
  `advisory_group_projection` rows (conflict_domain `advisory_group`), all
  `pending`, every conflict key distinct (the saturation-shim 1-row-per-key
  shape at ~10k claimable), `updated_at` spread over the past hour, small
  (~200B) payloads, then `ANALYZE fact_work_items`.
- Queries derived from shipped Go sources, never hand-copied
  (`go/internal/storage/postgres/reducer_claim_lane_theory_shim_live_test.go`):
  the WITH-prefix (readiness + stale-gate + candidate chain) is composed
  from `reducerClaimReadinessRequirementsCTE`,
  `reducerClaimStaleGateCTEs`, `reducerClaimBatchCandidateChainCTEs`; the
  laned variant appends the design's snapshot predicate
  (`SUBSTRING(md5(conflict_domain || '|' || ckey) ...) IN (...)`) to the
  single base anchor `AND ($2::text[] IS NULL OR domain = ANY($2::text[]))`
  (unique to base; derivation fails loudly if the anchor count drifts).
  A hermetic guard (`TestLaneTheoryDerivedPrefixMatchesProduction`, no DB)
  asserts the derived prefix is a byte-prefix of the shipped
  `claimReducerWorkBatchQuery`. Args mirror the vuln lane:
  `$2 = {supply_chain_impact, advisory_group_projection}`, `$5 = false`
  (no projector drain), `$6 = $7 = 0` (no semantic cap).
- Timed shape: the candidate SELECT with the rest-arm ORDER BY keys and
  `LIMIT 32` (vuln-lane `batchClaimSize` at 8 workers), via
  `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`; 1 warmup discarded, median of
  5 measured runs. Partition sanity on the unlimited id sets: union of the
  4 lanes equals the unlaned set exactly (10000 = 10000, no overlap, no
  gap) — the predicate partitions correctly; it just does not prune.

## Measurements (median of 5 EXPLAIN ANALYZE runs; 0 temp buffers everywhere)

| query | run 1 exec | run 2 exec | shared buffers (both runs) |
|---|---|---|---|
| unlaned | 60.10 ms | 66.56 ms | 1274 |
| lane 0 (`0-3`) | 19.38 ms | 21.14 ms | 1274 |
| lane 1 (`4-7`) | 18.93 ms | 19.94 ms | 1274 |
| lane 2 (`8-b`) | 19.02 ms | 19.91 ms | 1274 |
| lane 3 (`c-f`) | 19.14 ms | 20.16 ms | 1274 |
| aggregate 4-lane | 76.47 ms (ratio 1.272) | 81.14 ms (ratio 1.219) | 5096 total vs 1274 |

- No per-lane scan shrink: every lane touches exactly the unlaned buffer
  count. The md5 predicate forces a full `fact_work_items` heap re-scan per
  lane (no expression index exists on the lane function), so I/O scales
  with L while the window/sort work shrinks only modestly per lane.
- Aggregate claim CPU regression >10%: +27.2% and +21.9% at L=4. Each lane
  is faster alone (~19-21 ms vs ~60-67 ms), but four lanes redo the full
  base scan; the sum exceeds the single unlaned claim.
- Production consequence: the QA Postgres primary sits at ~96% of its CPU
  request (saturation shim). Quadrupling claim-path buffer touches
  (5096 vs 1274) for the same admission spends the binding resource the
  saturation shim convicted the design to respect.

## Decision

STOP after proof item 1, as ordered. No lane config, no claimer changes, no
telemetry, no replay. An L=2 re-run was not needed to reach this verdict:
the no-scan-shrink finding is structural (identical full base scans follow
from the unindexed md5 predicate, independent of the IN-list width), and
either threshold alone is STOP. What reopens this: a fresh arbiter ruling on
a changed mechanism — not a mutex around lane claimers (refused in
advance), not a bare `Workers`/gate increase (refused), not silent
serialization.

## Commands and observed values

- `git symbolic-ref -q HEAD` → `refs/heads/drive/11430-drain-bottleneck`
  (verified before editing; all reads/writes inside that worktree).
- `go vet ./internal/storage/postgres/` → clean (from `go/`).
- `TestLaneTheoryDerivedPrefixMatchesProduction` (hermetic, no DSN) →
  PASS.
- `ESHU_POSTGRES_DSN='postgresql://allen@127.0.0.1:55433/postgres'
  ESHU_POSTGRES_DISPOSABLE=1 go test ./internal/storage/postgres/ -run
  'TestClaimLaneTheoryShim' -v -count=1 -timeout 20m` → FAIL with the two
  STOP lines above (run twice; values in the table). The FAIL is the
  verdict signal, not a regression: without a DSN the test Skips and the
  default suite stays green.
- Timing caveat: shared host, not a quiet room; medians of 5 with 1 warmup
  per query, reproduced on a second full run (fresh database). Buffer
  counts are cache-state independent and identical across runs.

## Not covered

- Node-level plan breakdown (which CTE owns the 60 ms) was not captured;
  totals plus identical buffer counts already settle both thresholds.
- QA-scale absolute wall time on QA hardware: the comparison is relative
  (laned vs unlaned on identical data), not a production latency claim.
- L=2 timing, expression-index variants, and lock-arm placement were not
  measured: the approved mechanism failed its own bar, and unapproved
  variants need a fresh ruling first.
