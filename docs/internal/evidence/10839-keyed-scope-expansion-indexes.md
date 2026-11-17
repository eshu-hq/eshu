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

## No-Observability-Change:

No new metric, span, log field, route, worker, lease, or batch knob. The
new statements run inside the existing claim/consume/build_idents phases,
so the per-phase tick histogram the lane's diagnostic signal reads shows
the improvement with no new instrumentation.
