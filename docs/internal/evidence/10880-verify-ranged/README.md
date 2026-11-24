# Ranged supports verification: theory proof (#10880 decision 6)

Refs #10880. The arbiter's decision 6 (2026-11-23) found the supports backfill's
verification a liveness and vacuum-horizon defect above QA scale. Two unbounded
statements each held one snapshot, could not resume, and grew with the table. On
QA with 11.6M live findings, the orphan probe took up to 22.9 min and the missing
count took 45.2 min. The arbiter ordered a resumable ranged verification, proved
on the replica first. Each statement must be index-bounded and short, the cursor
must live in the marker row, and the soundness basis must not change.

## Method

All runs used the QA read replica only, never the primary. Each
session ran `BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='30s'`. Each run was an `EXPLAIN (ANALYZE, BUFFERS,
TIMING OFF)` of one slice. No full scan, no setting change, no primary access.
The two shipped statements were planned with plain `EXPLAIN` only, never run.
Each slice started at a cursor taken from a different position in the
uniformly hashed fact id space, so cache reuse between runs is small. Replica
replay lag at the start was 0.01 s.

Catalog read (2026-11-23 23:04Z):

| Relation | Rows (estimate) | Size |
| --- | --- | --- |
| `fact_records` | 96.8M | 193 GB |
| `fact_records_pkey` | 96.0M | 21 GB |
| `fact_records_supply_chain_impact_active_scan_idx` (`scope_id, generation_id, fact_id`, partial on live findings) | 11.6M | 33 GB |
| `supply_chain_impact_finding_supports` | 11.6M | 19 GB |
| `supply_chain_impact_finding_supports_pkey` | 11.6M | 2.0 GB |
| `supply_chain_impact_finding_supports_scope_generation_idx` | 11.6M | 687 MB |

The marker `supply_chain_impact_finding_supports_v1` was complete with 0 sweeps.
`pg_stats` puts the most common (scope, generation) at a frequency of 0.0017
(about 20,000 supports). That is a sample estimate, not a bound; the exact
largest group is measured below (16,858 supports).

## Today's statements (plain EXPLAIN)

- Orphan probe: `Seq Scan on fact_records` (Filter `is_tombstone`, the 193 GB
  heap), then a supports primary-key probe per tombstone.
- Missing count: an `Index Only Scan` of
  `fact_records_supply_chain_impact_status_lookup_idx` over 13.3M estimated
  rows, with a supports primary-key anti-join. With no gap, the `LIMIT 1000`
  never stops it early.

## Shapes measured

| Run | Shape | Rows | Time | Verdict |
| --- | --- | --- | --- | --- |
| 1 | Orphan only: supports by primary key, `fact_records_pkey` probe per row for `is_tombstone` | 10,000 | 7.39 s (probe IO 5.0 s) | Projects 143 min. Rejected |
| 2B | Orphan only: supports in (scope, generation, fact) order, primary-key probe | 10,000 | 10.34 s | Rejected (the probe, not the order, costs) |
| 2C | Same, `NOT EXISTS` live fact of the same key (planner chose the primary key) | 10,000 | 6.96 s | Rejected |
| 1 | Missing only: `active_scan_idx` row cursor, supports primary-key anti-join | 50,000 | 8.05 s | Projects 31 min. Meets the bar |
| 3 | Combined interval slice (below) | 50,000 | 10.64 s, 10.03 s | Projects 40 min for both halves |
| 3 | Combined interval slice | 20,000 | 4.45 s | Projects 43 min for both halves |
| 5 | Shipped constant, prepared, `force_custom_plan` | 20,000 | 4.62 s, 4.64 s | Same plan as run 3 |
| 5 | Shipped constant, prepared, `force_generic_plan` | 20,000 | 6.01 s | Same plan shape, no seq scan |

The orphan-only shapes are cold random reads of the `fact_records` heap, one per
support, at about 0.7 to 1 ms each. No ordering of the supports side removes
that cost, so a per-support probe cannot meet the projection.

## The shipped shape

One statement per slice and one snapshot per slice, for both halves.
`supply_chain_impact_supports_verify.go` holds the text and documents it:

1. Read up to N supports after the cursor in (scope, generation, fact) order:
   `supply_chain_impact_finding_supports_scope_generation_idx` plus an
   incremental sort of each pair's fact ids.
2. Read up to N live findings after the cursor:
   `fact_records_supply_chain_impact_active_scan_idx`, `Index Only Scan`, one
   index search.
3. The interval ends at the smaller last key of a side that came back full.
   With neither side full, it runs to the end of the key space and the slice is
   the last one. Both sides then hold every row of the interval, so consecutive
   slices tile the key space.
4. Missing: live findings in the interval with no support of the same
   `fact_id`. This is the unsliced count's predicate (supports primary-key
   `Index Only Scan`, 20,000 searches, 0.12 s).
5. Orphan: supports in the interval with no live finding of the same key (a
   hash anti join of the two slices), then the `fact_records_pkey` probe for
   `is_tombstone` on those unmatched rows only. A clean slice never runs the
   probe (`never executed`). The predicate is the unsliced probe's: a support of
   a tombstoned fact has no live finding of its fact id, so it is always
   unmatched.

The run 5 cost split at 20,000 rows was the live index walk 3.05 s, the supports
walk 0.18 s, and the missing probes 0.12 s. The live index is 33 GB for 11.6M
entries, so the walk reads about 0.36 pages per entry. That walk dominates, and
the total grows linearly with live findings. The pass time does not depend on N;
N sets only the snapshot length. The default is N = 20,000 (4.6 to 6.0 s).

