# #11466 enrichment first-carry stamp: RED-half evidence

The enrichment-arrival-to-rescore chain stamps each winners canonical key
with the first instant its winners-visible row carried each enrichment
value (`supply_chain_impact_winners_enrichment_first_carry`, migration
359), claimed set-once per value by one `INSERT ... SELECT ... ON CONFLICT
DO UPDATE` restricted to `value_identity IS DISTINCT`, inside the resweep
transaction after the rebuild (full path and over-cap fallback) or the
keyed apply (keyed path, identifier-set-restricted).

## Failing-first record

RED (pre-stamp main, same test files): the full resweep reconciles the
winner (`inserted=1`) but the stamp read fails with `relation
"supply_chain_impact_winners_enrichment_first_carry" does not exist`, and
the QA-shaped lag join fails the same way: the lag is UNMEASURABLE, not
zero. GREEN (this lane): all six proof passes plus the concurrency proof
pass on disposable PostgreSQL 18 with the real finding build, the real
writer, and the real full and keyed resweeps.

## Resweep cost (wall time logged, not gated)

Seeded 6-winner store, disposable PostgreSQL 18, same host as the proof:

- full resweep: 4-9 ms per tick (rebuild + carry over all winners);
- keyed resweep: ~15 ms per tick (1 identifier of 6 stored winners).

The carry adds one set-based statement per tick and writes only on value
change: rewrites and same-value re-ingests resolve the same identity, the
`IS DISTINCT` guard skips the update, and no row version moves. The QA lane
re-measures on real scale; no budget is set from these numbers.

No-Regression Evidence (#11466): the carry adds one set-based
INSERT...SELECT per resweep tick and writes only when the incoming
value_identity IS DISTINCT FROM the stored one. Baseline is the pre-stamp
tree (RED: the same proof fails on the missing relation, so the tick cost
below is the whole marginal cost of the feature, not a delta against an
unmeasured path). After, on disposable PostgreSQL 18 (local PG 18.6,
seeded 6-winner store, real build plus writer plus resweeps): full ticks
3.7-8.7 ms, keyed tick 14.9 ms for 1 identifier of 6 stored winners, each
logged per tick by the live proof (wall time logged, not gated). Stable
ticks (rewrites, same-value re-ingests) resolve the same identity, so the
conflict guard skips every update and no winner row version moves; the
write-only-changed-rows live proof still passes unchanged. The QA lane
re-measures on real scale before any budget.

No-Observability-Change (#11466): the carry emits no metric of its own.
The keyed carry runs inside the maintainer apply phase timed by the
existing eshu_dp_supply_chain_impact_winners_tick_phase_duration_seconds,
and a claim error fails the resweep through the existing maintainer error
path. Coverage row lives in
docs/public/observability/telemetry-coverage-reducer-domains.md beside the
supply-chain projection rows.

## Cohort rule

`ClassifyEnrichmentCarrySample` admits a sample only with a known source, a
non-empty value identity, a real first-carry, a real ingestion anchor,
first-carry at or after ingest, and ingest at or after the per-source T0.
Buckets are per value (`first_epss:<score_date>`, `cisa_kev:true/false`),
so KEV flips never pool and rescores never pool across score_dates.

## Decisions for arbiter review

- **D1 (KEV-false value_identity):** the stamp records `false` episodes
  (mechanism), while the classifier excludes anchorless samples
  (no-ingest-anchor: a KEV-false-by-default carry has no KEV fact). A flip
  back to `true` moves the stamp observably.
- **D2 (same-value re-ingest):** the stamp never moves (proven live); the
  QA anchor advances to the new generation. In production the re-ingest
  generation lands after the previous resweep, so the persisting value
  classifies as carried-before-ingest and yields no phantom lag (pinned in
  classifier units; the seeded proof inverts the clock shape and pins stamp
  stability plus anchor resolution instead).
- **D3 (overlapping generations):** a re-ingest generation ingested before
  the previous resweep commits stays in-cohort with lag first-carry minus
  the new anchor. The QA lane disposition this shape over >= 2 real cycles.
