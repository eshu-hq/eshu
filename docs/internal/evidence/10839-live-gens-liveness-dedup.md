# #10839 live-generation liveness dedup: QA shim evidence

## Claim

`buildWinnersKeyedActiveFactsSQL` paid the active-generation liveness check
once per candidate copy instead of once per distinct live generation, so
per-tick cost scaled with copies-per-identifier instead of identifiers. The
fix precomputes the live generation set once per tick and semi-joins the
narrow probe to it. No chunk, worker, cadence, lock, or index change.

## Read-only measurements (QA replica, 2026-11-24, statement_timeout bound)

- Pinned oldest-200 dirty identifiers (first set, scanned pre-filter copies):
  50,123 copies collapse to 236 distinct live `(scope_id, generation_id)`
  pairs (212x dedup); 13,260 of them are kept (live) rows.
- Pinned single key (`brace-expansion` / CVE-2026-45149): 318 copies across
  160 scopes, all 318 live (zero retired discards post-#10020-predrain).
- Shipped shape, same snapshot: liveness joins ran 50,123 probes each
  (100,246 total), ~622k buffer hits at ~10x the fact fetch, and the wide
  `fact.*` MATERIALIZED CTE spilled 73 MB to disk (temp written 9,133
  blocks).
- Row-set equivalence on kept rows (same statement, same snapshot, both
  directions): 13,260 = 13,260 with 0 diffs on the first set, and
  24,936 = 24,936 with 0 diffs on a second pinned set (279 distinct live
  pairs there).
- Same-snapshot timing: shipped arm 6,531 ms vs restructured arm 298 ms
  (cold-cache run; the gap is I/O-dominated on both arms).
- Narrow probe (fact_id only) vs wide probe on one key, warm cache:
  1.5 ms / 370 buffers vs 1.69 ms / 370 buffers — the warm gap is invisible,
  so the QA cost is cold heap + detoast + temp spill + probe count, not the
  index descent.

## Falsifiers checked

- Retired-generation waste: falsified on current data (318/318 live on the
  pinned key; 64,019 of 64,048 copies join a live generation). The #9975-era
  triple fan-out was already fixed by the 291 index; what remains is live
  multiplicity, not dead rows.
- Chunk bound as cause: falsified as root cause (per-copy liveness cost is
  chunk-invariant); retained as amplifier (chunk-1000 trial: 1.82x
  per-row, cause not shown, reverted in #11310).
- Worker count / serialization: rejected (single-flight lock; AGENTS.md
  Serialization Is Not A Fix).
- Missing liveness index: rejected (both joins already have unique access
  paths; the defect is probe *count*, with scope_id as Filter beside the
  generation index condition).

## Bar status for #10839

Theory shim bar (equivalence + loops collapse + buffers/temp down): PASS.
Remaining per the arbiter ruling: hermetic failing-first tests (landed with
the fix), full-suite proof, and the QA before/after (scanned-copies
telemetry + build-phase timing with dirty-depth derivative) after deploy —
EXPLAIN alone does not close #10839.

## Performance Evidence (#10839):

Tick-invariant floor note: the live_gens build reads all live scopes and generations once per tick regardless of tick size (two small-table scans, ~17k scopes and ~31k generations, one hash join; ~150 ms cold on the QA replica, expected buffer-resident warm, ~1% of the 15 s tick bar at the 200-identifier bound; idle ticks skip the build entirely). Sub-200 ticks near drain pay the same absolute floor. Falsifiers that re-open tick-scoping as require-change: warm floor intercept p95 above 500 ms, floor above 1 s at observed scope counts or growing linearly with scope_generations past ~300k rows with no planner flip to PK probes, or live scopes above 100k. The post-deploy intercept (build_active_facts duration vs ChunkIdentifiers deciles, including the smallest decile, plus live-scope and scope_generations counts) is part of the #10839 QA before/after below.

Baseline is the shipped `buildWinnersKeyedActiveFactsSQL` on the QA read
replica (PostgreSQL 18.6-eshu-7957, `statement_timeout` 600s): the liveness
joins ran 50,123 probes per table on the first pinned set (100,246 total),
~622k buffers at ~10x the fact fetch, and the wide CTE spilled 73 MB (temp
written 9,133 blocks), for 6,531 ms end to end on a later timed snapshot of
the oldest-200 identifiers (15,098 scanned copies there). After is the
restructured statement on the identical snapshot: hash semi-join against
the once-per-tick live set, no per-copy probes (CTE scan loops 1), 298 ms
end to end. Input shapes are the oldest-200 dirty identifiers in two pinned
sets (50,123 scanned / 13,260 kept, and 24,936 kept). Terminal row counts:
row-set equivalence on kept rows holds with zero diffs both ways on both
sets.
The change is safe because the output row set is identical by construction
(same five-tuple predicate, fact_kind/tombstone filter, and liveness
equalities) and proven by differential; it touches no chunk, worker,
cadence, lock, or index. Telemetry evidence after deploy: the existing
`build_active_facts` phase timing plus the `ChunkScannedCopies` counter
(#11344) with the dirty-depth derivative over a comparable window.

## No-Observability-Change (#10839):

No phase, span attribute, or result-counter change ships here: the tick
still records `build_active_facts` per phase, `ChunkScannedCopies` semantics
are untouched (that probe's text is unchanged), and the telemetry reference
needs no update. Post-deploy watch reuses the existing phase histogram and
scanned-copies counter.
