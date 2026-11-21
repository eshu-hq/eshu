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
trips. Default bound 200 keeps 1,000 params, far under the 65,535 ceiling;
non-default operator bounds above 13,107 identifiers are a known cliff
tracked in #11157.
