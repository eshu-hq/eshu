# #10839 Convert Fix: Once-Per-Tick MATERIALIZED Chunk-Applied Set

The scope-remainder convert arm carried a correlated applied-scope
`EXISTS` that let the planner pick a Nested Loop Semi Join re-probing
the chunk per expansion row: 41,400 outer loops x 600 same-scope applied
rows = 24,840,000 `winners_keyed_chunk_idents_all_idx` probes, 50.2M
local buffer hits, ~24.1s of a ~22s slow tick (diagnosed on the retained
`d5_diagfast` pre-slow-tick database, pending 2599). The arm now
precomputes the chunk-applied `(scope_id, generation_id)` set once per
tick in an explicit `AS MATERIALIZED` CTE (which the planner cannot
inline back into the flip) and probes that small set in place of the
correlated `EXISTS`. The `NOT`-chunk-match remainder predicate, the
`GROUP BY` conflict key with `MIN(source)`, the K-order insert, the
`ON CONFLICT dirty_version + 1` tail, and the shipped default chunk
(200) are unchanged, as is every other chunk-match site: the
applied-delete stays bounded by its consumed-scope-row outer and the
restrict stays a single-level anti join (both EXPLAIN-cleared at the
slow alignment), so only the convert routes through the set.

## Performance Evidence (#10839 convert fix):

Shim (no production edit, old arm derived from the shipped constant):
retained `d5_diagfast` (pending 2599) and `d5_diagslow` (pending 2099),
local PostgreSQL 18.6, single backend, 69 scopes x 600 fan-out =
41,400-row expansion, disjoint chunk, rolled-back transactions.

| Database | Chunk | Old arm | Candidate arm |
|---|---|---|---|
| d5_diagfast | 200 | 6.5ms (fast shape) | 6.4ms, 707 local hits |
| d5_diagfast | 500 | 24,128ms, 24.84M chunk searches, 50,231,760 local hits | 6.6ms, 711 local hits, 0 inner chunk searches |
| d5_diagslow | 200 | 22,609ms slow shape | 6.4ms |
| d5_diagslow | 500 | 24,513ms slow shape | 6.7ms |

The candidate meets the pre-registered bar everywhere: no
per-expansion-row chunk re-probe (chunk hashed once, CTE scanned once),
within 10x of the 6.8ms fast arm, under 100ms, buffers orders below
50.2M, with no GUC, worker, batch, cadence, or retry change. Retained
databases verified unchanged after (2599/2099).

Parity on a converting fixture (one scope forced partially applied):
old-vs-new SELECT sets identical (590/590 rows, `EXCEPT` both ways 0,
keys plus `MIN(source)`); old-vs-new INSERT diffs byte-identical
including 590 `ON CONFLICT` version bumps. The production arm is
byte-identical to the shimmed candidate (dump-diffed, blank lines
aside), so every shim measurement applies to the shipped SQL.

Keyed family green on the final tree: parity
(`MatchesRecomputeAcrossChangeClasses`,
`RandomInterleavingMatchesRecompute`), contract, chunked (consume,
single-ident, truncate, zero-applied, scope-split, oldest-first),
horizon (bounded, never-regresses, remainder, late-commit x3,
tuple-order, db-sourced), tally/settle units
(`ConvertsBeforeItDeletes`, `KeyOrderedUpsert`,
`SelectDerivesFromShippedConvert`, drain lock order), deadlock
(`SettleDeadlockFreeLive`: zero 40P01, no retry, version-sum
conservation, empty residual), writer envelope
(`WriterStratifiedEnvelopeLive`, `ConvertMixedSourceLive`), probe plan,
fallback watermark, and both scope-plan tests including the new
slow-window plan guard (RED on the pre-fix tree, GREEN after).

## Full Decision-5 replay at shipped 200 (5/5 PASS):

Rebuilt binaries from exact SHAs (candidate fixed @7c149d7341, control pre-fix @e5e92648db), PG18 F1-port
composition (5,000-row admit window: 70 scope + 4,930 fact rows; 42,000-row scope expansion; 42,000 stored
winners), byte-identical arms (md5 equal), quiet-host canary (load1 max 1.14 < 8 bound on 16 CPUs).
Recorded on #10839 (comment 7030577480); command, walls, and terminals in the F1 format there.

| Bar | Candidate (fixed @200) | Control (pre-fix @200) | Verdict |
|---|---|---|---|
| Exactness (admitted/kept/deleted/remaining) | 5071 / 46930 / 5071 / 0, keyed==recompute, winners hash 5389611c:46930 x3 | identical terminals, identical hash | PASS |
| Throughput ident/s | 1849.3 | 74.4 (24.9x) | PASS (>= 2.0x) |
| Throughput rows-deleted/s | 857.9 | 34.5 (24.9x) | PASS (>= 2.0x) |
| Tail max tick | 0.334s (total 5.911s) | 20.390s (total 146.953s) | PASS (<= 15s, ratio 0.016) |
| Plan | CTE evaluated once (hash, loops=1), index probes, 0 Join Filter fan-out, max loops 5530 | — | PASS |
| Concurrency (6272-mark writer) | 32 ticks / 6.332s / max 0.340s, 0 errors, 0 deadlocks, head advanced, parity equal | — | PASS |

The fix cures the control's own 20.390s tail (control ticks 17-23 at 20.2-20.4s). No chunk-value change rides
this evidence: default chunk stays 200; 500/1000 remain separate claims requiring their own full replays.

## No-Observability-Change (#10839 convert fix):

The convert runs through the same instrumented `ExecContext` path as
before, so the existing `eshu_dp_postgres_query_duration_seconds`
spans still cover it with no new metric, label, span, log field,
worker, or runtime knob. Operators diagnose the path through the same
tick logs and Postgres query spans, plus the new slow-window plan guard
(`TestSupplyChainImpactWinnersResweepKeyedConvertSlowWindowPlanLive`)
and the MATERIALIZED shape pin
(`TestConvertWinnersKeyedScopeRemainderSelectUsesMaterializedChunkApplied`),
which both fail on the old arm.
