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
each, the Leg-B QA fanout shape) against disposable PostgreSQL 18, the
narrow temp measured 1,048,576 bytes against the wide temp's 3,670,016
bytes, and the single-evaluation streamed select measured 8,000 shared
buffers against the legacy select's 3,200 at a 100% win rate (5.0 buffers
per emitted winner, pinned ceiling 8, `supports_pkey` point-lookup plan
pinned). The fixture lane authorized the shape on single-evaluation 1007 ms
vs CTAS 1542 ms. Proven by
`TestSupplyChainImpactWinnersKeyedStreamStaysWithinBufferBudgetLive`.

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
