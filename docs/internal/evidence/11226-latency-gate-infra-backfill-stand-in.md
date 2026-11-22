# #11226: the latency gate's infra backfill is a permanent stand-in

Refs #11226, #11170, #10860, #11196.

This note records why the latency gate (`scripts/verify-read-api-latency-gate.sh`)
runs the infra read model backfill itself, and how parity with the reducer is kept.
Arbiter ruling `arbiter-11226`: option C, amended.

## Decision

#11196 (reducer-owned infra backfill) is merged on `origin/main` as #11210. The gate
starts no reducer (it runs the core, the satellite and the MCP server, all built from
the worktree) and records the marker itself, labelled `infra_backfill: gate_stand_in`.
An earlier plan was to start the in-tree reducer and drop the stand-in (#11226). A
throwaway run showed a real reducer cannot run on the gate's seeded corpus, so the
arbiter ruled the stand-in permanent and made parity with the product structural: the
stand-in builds its backfiller with `inventory.NewBackfiller`, the constructor
`RunBackground` uses, and the proof that the reducer records the marker moved to a
reducer live test (`TestInfraInventoryBackfillRunnerRecordsTheMarkerLive` in
`go/cmd/reducer`).

## The throwaway reducer run

A full-corpus gate seed was kept up after the seed phase (`98db9e39bd`, seed plus the
stand-in, host io pressure high and shared). The baseline corpus was snapshotted
(counts of `fact_work_items` by stage, domain and status, `shared_projection_intents`,
`scope_generations`, `ingestion_scopes`, `fact_records`, `content_entities`,
`content_files`, the infra rows and the winners). The marker row was deleted, then
the real `eshu-reducer` (built from the checkout with `go build ./cmd/reducer`) was
started with every restriction the reducer offers:

```text
ESHU_REDUCER_CLAIM_DOMAINS=data_lineage      # a domain the seed does not use
ESHU_REDUCER_WORKERS=1
ESHU_GENERATION_RETENTION_ENABLED=false  ESHU_GENERATION_LIVENESS_ENABLED=false
ESHU_SECRET_LINES_REPAIR_ENABLED=false   ESHU_STATUS_SUMMARY_WRITER_ENABLED=false
ESHU_INFRA_INVENTORY_RECONCILE_ENABLED=false
ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED=false
ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED=false
ESHU_QUERY_PROFILE=local_lightweight         # required: the reducer refuses retention=false otherwise
```

Those variables were the experiment's restrictions and appear in no committed gate.

| Observation | Result |
| --- | --- |
| Marker | recorded 24 s after start (runner duration 22.4 s, 201 repositories, 1,050,000 rows) |
| `fact_work_items` | 7,680 rows set to `superseded` by the reducer's stale-generation sweep (5,000 then 2,680 rows), 2,560 in each of the three seeded domains, all in the 320 seeded superseded generations; per domain `dead_letter` 2,240 to 1,600, `pending` 3,360 to 2,400, `retrying` 3,360 to 2,400 |
| Config-artifact walker | ran its own cycle, `derived=45` |
| Idle load | about 45 statements per second (1,407 commits in 30 s), against the 2 per second limit `prepareWorkMeter` enforces |
| Everything else in the snapshot | identical: 2,500,000 shared intents with 25,000 pending (the shared-projection runner logged that it was leaving them pending), `scope_generations`, `ingestion_scopes`, `fact_records`, `content_entities`, `content_files`, the infra rows and the winners |

## Why the other options were rejected

- Running the reducer between two seed stages: stage 1 would only be clean because
  `fact_work_items` is still empty, which is luck, not a guarantee, and the walker and
  the polling remain.
- A product switch that runs the reducer's backfill runner alone: a product change
  whose only user is a test, with partial wiring coverage.
- Restoring the work-item statuses after a reducer run: it hides a mutation.
