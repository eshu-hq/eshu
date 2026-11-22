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

## Proof after the shared constructor

Performance Evidence: the change touches the gate's seed stand-in and the shared
`inventory.NewBackfiller` constructor, not a production hot path. The measured subject is
the stand-in's backfill on the full gate corpus, run seed-only (the script stopped after
the seed phase) on the CI-pinned `eshu-deploy` compose stack with Neo4j and PostgreSQL 18.
Input shape: 800 scopes, 150,000 nodes per label, 150,000 IaC facts, 2,500,000 shared
intents, 20,000 finding facts. Result: the backfill derived 1,050,000 rows over 201
repositories in 57.5 s (the throwaway reducer run recorded 87.3 s in its own backfill on a
busier host; the host load was not comparable and no speedup is claimed). `VACUUM
(ANALYZE)` then took 1 m 30.6 s. The derive path (`Backfiller.Run`) is unchanged.

No-Regression Evidence: the seed-only run exited 0. After it, `infra_resource_entity_backfill_markers`
holds `infra_resource_entities_v2`, `infra_resource_entities` holds 1,050,000 rows over 201
repositories, `infra_resource_entity_dirty_repos` is empty (the stand-in returns an error
unless `infraReadModelState.ready()` holds), and `fact_work_items` is untouched (67,201 rows,
0 `superseded`). The committed work-budget tables do not change. No two-leg rerun was done
because the derive path and the tables are unchanged.

No-Observability-Change: no metric, span, log key or status field is added to a runtime
service. The gate's seed log line changes its wording only. The reducer's existing
`eshu_dp_infra_inventory_backfill_runs_total` counter is now asserted by a live test.

Live tests, on a disposable PostgreSQL 18.6: `go test ./internal/storage/postgres/infra/inventory/`
(ok, 311.7 s), `./cmd/read-api-latency-gate/` and the whole `./cmd/reducer/` package (ok, 53.6 s)
all pass, and the new reducer live test fails against both a no-op adapter (no marker) and an
adapter that calls `Backfiller.Run` directly (no `runs_total` series).
