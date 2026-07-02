# Resolution Engine

Use this page for the reducer runtime boundary, concurrency model, and operator
signals. Helm lane values live in
[Helm Runtime Values](../deploy/kubernetes/helm-runtime-values.md).

The resolution engine claims durable work, runs reducer domains, drains shared
projection lanes, writes canonical graph/read-model truth through configured
storage ports, and records retry, failure, and completion state.

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
  -> start bounded generation-retention cleanup runner
  -> start bounded graph orphan cleanup runner
```

The main loop claims reducer intents, dispatches workers, heartbeats
long-running work, and acks, retries, or fails work. Shared and dedicated
projection runners acquire leases, wait for accepted generation/readiness
state, write or retract edges, and mark intents processed.
Claim-path performance and correctness changes are gated by the
[Reducer Claim-Latency Gate](../reference/reducer-claim-latency-gate.md).
Code graph conflict-key changes must satisfy the
[Code-Graph Sub-Scope Partitioning](../reference/code-graph-subscope-partitioning.md)
contract before claiming intra-repo reducer concurrency.

The generation-retention runner prunes superseded source-generation history in
bounded Postgres transactions. It never retracts graph truth; relationship
retraction and graph orphan cleanup remain separate reducer work. Retention
events store safe hashes for scope and generation identifiers so changed-since
requests can return `retention_expired` instead of a false zero delta after
history ages out. Production Helm renders and default/production binaries reject
`ESHU_GENERATION_RETENTION_ENABLED=false`; use that disable flag only with an
explicit local `ESHU_QUERY_PROFILE` for local or test binary runs.

The graph orphan cleanup runner counts, marks, and deletes only aged
zero-relationship graph nodes in the closed cleanup label set. It is not a
substitute for relationship retraction or canonical node replacement: first it
marks disconnected nodes, then it deletes only nodes that remain disconnected
after the configured TTL, and it clears the marker when a relationship returns.
Repository cleanup excludes source-local canonical repository nodes, so an empty
but active repository is not treated as an orphan. Each cleanup cycle first
claims a single Postgres partition lease, so scaled reducer deployments do not
run duplicate label-wide graph mutations at the same time.

## Domains And Projection

The default runtime processes workload identity, deployable-unit correlation,
cloud asset resolution, deployment mapping, workload materialization, code-call
materialization, semantic entity materialization, SQL relationships,
inheritance, Terraform config-vs-state drift, package source correlation,
container image identity, CI/CD run correlation, SBOM/attestation attachment,
supply-chain impact, and AWS runtime drift domains.

Shared projection is split across:

- `SharedProjectionRunner` for partitioned canonical edge domains
- `CodeCallProjectionRunner` for high-volume `code_calls`
- `RepoDependencyProjectionRunner` for source-repo dependency projection

The partitioned runner handles `platform_infra`, `workload_dependency`,
`sql_relationships`, and `inheritance_edges`.

## Concurrency Model

- The main reducer loop is concurrent by default. NornicDB uses `NumCPU`
  workers and a claim window equal to workers. Neo4j uses `min(NumCPU, 4)`
  workers and a larger bounded claim window.
- `SharedProjectionRunner` uses up to `min(NumCPU, 4)` partition workers by
  default. Tune `ESHU_SHARED_PROJECTION_WORKERS` only when telemetry proves
  shared projection is the bottleneck.
- The main loop, shared projection runner, code-call runner, and
  repo-dependency runner run as concurrent goroutines inside `Service.Run()`.
- `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING` is a diagnostic-only switch
  for bounded proof runs. Leave it off for normal operation; when enabled it
  splits repo-dependency retract execution into timed
  `repository_relationship_edges`, `runs_on_relationships`, and
  `evidence_artifacts` roles so logs show which cleanup owns the cost.
- The generation-retention runner runs beside those loops and relies on
  Postgres row locks plus bounded batch and row limits. Do not reduce reducer
  worker counts to make retention safe.
- The graph orphan cleanup runner runs beside those loops with bounded per-label
  graph writes and no reducer worker-count change. Do not use lower worker
  counts or batch-size `1` as a substitute for idempotent relationship
  retraction.
- Queue rows carry `conflict_domain` and `conflict_key`; claim SQL fences only
  rows sharing an active durable conflict key so unrelated work can overlap.

Do not lower worker counts as a shipped fix for non-idempotent writes or graph
MERGE races. Diagnose the conflict key, retry, and write path.

## Configuration

Important env vars:

- `ESHU_REDUCER_RETRY_DELAY`
- `ESHU_REDUCER_MAX_ATTEMPTS`
- `ESHU_REDUCER_WORKERS`
- `ESHU_REDUCER_BATCH_CLAIM_SIZE`
- `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT`
- `ESHU_SHARED_PROJECTION_WORKERS`
- `ESHU_SHARED_PROJECTION_PARTITION_COUNT`
- `ESHU_SHARED_PROJECTION_POLL_INTERVAL`
- `ESHU_SHARED_PROJECTION_LEASE_TTL`
- `ESHU_SHARED_PROJECTION_BATCH_LIMIT`
- `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT`
- `ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT`
- `ESHU_CODE_CALL_PROJECTION_WORKERS`
- `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING`
- `ESHU_GENERATION_RETENTION_ENABLED`
- `ESHU_GENERATION_RETENTION_POLL_INTERVAL`
- `ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS`
- `ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE`
- `ESHU_GENERATION_RETENTION_BATCH_GENERATION_LIMIT`
- `ESHU_GENERATION_RETENTION_BATCH_ROW_LIMIT`
- `ESHU_GRAPH_ORPHAN_SWEEP_ENABLED`
- `ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL`
- `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER`
- `ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL`
- `ESHU_GRAPH_ORPHAN_SWEEP_TTL`
- `ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT`
- `ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT`
- `ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT`
- `ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED`

`ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED` defaults to `true` and gates
the symbol→runtime edges on their target having committed, so they cannot drop on
a cold first generation: `Function-[:HANDLES_ROUTE]->Endpoint` on its target
`(repo_id, path)` `:Endpoint` (#2809), and `Function-[:RUNS_IN]->Workload` on the
repo having a committed `:Workload` (#2855). Both share one presence store and
this one flag. It is independent of
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`: the secrets/IAM flag never
enables or disables these gates, and this kill switch never widens uid presence
writes onto the cloud/Kubernetes materializers. Set it to a false value to restore
the pre-gate behavior. A handler whose target never materializes (a route with no
endpoint, or a repo with no workload) is drained with no edge
(`terminal_no_endpoint` with its `domain` in the shared projection cycle log),
never deferred, so the backlog cannot stall.

