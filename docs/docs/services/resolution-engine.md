# Resolution Engine (Reducer)

## Role and Purpose

The Resolution Engine is a **long-running Deployment** that processes
cross-domain reducer intents and shared projection work. It takes the
source-local facts produced by the Ingester/Bootstrap-Index and materializes
canonical cross-repo relationships in the configured graph backend.

**Binary**: `/usr/local/bin/eshu-reducer`
**Docker service name**: `resolution-engine`
**Kubernetes shape**: Deployment
**Source**: `go/cmd/reducer/`, `go/internal/reducer/`

## Workflow

```text
1. Initialize telemetry
2. Open Postgres (InstrumentedDB) and graph backend (InstrumentedExecutor)
   connections
3. Build reducer service with:
   - DefaultRuntime executor (domain-specific handlers)
   - SharedProjectionRunner plus dedicated code-call and repo-dependency
     projection runners
   - ReducerQueue (Postgres-backed work queue)
   - GraphProjectionPhaseRepairer for exact readiness publication repair
4. reducer.Service.Run() — two concurrent loops:

   Main reducer loop                  Shared and dedicated projection runners
   ─────────────────                  ──────────────────────────────────────
   loop forever:                      loop forever:
     claim up to batch size             acquire partition or domain lease
     dispatch to configured workers     read pending intents
     execute domain handlers            filter by accepted generation/readiness
     heartbeat long-running work        retract/write edges through Cypher
     ack, retry, or fail intents        mark processed intents
     if no work: sleep 1s               if no work: sleep configured interval
```

## Intent Domains

The default reducer runtime processes these implemented intent domains. The
registry also reserves source-neutral domains such as `data_lineage`,
`ownership`, and `governance`, but they are not wired as default handlers in
the current runtime.

| Domain | Handler | Purpose |
|--------|---------|---------|
| `workload_identity` | PostgresWorkloadIdentityWriter | Resolve canonical workload identity |
| `deployable_unit_correlation` | DeployableUnitCorrelationHandler | Correlate deployable-unit candidates before workload admission |
| `cloud_asset_resolution` | PostgresCloudAssetResolutionWriter | Resolve cloud asset identity |
| `deployment_mapping` | (registered handler) | Resolve deployment relationships |
| `workload_materialization` | WorkloadMaterializer (Cypher) | Materialize workload graph nodes |
| `code_call_materialization` | CodeCallMaterializationHandler | Emit durable code-call shared projection intents |
| `semantic_entity_materialization` | SemanticEntityMaterializationHandler | Enrich source-local semantic entity nodes |
| `sql_relationship_materialization` | SQLRelationshipMaterializationHandler | Materialize SQL relationship edges |
| `inheritance_materialization` | InheritanceMaterializationHandler | Materialize inheritance, override, and alias edges |

## Shared Projection

Shared projection is split by ownership:

- `SharedProjectionRunner` processes partitioned canonical edge domains.
- `CodeCallProjectionRunner` owns the high-volume `code_calls` lane.
- `RepoDependencyProjectionRunner` owns source-repo dependency projection.

The partitioned shared runner handles these canonical edge domains:

| Edge Type | Cypher Operation |
|-----------|-----------------|
| `platform_infra` | MERGE infrastructure-platform relationships |
| `workload_dependency` | MERGE workload dependency edges |
| `sql_relationships` | MERGE SQL relationship edges |
| `inheritance_edges` | MERGE inheritance, override, and alias edges |

Processing model:

- **Partitions**: 8 by default (`ESHU_SHARED_PROJECTION_PARTITION_COUNT`)
- **Lease-based**: Each partition acquired via time-limited lease (60s TTL)
- **Batch reads**: Up to 100 intents per partition per cycle
- **Generation filtering**: Only processes intents for accepted generations

## Concurrency Model

- **Main reducer loop**: Concurrent by default. NornicDB uses
  `min(NumCPU, 8)` workers and a claim window equal to workers; Neo4j uses
  `min(NumCPU, 4)` workers and a larger bounded claim window.