Projection at QA scale, from the shipped statement (run 5): ceil(11,633,317 /
20,000) = 582 slices x 4.62 to 6.01 s = 44.8 to 58.3 min for both halves. The
upper figure is the forced generic plan, which the reducer's pgx statement cache
can reach after five executions on one connection. The two statements it
replaces took 22.9 + 45.2 = 68.1 min. A fleet 1.3 times QA projects to 58.3 to
75.8 min in 757 slices. This is a pair-boundary projection: it uses slices that
start at a pair boundary, and a cold slice that starts inside a large pair is
slower (20.34 s measured below). Every statement stays bounded by N, or by the
largest pair when a pair exceeds N; none does on QA.

## Largest group and a mid-pair slice (run 9, after review)

The supports side walks the (scope_id, generation_id) index, which has no
fact_id, so a slice whose cursor is inside a pair re-reads and sorts that pair's
supports from its start. A pair of G supports costs about G squared over N rows
across its slices once G exceeds N; below N, a slice re-reads at most one group.

- Exact census, read-only on the replica (`GROUP BY scope_id, generation_id`
  split into scope ranges: the 101 `pg_stats` histogram buckets, and the one
  bucket that timed out split again into index-walked ranges of about 100,000
  rows): 11,645,720 supports in 6,972 pairs. The largest group is 16,858
  supports, then 15,531 and 11,686. Every group is under N = 20,000.
- The shipped slice with N = 20,000 and the cursor at the 8,430th fact of the
  largest group: 20.34 s cold under the custom plan. The supports walk, with the
  whole group re-read, cost 0.49 s of reads; the live index walk 6.12 s; the
  missing-support primary-key anti-join 12.32 s, from 9,655 heap fetches on
  supports pages that were not yet all-visible. An immediate rerun under the
  forced generic plan took 1.07 s warm.
- So the mid-pair re-read is not the cost; heap fetches after recent supports
  writes are, and they shrink once autovacuum sets the visibility map. The worst
  cold slice measured (20.3 s) is inside the 30 s bar.

## Performance and observability markers

Performance Evidence: baseline is the shipped verification on QA (PostgreSQL
replica, 11,633,317 live findings): the orphan probe up to 22.9 min and the
missing count 45.2 min, one statement and one snapshot each, terminal state
marker set with 0 sweeps. After: the shipped slice statement, prepared, 20,000
rows per side, 4.62 s and 4.64 s cold (custom plan) and 6.01 s (forced generic
plan), result rows 20,000 / 20,000 with 0 missing and no orphan; 20.34 s cold for
a slice that starts inside the largest group. The whole verification projects
to 582 slices, 44.8 to 58.3 min (pair-boundary slices), with every statement
bounded by the slice size, or by the largest pair while one exceeds it, instead
of the table. Safe because both predicates
are the unsliced ones, the cursor is guarded by the sweep it belongs to, and the
completion statement keeps the fence recheck.

Observability Evidence: each slice is counted by
`eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total{outcome}` and
timed by `eshu_dp_supply_chain_impact_supports_backfill_verify_slice_duration_seconds{outcome}`
(`advance`, `orphan`, `gap`, `last`, `failed`, a failed slice keeping its
duration); a slice that neither restarted the sweep nor set the marker is the step outcome
`verify` on `eshu_dp_supply_chain_impact_supports_backfill_steps_total`; the
`verify_gap` warn log now carries `slice_duration_seconds`; the cursor is readable
in the marker row (`verify_cursor_*`, `verify_sweep`).

## Soundness

Today's two statements already ran in separate snapshots. The basis is the
same-transaction maintainer, the old-writer fence, and the fence recheck in the
completion statement. Slicing keeps that basis:

- A maintainer writer keeps supports current in its own transaction, before or
  after any slice.
- An old writer marks its scope dirty. Every step drains first, and the
  completion statement refuses while the fence is not empty.
- A missing support in any slice restarts the sweep, exactly as `verify_gap` did.
  The restart drops the verification cursor.
- An orphan in a slice has its pair's orphans removed under the pair's
  conflict-domain lock, and the cursor stays, so the slice runs again.
- The cursor counts only while `verify_sweep = sweeps` (migration 348). An older
  binary's restart bumps `sweeps` without clearing the cursor, which voids it.
  The advance is guarded by the sweep the step read.
- Completion runs in one transaction: a statement drops the cursor, guarded by
  the sweep the step read, then the pinned completion statement rechecks the
  fence. A marker whose `completed_at` is reset later without a sweep bump
  verifies from the start, and a runner whose sweep was restarted meanwhile
  cannot set the marker. A dirty fence rolls back and keeps the cursor.

## Rejected and not done

- No new index. The two existing indexes serve both sides.
- The count-equality form, count(supports) = count(live) in one statement, is
  unbounded by construction. Rejected.
- The per-support orphan probe was rejected on run 1 and run 2.

## Not checked here

- Warm-cache and primary timings. These runs used a cold replica cache.
- A pair larger than N on another fleet. Its slices re-read it, about G squared
  over N rows in all; none exists on QA (largest 16,858).
- The live proofs (writer between slices, old writer, restart, seeded RED/GREEN
  against the real statement) live in
  `go/internal/storage/postgres/supply_chain_impact_supports_verify_live_test.go`
  and run in the reducer-contention gate.
