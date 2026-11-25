# #11441 stored_range complementary UNION ALL: performance evidence

Refs eshu-hq/eshu#11441 (arbiter verdict-B). The single-scan
`stored_range` predicate was `WHERE <lower>((SELECT is_last FROM
range_bounds) OR <row> <= <bound>)`. The OR over the `is_last` InitPlan
blocked every btree path, so the stored side sequentially scanned the
canonical winners table at any table size. The rewrite splits it into
complementary UNION ALL arms — bare `is_last`, and `NOT is_last` with
the pure row-comparison — so the bounded arm rides the migration-356
canonical-ident index while only the unbounded arm sequentially scans.
Read-path only: the shipped renderer, `range_facts`, and all
capture/lease/queue/clock paths are untouched, and there is no new
migration or index.

No-Regression Evidence: the RED 2 cost test still passes on the padded
1500-identifier fixture (budget 2000: shipped total 141457 blocks vs
single-scan 143443; budget 10000: 775218 vs 777141, inside the 1.25x
parity bound). Old-vs-new first-range statements on the 50k-row padded
fixture read byte-identical totals under the natural planner (893 vs
893 shared blocks, execution time within 1% across 5 reps), and the new
equivalence test proves byte-identical row multisets plus disjoint arms
on last, selective, unselective, empty-winners, NULL-bound, next-range,
and full-sweep shapes. Backend/version: PostgreSQL 18.6, disposable
proof databases. Input shape: 60-identifier audit fixture with perturb,
50k high-key pad winners, first range at budget 200 (not last) and
whole-corpus last range. Safe because the statements return identical
row multisets (proven, not argued), the change adds no write, lock,
queue, or DDL, and the bounded arm returns nothing when the range is
last (the `NOT is_last` gate evaluates first, zero loops measured).

Benchmark Evidence: with the index path forced on the padded fixture,
the bounded arm serves 49 rows from 12 shared blocks in 0.04 ms
(Bitmap Heap Scan over a Bitmap Index Scan on
`supply_chain_impact_winners_canonical_ident_idx` with the pure
row-comparison as Index Cond) while the unbounded arm executes zero
loops; statement totals 1010 vs 1015 shared blocks old-vs-new with
timing at parity. The winner-body recompute dominates both statements
(the stored leg is a few percent of each call), so the statement-level
delta is noise and the win is structural: the folded pre-UNION ALL
statement never touches the 356 index under the same forcing (the
committed pin's control leg), while the rewritten bounded arm always
does. The 356 index is now load-bearing for the bounded arm; its
presence is pinned by the migration byte-identity half of the same
test.

No-Observability-Change: no new metrics, spans, logs, status fields, or
dashboards; the staged-audit per-range observation line is unchanged.
