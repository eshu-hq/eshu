# Read API Latency Gate: Seed Notes

Two steps in the [read API latency gate](read-api-latency-gate.md)'s seed phase
change what the sweep measures. Each is labelled in the seed log.

## Infra read model backfill (stand-in)

The seed phase runs the product's
`inventory.Backfiller` after the content seeds and before any core starts, then
requires the infra read model to be installed, marked, and free of dirty
repositories. The gate does this because it starts no reducer, and since
[#11210](https://github.com/eshu-hq/eshu/pull/11210) the reducer owns the
backfill (it runs the same `Backfiller.Run`). Both legs' latency reports carry
`infra_backfill: gate_stand_in` in their identity block. The infra route numbers
therefore describe an install that holds the marker, and a regression in the
reducer's wiring would not show here. Starting the reducer and dropping the
stand-in is [#11226](https://github.com/eshu-hq/eshu/issues/11226).
`awaitInfraReadModel` and the truth-basis check still run unchanged as the guard
on the same outcome.

## VACUUM (ANALYZE) after the bulk seed

The seed ends with one statement, `VACUUM (ANALYZE)`, over every table it
bulk-writes: the seven relational tables the seed counts back
(`ingestion_scopes`, `scope_generations`, `fact_work_items`, `fact_records`,
`shared_projection_intents`, `content_entities`, `content_files`) and the two
read-model tables it fills first, `infra_resource_entities` (the backfill
stand-in) and `supply_chain_impact_canonical_winners` (the winners rebuild). It
runs after the last bulk Postgres write and before the sweep, and never as
`VACUUM FULL`. Tests pin the statement, the table list, the call order and that a
failure fails the seed.

The reason is the visibility map. The entity search walk's second arm is an Index
Only Scan over the first 1,000 index entries, and it visits the heap for every row
whose page is not marked all-visible. Autovacuum sets those bits asynchronously
after the bulk load, so before this step the walk's cost depended on how far
autovacuum had got when the sweep began: on one seeded database, 754 heap fetches
and 512 buffers as seeded, and 0 heap fetches and 32 buffers after a `VACUUM`,
with the same plan. That is the spread (396, 400, 642, 884 and 889 blocks across
five separately seeded runs) the entity row of the work tables could not hold
([#11224](https://github.com/eshu-hq/eshu/issues/11224)). A production database is
vacuumed in steady state, so the gate measures that state on purpose. The raw
`EXPLAIN (ANALYZE, BUFFERS)` evidence is in
`docs/internal/evidence/11170-latency-gate-core-leg.md`.
