# Resolution Engine

The resolution engine is the long-running reducer runtime. It claims durable
work, runs reducer domains, drains shared projection lanes, writes canonical
graph/read-model truth through the configured storage ports, and records retry,
failure, and completion state.

| Runtime | Value |
| --- | --- |
| Binary | `/usr/local/bin/eshu-reducer` |
| Docker service name | `resolution-engine` |
| Kubernetes shape | `Deployment` |
| Source | `go/cmd/reducer/`, `go/internal/reducer/` |

## Runtime Flow

```text
start telemetry
  -> open Postgres and graph backend
  -> build DefaultRuntime domain handlers
  -> build reducer queue
  -> start main reducer loop
  -> start shared projection, code-call, and repo-dependency runners
```

The main reducer loop claims reducer intents, dispatches configured workers,
heartbeats long-running work, and acks, retries, or fails work. Shared and
dedicated projection runners acquire leases, load pending intents, wait for
accepted generation/readiness state, write or retract edges, and mark intents
processed.

## Intent Domains

The default reducer runtime processes these implemented intent domains. The
registry also reserves source-neutral domains such as `data_lineage`,
`ownership`, and `governance`, but they are not wired as default handlers in
the current runtime.

| Domain | Purpose |
| --- | --- |
| `workload_identity` | Resolve canonical workload identity. |
| `deployable_unit_correlation` | Correlate deployable-unit candidates before workload admission. |
| `cloud_asset_resolution` | Resolve cloud asset identity. |
| `deployment_mapping` | Resolve deployment relationships. |
| `workload_materialization` | Materialize workload graph nodes. |
| `code_call_materialization` | Emit durable code-call shared projection intents. |
| `semantic_entity_materialization` | Enrich source-local semantic entity nodes. |
| `sql_relationship_materialization` | Materialize SQL relationship edges. |
| `inheritance_materialization` | Materialize inheritance, override, and alias edges. |
| `config_state_drift` | Emit bounded Terraform config-vs-state drift counters and logs. |
| `package_source_correlation` | Classify package ownership, publication, and consumption correlations. |
| `container_image_identity` | Join image references into digest-keyed identity decisions. |
| `ci_cd_run_correlation` | Correlate CI/CD runs, artifacts, and environments. |
| `sbom_attestation_attachment` | Attach SBOM and attestation evidence to explicit subject digests. |
| `supply_chain_impact` | Publish reducer-owned vulnerability impact findings. |
| `aws_cloud_runtime_drift` | Publish admitted AWS runtime orphan and unmanaged drift findings as durable reducer facts. |

## Shared Projection

Shared projection is split by ownership:

| Runner | Owns |
| --- | --- |
| `SharedProjectionRunner` | Partitioned canonical edge domains. |
| `CodeCallProjectionRunner` | High-volume `code_calls` lane. |
| `RepoDependencyProjectionRunner` | Source-repo dependency projection. |

The partitioned shared runner handles `platform_infra`,
`workload_dependency`, `sql_relationships`, and `inheritance_edges`. It uses
partition leases, accepted-generation filtering, readiness checks, and bounded
batch reads so independent work can overlap without serializing the reducer.

## Concurrency Model

- The main reducer loop is concurrent by default. NornicDB uses `NumCPU`
  workers and a claim window equal to workers. Neo4j uses `min(NumCPU, 4)`
  workers and a larger bounded claim window.
- `SharedProjectionRunner` uses up to `min(NumCPU, 4)` partition workers by
  default. Tune `ESHU_SHARED_PROJECTION_WORKERS` only when shared projection
  telemetry proves that partition work is the bottleneck.
- The main loop, shared projection runner, code-call runner, and
  repo-dependency runner run as concurrent goroutines inside `Service.Run()`.
- Reducer queue rows carry `conflict_domain` and `conflict_key`; claim SQL
  fences only rows sharing the active durable conflict key so unrelated repos
  and graph families can still overlap.

## Configuration

| Env var | Default | Purpose |
| --- | --- | --- |
| `ESHU_REDUCER_RETRY_DELAY` | `30s` | Retry delay for failed intents. |
| `ESHU_REDUCER_MAX_ATTEMPTS` | `3` | Max retry attempts. |
| `ESHU_REDUCER_WORKERS` | Neo4j: `min(NumCPU, 4)`; NornicDB: `NumCPU` | Concurrent reducer intent workers. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | Neo4j: `workers * 4` capped at `64`; NornicDB: `workers` | Reducer intents leased per claim cycle. |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | NornicDB: `1`; otherwise disabled | Concurrent semantic entity materialization claims after source-local drain. |
| `ESHU_SHARED_PROJECTION_WORKERS` | `min(NumCPU, 4)` | Concurrent shared projection workers. |
| `ESHU_SHARED_PROJECTION_PARTITION_COUNT` | `8` | Number of partitions. |
| `ESHU_SHARED_PROJECTION_POLL_INTERVAL` | `500ms` | Poll interval when idle. |
| `ESHU_SHARED_PROJECTION_LEASE_TTL` | `60s` | Partition lease duration. |
| `ESHU_SHARED_PROJECTION_BATCH_LIMIT` | `100` | Max intents per partition read. |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` | Max code-call shared intents scanned or loaded for one accepted repo/run before failing safely instead of projecting partial CALLS truth. |

Increase `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` only after a reducer
cycle reports the explicit acceptance-cap failure and discovery evidence shows
the repo is dominated by authored source that should remain indexed. Do not use
it for graph write deadlines, slow canonical phases, or ordinary queue backlog.

## Telemetry

| Signal | Instruments |
| --- | --- |
| Spans | `reducer.run`, `canonical.write` |
| Histograms | `eshu_dp_reducer_run_duration_seconds`, `eshu_dp_canonical_write_duration_seconds`, `eshu_dp_queue_claim_duration_seconds{queue=reducer}` |
| Counters | `eshu_dp_reducer_executions_total`, `eshu_dp_shared_projection_cycles_total` |
| Logs | Reducer execution result logs and shared projection cycle logs with domain, worker, route, row count, and failure class. |

## Related Docs

- [Service Runtimes](../deployment/service-runtimes.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Local Testing](../reference/local-testing.md)