Raise `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` only after the reducer
reports the explicit acceptance-cap failure and discovery evidence shows the
repo is dominated by authored source that should remain indexed. Do not use it
for graph write deadlines, slow canonical phases, or ordinary backlog.

## Telemetry

Start with:

- spans: `reducer.run`, `canonical.write`
- histograms: `eshu_dp_reducer_run_duration_seconds`,
  `eshu_dp_canonical_write_duration_seconds`,
  `eshu_dp_queue_claim_duration_seconds{queue=reducer}`
- counters: `eshu_dp_reducer_executions_total`,
  `eshu_dp_shared_projection_cycles_total`,
  `eshu_dp_generation_retention_generations_pruned_total`,
  `eshu_dp_generation_retention_rows_pruned_total`,
  `eshu_dp_generation_retention_failures_total`,
  `eshu_dp_generation_retention_skipped_total`
- retention histograms: `eshu_dp_generation_retention_duration_seconds`,
  `eshu_dp_generation_retention_batch_size`,
  `eshu_dp_generation_retention_oldest_eligible_age_seconds`
- graph cleanup gauge: `eshu_dp_graph_orphan_nodes`
- logs: reducer execution result logs and shared projection cycle logs with
  domain, worker, route, row count, and failure class

## Related Docs

- [Service Runtimes](../deployment/service-runtimes.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
- [Reducer Claim-Latency Gate](../reference/reducer-claim-latency-gate.md)
- [Code-Graph Sub-Scope Partitioning](../reference/code-graph-subscope-partitioning.md)
- [Telemetry Overview](../reference/telemetry/index.md)
- [Local Testing](../reference/local-testing.md)