- **SharedProjectionRunner**: Sequential by default (Workers=1). Supports
  concurrent partition workers via `ESHU_SHARED_PROJECTION_WORKERS`.
- **Both loops run as concurrent goroutines** within `Service.Run()`.
- **Conflict fencing**: reducer queue rows carry `conflict_domain` and
  `conflict_key`; claim SQL fences only rows sharing the active durable
  conflict key so unrelated repos and graph families can still overlap.

## Backing Stores

| Store | Usage |
|-------|-------|
| Postgres | Intent queue (claim/ack/fail), shared intent store, fact lookups, workload/cloud asset writes |
| Graph backend | Canonical edge writes, workload/infrastructure materialization, and other Cypher graph writes |

## Configuration

| Env Var | Default | Purpose |
|---------|---------|---------|
| `ESHU_REDUCER_RETRY_DELAY` | 30s | Retry delay for failed intents |
| `ESHU_REDUCER_MAX_ATTEMPTS` | 3 | Max retry attempts |
| `ESHU_REDUCER_WORKERS` | Neo4j: `min(NumCPU, 4)`; NornicDB: `min(NumCPU, 8)` | Concurrent reducer intent workers |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | Neo4j: `workers * 4` capped at `64`; NornicDB: `workers` | Reducer intents leased per claim cycle |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | NornicDB: `1`; otherwise disabled | Concurrent semantic entity materialization claims after source-local drain |
| `ESHU_SHARED_PROJECTION_WORKERS` | 1 | Concurrent shared projection workers |
| `ESHU_SHARED_PROJECTION_PARTITION_COUNT` | 8 | Number of partitions |
| `ESHU_SHARED_PROJECTION_POLL_INTERVAL` | 5s | Poll interval when idle |
| `ESHU_SHARED_PROJECTION_LEASE_TTL` | 60s | Partition lease duration |
| `ESHU_SHARED_PROJECTION_BATCH_LIMIT` | 100 | Max intents per partition read |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | 250000 | Max code-call shared intents scanned or loaded for one accepted repo/run before failing safely instead of projecting partial CALLS truth |

Increase `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` only after a reducer
cycle reports the explicit acceptance-cap failure and discovery evidence shows
the repo is dominated by authored source that should remain indexed. Do not use
it for graph write deadlines, slow canonical phases, or ordinary queue backlog;
those indicate different reducer, discovery, or graph-write bottlenecks.

## Telemetry

| Signal | Instruments |
|--------|-------------|
| Spans | `reducer.run` (per intent), `canonical.write` (per partition cycle) |
| Histograms | `eshu_dp_reducer_run_duration_seconds`, `eshu_dp_canonical_write_duration_seconds`, `eshu_dp_queue_claim_duration_seconds{queue=reducer}` |
| Counters | `eshu_dp_reducer_executions_total` (domain + status attrs), `eshu_dp_shared_projection_cycles_total` |
| Logs | `reducer execution succeeded/failed` (domain, intent_id, worker_id), `shared projection cycle completed` |

---

## Current Follow-Ups

The old single-worker reducer and single-item claim notes are historical. The
current runtime has concurrent reducer workers, batch claims, durable conflict
keys, queue blockage reporting, shared projection telemetry, and dedicated
code-call and repo-dependency runners.

Keep future reducer work evidence-led:

- Tune `ESHU_SHARED_PROJECTION_WORKERS` only when shared projection wait or
  processing telemetry shows that the runner is the bottleneck.
- Raise reducer workers or add reducer pods only after queue blockage,
  handler-duration, retry, and graph-write telemetry show safe independent
  work remains.
- Keep NornicDB PR #136 release/pin status explicit before treating the
  semantic hot path as release-backed.
- Continue storage/query maintainability work in `go/internal/storage/cypher`
  and query packages without changing reducer scheduling unless new evidence
  names a reducer bottleneck.
