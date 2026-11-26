# CTE-streaming keyed winners body (#9805 C5 / #9975 C4)

Stage 1 builds `winners_keyed_active_facts` without the payload: the copy
identity (`fact_id`, `scope_id`, `generation_id`) plus `source_confidence`
plus the once-extracted detoast fields. The keyed body streams through CTEs,
fetching the full payload by `fact_id` only for the emitted winner and
override rows. The streamed body and the narrow builds are derived from the
shipped statements with fail-closed anchors, never copied; the triple
reversal (stream, supports, detoast) chains back to the pre-supports
contract pin. Live_gens, the scanned-copies probe, the chunk bound, the
statement guard, and the index set are untouched.

Benchmark Evidence: on a QA-ratio corpus (8 identifiers at ~200 copies
each, the Leg-B QA fanout shape; 1600 winners) against disposable
PostgreSQL 18.7, one seed, both arms back-to-back in rolled-back
transactions over the same committed snapshot, round 1 cold per arm and
rounds 2-3 warm, `TestSupplyChainImpactWinnersKeyedStreamWallTimeLive`
measured keyed-tick wall-time (build + operator + select) with NO
wall-time win for the stream. The streamed cold ran after the legacy cold
over the same base tables, so its cache starts warmer; the test records
this order effect instead of hiding it.

Round 1 (cold): legacy total 100.398881ms (build 7.24232ms, operator
1.664019ms, select 91.492542ms), temp 3670016 bytes, select 3200 shared
buffers; streamed total 100.291279ms (build 14.561446ms, operator
987.894µs, select 84.741939ms), temp 1048576 bytes, select 8000 shared
buffers; winners 1600 both arms.

Round 2 (warm): legacy total 97.355962ms (build 5.916408ms, operator
1.855024ms, select 89.58453ms); streamed total 99.204599ms (build
14.033986ms, operator 1.06254ms, select 84.108073ms); temp 3670016 vs
1048576 bytes, buffers 3200 vs 8000, winners 1600 both arms.

Round 3 (warm): legacy total 97.251261ms (build 6.469428ms, operator
1.559587ms, select 89.222246ms); streamed total 98.094871ms (build
13.968404ms, operator 973.697µs, select 83.15277ms); temp 3670016 vs
1048576 bytes, buffers 3200 vs 8000, winners 1600 both arms.

The stream build costs ~2x the wide build (the detoast extraction moves
into stage 1) while the stream select is a few ms faster but touches 8000
vs 3200 shared buffers (5.0 buffers per emitted winner, pinned ceiling 8,
`supports_pkey` point-lookup plan pinned). Net tick wall-time is a wash:
cold within 0.1ms on ~100ms, warm streamed ~1-2% slower. The proven
benefit is temp-size reduction only (1048576 vs 3670016 bytes, every
round). The p95-remedy claim does NOT stand on this corpus: this change is
a temp-size reduction, not a demonstrated tick-time win. The fixture lane
authorized the shape on single-evaluation 1007 ms vs CTAS 1542 ms; the live
wall-time above supersedes it for any tick-time claim. Proven by
`TestSupplyChainImpactWinnersKeyedStreamStaysWithinBufferBudgetLive` and
`TestSupplyChainImpactWinnersKeyedStreamWallTimeLive`.

No-Regression Evidence: the streamed select returns exactly the legacy
select over winner choice, `source_count`, evidence ids, jsonb-null array
semantics, every cvss bucket, operator facts, and the assoc/class joins
(`EXCEPT ALL` 0 rows both directions), with identical all-NULL extraction
and identical 22023 abort on a planted non-object payload row. Proven by
`TestSupplyChainImpactWinnersKeyedStreamBodyMatchesLegacySelectLive` and
`TestSupplyChainImpactWinnersKeyedStreamScalarRowParityLive`, plus the
unchanged keyed parity, deadlock, settle-skip, fanout, horizon, and audit
suites.

No-Observability-Change: no phase renamed (`build_active_facts` covers the
same phase), no new metric, span, log, or gate; the telemetry page is
unchanged.
