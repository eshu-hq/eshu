# Read API Latency Gate: Seed Notes

Three steps in the [read API latency gate](read-api-latency-gate.md)'s seed phase
change what the sweep measures. Each is labelled in the seed log.

## Infra read model backfill (permanent stand-in)

The seed phase runs the product's `inventory.Backfiller` after the content seeds
and before any core starts, then requires the infra read model to be installed,
marked, and free of dirty repositories. The reducer has owned this backfill since
[#11210](https://github.com/eshu-hq/eshu/pull/11210), but a real reducer cannot run
on the gate's seeded corpus. Measured on the full corpus with every restriction the
reducer offers (claims limited to an unseeded domain, retention, liveness and the
other switchable runners off), a reducer started between the seed and the sweep:

- set 7,680 seeded `fact_work_items` rows to `superseded`, in its stale-generation
  sweep, across the 320 superseded generations the gate seeds on purpose;
- ran the config-artifact walker, which derived 45 repositories of its own;
- polled Postgres at about 45 statements per second (1,407 commits in 30 seconds),
  against the gate's idle limit of 2 per second.

It did record the marker, in 22 seconds. So the gate runs the same code through
the shared `inventory.NewBackfiller` constructor, which `RunBackground` also uses,
and labels both legs' reports `infra_backfill: gate_stand_in`. The infra route
numbers describe an install that holds the marker. That the reducer process
records the marker is proved elsewhere, by
`TestInfraInventoryBackfillRunnerRecordsTheMarkerLive` in `go/cmd/reducer`
([#11226](https://github.com/eshu-hq/eshu/issues/11226)).
`awaitInfraReadModel` and the truth-basis check still run as the guard on the same
outcome.

## Supports read model backfill (permanent stand-in)

The seed phase runs the product's `SupplyChainImpactSupportsBackfill` to
completion after the relational count check and before the winners rebuild,
then requires the supports read model to read ready. The reducer has owned this
backfill since the supports marker gated the findings serving route, but a real
reducer cannot run on the gate's seeded corpus. The seeded corpus holds no
finding facts in `fact_records` (a reject trigger refuses the kind), so the
sweep covers no pairs and the verification proves "no live finding fact lacks a
support" vacuously over `fact_records`; what makes that vacuous proof safe is
proved elsewhere, in the same seed: the supports were written by the production
maintainer (`SeedWinnersSupports` through
`WriteSupplyChainImpactFindingRows`), the count check pins the supports rows,
and the stand-in drains the fence, runs the product verify slices, and stamps
only through the fenced completion statement. The marker's `completed_at`
lands before the winners watermark, matching the production order (ready
supports, then winners). The report labels both legs' identity blocks
`supports_backfill: gate_stand_in`. The findings route numbers describe an
install that holds the marker. That the reducer records the marker is proved
elsewhere, by the product live suite for the backfill
(`TestSupplyChainImpactSupportsBackfillLive` in
`go/internal/storage/postgres`); that the stand-in drives this seeded corpus
to ready is proved by the gate's own live test,
`TestBackfillSeededSupportsReadModelDrivesSeededSupportsToReadyLive` in
`go/cmd/read-api-latency-gate`.

## VACUUM (ANALYZE) after the bulk seed

The seed ends with one statement, `VACUUM (ANALYZE)`, over every table it
bulk-writes: the seven relational tables the seed counts back
(`ingestion_scopes`, `scope_generations`, `fact_work_items`, `fact_records`,
`shared_projection_intents`, `content_entities`, `content_files`) and the three
read-model tables it fills first, `supply_chain_impact_finding_supports` (the
supports-backfill stand-in), `infra_resource_entities` (the infra backfill
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
vacuumed in steady state, so the gate measures that state on purpose (#11170).
