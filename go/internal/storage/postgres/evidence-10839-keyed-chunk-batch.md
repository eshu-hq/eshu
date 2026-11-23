# Keyed winners chunk batch (#10839)

`winnersKeyedChunkIdentBatch` packs a tick's chunk identifiers into one
multi-row INSERT instead of one INSERT per identifier (saturated tick: 1 list
+ 200 per-row INSERTs + index + analyze before probe/apply).

No-Regression Evidence: failing-first batch tests prove 200 Exec statements
before and exactly 1 after at the saturated 200 bound (single-ident and empty
windows pinned); admitted set, oldest-first order, truncate rule, temp
table/unique index/ANALYZE/restrict, S/H horizon, and exact-applied delete
are untouched, and no lock, worker, batch, chunk, window, or cadence knob
changed. Statement-shape proof only: tick-time headroom is NOT claimed here
(the per-tick dominant term stays plan/fan-out in convert/delete); the
rebuilt-binary bounded replay and 60-minute Bounded segment run post-merge
per the arbiter conditions on #10839.

Observability Evidence: no new telemetry; the existing BuildActiveFacts
tick-phase histogram and per-tick statement count expose the dropped round
trips. Default bound 1000 keeps 5,000 params, far under the 65,535 ceiling;
non-default operator bounds above 13,107 identifiers are a known cliff
tracked in #11157.

## Nov-23 default raise to 1,000 (#10839)

Performance Evidence (#10839): arbiter-verified Nov-23 sequence replay at the
1,000-identifier default: throughput 2.30x/2.30x (ident/s, rows-deleted/s),
max tick 554ms against the 15s per-tick bar. version-sums/s 0.86x is disclosed
as a non-gated diagnostic: it rides the replay as an observation only and
gates nothing. A saturated default tick carries 1,000 identifiers x 5 columns
= 5,000 params, far under the 65,535 ceiling and the 13,107-row #11157 clamp;
admitted set, oldest-first order, truncate rule, temp table/unique
index/ANALYZE/restrict, S/H horizon, and exact-applied delete are unchanged,
and no admit-window, clamp, cadence, or lane knob changed.

No-Observability-Change (#10839): no new telemetry; the existing
BuildActiveFacts tick-phase histogram and per-tick statement count expose the
tick, and default-bound ticks emit nothing new.
