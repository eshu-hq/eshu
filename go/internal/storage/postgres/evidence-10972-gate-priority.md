# Evidence — #10972 gate-waited claim tier costs one extra in-memory sort (no I/O change)

## What changed

The QA canary held chunk 2 for ~8h: fair-rank order kept serving older
ordinary backlog while a live NVD generation a gate waited on sat
unclaimed. The batch claim now carries a bounded priority tier: a
MATERIALIZED `gate_waited_generations` CTE (one aggregate over the
`vulnerability_nvd_baseline_checkpoints` table), a hash join of that
one-row gating set against the candidate set, a `ROW_NUMBER()` window
partitioning gating from ordinary rows ordered by wait start, and a
`gate_boost` key placed behind `reclaim_rank` and ahead of the fair-rank
keys in the rest-arm ORDER BY. At most one boosted row per batch (the
oldest-waiting generation's oldest row first, FIFO); every other row keeps
its ordinary key, so non-gating relative order is unchanged. A drained or
completed year contributes no live candidate rows, so the tier
auto-expires. The candidate CTE additionally projects `generation_id` for
the join; the reserved arm, the lock/claim tail, lease handling, and every
claim-time fence are untouched, and the single-claim query is unchanged.

## Fixture and backend

Disposable Postgres 18.7 (Alpine, `postgresproof` fresh database per run)
with the full migration set applied. Fixture: 2,000 pending reducer rows
in distinct conflict keys (1,900 ordinary with older `updated_at`, 100 in
one gate-waited generation with newer `updated_at` so they lose without
the boost), plus a `year_complete = false` checkpoint row naming the
waited generation — the drain-gate shape. A second fixture scales the
same shape to QA reality: 7,900 ordinary + 100 gate-waited (8,000 rows).
Same `$1..$8` args both sides (fixed `now`, lease owner + 60s lease, batch
limit 16), three fresh databases per side, `EXPLAIN (ANALYZE, BUFFERS)` on
the full `ClaimBatch` statement.

## Measurements

Plan parity: the new plan adds exactly the designed tier and nothing
else — one `CTE gate_waited_generations` (HashAggregate + Seq Scan over
the ≤30-row checkpoint table), one hash join of that one-row set against
the candidate set, one `WindowAgg` with its sort, and a wider rest-arm
sort key. Sort nodes 18 → 20, same shared-buffer profile (1,530 hits at
2,000 rows; 6,608 vs 6,636 at 8,000 rows), no new scan node, planning
time unchanged (~2 ms). Sorts stay in-memory quicksorts (<300 kB).

| fixture rows | base execution | tier execution | delta |
| --- | --- | --- | --- |
| 2,000 | 11.8–12.1 ms (median 11.9) | 17.8–19.4 ms (median 17.9) | +6 ms |
| 8,000 | 45.4–45.9 ms (median 45.8) | 73.6–76.3 ms (median 73.8) | +28 ms |

Both sides scale the same way with candidate count (sort-dominated, no
complexity change); the tier adds a roughly constant factor (~1.6x) of
CPU-only work per batch claim.

Performance Evidence: full-claim `EXPLAIN (ANALYZE, BUFFERS)` old vs new
on the same 2,000-row fixture (11.9 ms vs 17.9 ms median) and the same
8,000-row QA-shape fixture (45.8 ms vs 73.8 ms median), same args, three
fresh databases per side; Sort nodes 18 → 20 with an identical buffer
profile and no new scan node, so the delta is one extra in-memory sort
plus the boost window and a wider sort key. Per-batch cost at QA scale
stays under 80 ms against a worker loop cadenced in seconds, and it buys
out the 8h drain-gate hold the canary measured: chunk-1 end-to-end
committed 1,546 facts with a 9.7s upsert, so claim time is noise next to
the write path either way.

No-Regression Evidence: the pinned ORDER BY, LIMIT-composition, and
rejoin-shape tests plus the four live `TestClaimBatchGatePriority*`
proofs (boost, cap-at-one, expiry on drain, superseded-year de-boost)
all pass on the shipped query; the #3624 rank-once, gate-precompute,
fence, and #10916 rejoin differentials keep their 0/0 membership with
order identical up to the intentional boosted-first partition.

No-Observability-Change: the tier adds no telemetry, log line, metric
label, or failure class — `telemetry.go` and the stderr diagnostic line
are untouched, and every claim-time fence still reports through the same
result classes an operator already reads. The existing claim
latency/count signals cover the path; a slower claim would show up there,
and a stuck gate still shows up as an unclaimed waited generation, not as
a new signal to learn.
