# #10839 capacity lane: temp indexes bound the keyed tick scope expansion

Refs #10839. Follows the Decision 4a theory shim
(/tmp/lane-capacity-report.md): at worst-case composition ~83% of one
keyed tick goes to the scope-expansion fan-out in convert-remainder and
the applied delete, which probe the admitted-window by generation-size
expansion with statistics-blind nested-loop sequential joins
(5,071 x 3,558 row comparisons per delete).

## Change

Three session-local indexes plus ANALYZE on the tick's transaction-scoped
temp tables, created after each CTAS/fill and before the statements that
plan on them. No shipped statement text changes.

- `winners_keyed_consumed(seq)` — the applied delete's dirty-log join.
- `winners_keyed_scope_idents(scope_id, generation_id)` — the expansion joins.
- `winners_keyed_chunk_idents` unique five-tuple — the chunk-membership
  probes in convert and the applied delete.

Each `CREATE INDEX` spells its table name literally in the same file as
that table's `CREATE TEMP TABLE`, so the DDL parity guard still classifies
the index as session-local. Row sets, oldest-first order, exact-applied
delete, and remainder-conversion-before-delete ordering are untouched, so
the single-flight lock, workers, batching, and claim shape are untouched
too (no concurrency proof owed beyond the existing live suite).

## Performance Evidence:

- Baseline: 2.53s claim-to-commit on local PostgreSQL 18, shim
  composition rebuilt from `/tmp/lane-capacity-bootstrap.sh` +
  `/tmp/lane-capacity-fixture.py`: 5,000-row admit window (70 scope +
  4,930 fact rows), 42,000-row scope expansion, 200-identifier chunk,
  14,000 kept facts. Reproduces the shim T1 2.47s within 2.5%.
- After: 1.25s on the byte-identical composition (second database,
  same scripts), a 51% cut against the 36% minimum (target <=1.59s).
- Stage split (MARK clocks): apply 2.295s -> 0.902s; index/analyze
  overhead +0.095s in consume/build_idents; probe, chunk, and commit
  unchanged.
- Terminal counts identical old vs new: admitted 5000, identifiers 600,
  kept 14000, consumed tally 1638, remaining 3692, apply
  0/0/0/14000/14000.
- Scripts: `/tmp/lane-capacity-ab-run.sh` (builds both databases, runs
  `/tmp/lane-capacity-tick1.sql` vs `/tmp/lane-capacity-tick1-new.sql`
  with `\timing`); outputs `/tmp/lane-capacity-tickA.log` and
  `/tmp/lane-capacity-tickB.log`.
- Contract: `TestSupplyChainImpactWinnersResweepKeyedScopeExpansionPlanLive`
  pins the after-plans (index probes, no seq-join filters); verified red
  on the before-plans. Full winners live surface green after, including
  the concurrency proof.

## Composition Re-proof (c835/92c204, 2026-11-18):

F1 (bounded tick, bar <=1.59s, worst-case shape: 5k admit LIMIT,
671-row versioned dirty window, 200-identifier chunk, migrations
303-308 applied). Fixture DB `lane_capacity_c835` (PG18 container,
42,000 fact_records), tick-only walls: 296ms, 293ms, 290ms (200-400
real versioned rows; identifiers ~1844, keys 14000); fresh disposable
DB: 36ms. Full 1,071-real-row / 8,567-version drain (exceeds the
worst-case shape): 6 ticks, max 282ms, drained to zero. Verdict: F1
PASSES with ~5.6x margin.

The earlier 10.76s "pristine" number measured fixture setup + tick,
not the tick. Isolated arm-vs-tick split: production-writer arm of
671 findings x 7 version bumps = 16.2s unbatched in a fresh DB
(0.71s batched in the fixture DB); tick = 36ms-0.30s. Stats-blind
retest (pg_statistic emptied, 400 real rows) ticks at 294ms,
identical to the analyzed runs, so missing base-table stats do not
explain it either; the tick is stats-robust (temp ANALYZE + bounded
chunk carry it). The 10.76s number is VOID as a tick measurement and
is retained only as a setup-cost data point.

F2 (statement-sequence pin + scope-plan on the new tree):
`TestSupplyChainImpactWinnersResweepKeyedScopeExpansionPlanLive`
green on the new tree with a 5,000-row over-cap arm (admitted window
at LIMIT): convert probes `winners_keyed_chunk_idents_all_idx` +
`winners_keyed_scope_idents_scope_idx`; applied delete probes the
dirty-log pkey (`Index Cond`); fallback delete hash-joins on the seq
equijoin (`Hash Cond`, no Join Filter) - planner-optimal at over-cap
width with `winners_keyed_consumed_seq_idx` present (per-row pkey
probes would be slower), pinned as a join-key shape (hash or index
cond) with the measurement cited in the test comment. `Identifiers`
doc fixed to marks/dirty_version sums per the verdict.

Observation for the #10840 lane (non-blocking, truth unaffected): a
single full-generation replacement write (471 new findings to a
generation holding 600 queued/tombstoned units) also re-marks the
generation's queued units - 200 superseded live facts become
`fact_retract` marks, 400 already-queued tombstone units each bump
+1 version (first timestamps preserved, version-collapse semantics).
Full drains still reach zero and tick cost stays flat (<=282ms), so
this is a version-inflation observation, not a correctness or
capacity finding; tracked under #10840 for the upsert lane to
assess at live backlog scale. Repro is a 12-line live probe against
the fixture DB.

No-Regression Evidence: the only production-code touch in this diff
is a doc comment on `SupplyChainImpactWinnersResweep.Identifiers`
(zero runtime behavior change); the remaining Go changes are a
test-only EXPLAIN export and a live plan-shape test. Baseline and
after for the re-proof, PG18 container, 42,000 fact_records, 5k
admit LIMIT, 200-identifier chunks: bounded-tick walls 36ms-297ms
(6-tick full drain of 1,071 real versioned dirty rows / 8,567
versions, max 282ms) vs the 1.59s bar; terminal counts drain to
zero; full `postgres` package green (13.2s), scope-plan live test
green (5.20s). Safe because no statement, row order, bound, lock,
or worker changed.

No-Observability-Change: no new metric, span, log field, route,
worker, lease, or batch knob; phase timings keep reading from the
existing per-phase tick histogram.

## No-Observability-Change:

No new metric, span, log field, route, worker, lease, or batch knob. The
new statements run inside the existing claim/consume/build_idents phases,
so the per-phase tick histogram the lane's diagnostic signal reads shows
the improvement with no new instrumentation.
