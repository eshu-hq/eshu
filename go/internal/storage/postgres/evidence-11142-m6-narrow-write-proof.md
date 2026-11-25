# #11142 M6 narrow-store write proof evidence

Three narrow-store write shapes built side by side on QA-shaped payloads
with the production index set (issue
[#11142](https://github.com/eshu-hq/eshu/issues/11142)):

- **prototype**: narrow table carrying key, ranking inputs and winners-only
  payload, plus an all-column covering index and the canonical key.
- **deduped**: the same table without derivable/duplicated bytes (no payload,
  no source_confidence, no redundant wide columns); winners-only payload is
  fetched from `fact_records` by `fact_id` after ranking.
- **index-expression**: no second table; ranking inputs ride as trailing key
  expressions of a new keyed-index-family member on `fact_records`.

The builders live in `supply_chain_impact_winners_narrow_shapes.go`. The
populate statements derive their candidate set from the shipped full-path
inner select (`supplyChainImpactWinnerActiveFactsInnerSQL`, split out of the
shipped const byte-identically so the winners contract pin still holds), and
the differential composes the shipped winner body over each shape's candidate
set. The proofs are `supply_chain_impact_winners_narrow_write_proof_live_test.go`
(write bar + differential), `supply_chain_impact_winners_narrow_replay_live_test.go`
(bounded replay + concurrency), and the knob file
`supply_chain_impact_winners_narrow_knobs_test.go`.

## Corrected design note

Shape (c) was specified as an "index-only first pass". That premise is false
on this stack: on PostgreSQL 18 any query that references a base-column Var
inside an expression can never use an Index Only Scan over an expression
index. Reproduced in isolation (plain covering index goes Index Only Scan;
any expression index stays Index Scan, selective predicate, fully set
visibility map). The shipped 291 probe has the same property, so the earlier
"visibility map unset" aside was insufficient. Shape (c) is kept with the
corrected claim: an Index Scan over a narrower index, ranking scalars from
the index tuple with no payload TOAST detoast, at the measured
index-maintenance write cost.

## Accuracy

Full keyed differential over the same snapshot: the winner digest of every
shape equals the shipped digest byte-for-byte.

- Smoke (2,000 QA-shaped facts): shipped `66e1b874aab5eed08e3913520fc71967f264b56deb71d818b248e764f51edca1`,
  all three shapes match.
- Full (41,000 QA-shaped facts): shipped
  `6787ff74aab5eed08e3913520fc71967f264b56deb71d818b248e764f51edca1`,
  all three shapes match.
- First-pass candidate sets agree across shapes (50/50/50 on the sampled
  50-identifier probe).

## Performance Evidence:

Baseline: the shipped per-identifier probe (`WinnersKeyedActiveFactsSQLForTest`,
derived not hand-copied) on the same disposable database. After: each shape's
first pass (`NarrowShapeFirstPassSQL`) over the same 50 identifiers.
Backend: PostgreSQL 18.6, local Ubuntu cluster, `VACUUM (ANALYZE)`d steady
state (a freshly loaded fixture has no visibility map; QA carries a set map
via autovacuum, so the proof vacuums before measuring). Input shape: 41,000
QA-shaped findings (production writer, production encoder, full production
index set including the 291 keyed probe index) in 42 scopes.

Write bar (WAL via `pg_wal_lsn_diff`, wall clock per fact):

- prototype populate: 5,058 bytes/fact, 0.0395 ms/fact.
- deduped populate: 1,354 bytes/fact, 0.0312 ms/fact.
- index-expression marginal (identical 500-fact write passes with and without
  the trailing-expression index; the fact write is sunk cost): 1,017
  bytes/fact, 0.0135 ms/fact.

Read bar (per-identifier `EXPLAIN (ANALYZE, BUFFERS)`, shared buffers):

- shipped probe: 2,470 buffers (2,308 hit + 162 cold read).
- prototype first pass: 91 buffers (covering index).
- deduped first pass: 87 buffers (covering index).
- index-expression first pass: 526 buffers (Index Scan using the
  trailing-expression index; not index-only, see the corrected note above).

Writer scaling on disjoint scopes (facts/hr, WAL rate, deadlocks; drain
ticks keyed, parity with a fresh recompute after every round):

- 8 writers: 400 facts, 14.0M facts/hr, 183.05 MB/s WAL, deadlocks 0, drain
  2 bounded ticks.
- 16 writers: 800 facts, 27.5M facts/hr, 300.40 MB/s WAL, deadlocks 0, drain
  4 bounded ticks.

Rates are local-fixture rates (small scopes, unloaded host), not QA-absolute
facts/hr: the rows are QA-shaped and the index set is the production set,
but the counts are scaled (41k, not 8.37M) and the host is not QA. QA-absolute
extrapolation stays unresolved pending a QA-backend run.

Bounded replay (worst-case backlog: every identifier rewritten at once,
rebuilt test binary `go1.26.11`, winners contract version 2): 1,000 facts
drained in 5 bounded keyed ticks (200 identifiers/tick production chunk
bound), parity with a fresh recompute holds, digest re-ranks correctly.

Concurrency: single-flight lease admits exactly one ticker (deterministic
held-lease tick reports Skipped and writes nothing); three writer passes
racing the tick lose no update (parity with a fresh recompute); deadlocks 0
with no serialization (writers and ticks overlap).

## RED-first provenance (Box 1 bars)

The bars the write-bar test enforces were each observed to fail before
they were observed to pass; nothing below is retyped from memory:

- Mutation control (near-miss RED): the control rewrite (no-op) is
  asserted to keep the digest stable while a single flipped ranking
  input must move it with the winner count unchanged. The lane's RED
  run exited 1 on the no-op rewrite mutant; the committed test
  (`TestSupplyChainImpactWinnersNarrowNearMissSingleFlipLive`) asserts
  both directions live on every run, and the seeded pair below covers
  the comparators.
- Blindness guard (statio RED): the shipped probe asserts a nonzero
  statio delta and fails loud otherwise —
  `shipped probe recorded 0 statio block touches: the statistics-view
  measurement is blind` — observed firing (exit 1) on a legacy
  `pg_stat_io_*` measurement path before the test was fixed to read
  `pg_stat_database` blks plus `pg_stat_force_next_flush`.
- Shipped-shape drift (CI RED, PR #11404 contention gate): after the
  #11400 two-step build landed, the un-mirrored test failed with
  `ERROR: relation "winners_keyed_narrow" does not exist
  (SQLSTATE 42P01)`; the fix mirrors `runKeyedOrFull` (narrow build +
  analyze in the explaining transaction).
- Seeded pair (committed, in-repo): `TestNarrowWriteBarSeededRedGreen`
  trips the 4x comparator on an over-budget shape and on a blind zero
  measurement, trips the deadlock bar on a nonzero count, and passes
  the in-bar cases — a comparator that cannot fail, or one broken to
  pass everything, fails this test.

## Observability Evidence:

No new telemetry stages: the proof exercises the already-instrumented keyed
tick path (claim/build/settle/apply phases, `ResweepWinnersKeyed` result
counts, watermark/horizon stamps) and the ledger deadlock counters. The
per-shape bars are test-log evidence (`t.Logf` lines quoted above), not new
metrics, because the shapes are proof-grade builders, not a production write
path. Wiring any shape into the production writer must add its own spans and
counters then.
