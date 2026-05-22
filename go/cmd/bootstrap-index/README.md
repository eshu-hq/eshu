# bootstrap-index

`eshu-bootstrap-index` seeds an empty or recovered Eshu environment. It collects
a finite repository set, commits facts, runs source-local projection, and drives
the post-collection passes that let the steady-state reducer finish
cross-repository truth.

Use this README when changing `go/cmd/bootstrap-index/`. Use the public
[Bootstrap Index service page](../../../docs/public/services/bootstrap-index.md)
when operating it.

## Runtime Role

Bootstrap is a one-shot runtime:

- `eshu-ingester` owns ongoing repository sync, parsing, and fact emission.
- `eshu-reducer` owns queued cross-domain materialization and repair.
- `eshu-bootstrap-index` owns the first finite pass, then exits.

It is packaged for Compose and direct process runs. It is not a steady-state
Kubernetes workload in the public Helm chart. Repeated restarts or long-running
bootstrap activity are incidents.

## Pipeline Contract

The `runPipelined` ordering is the correctness contract:

```text
open telemetry, Postgres, schema, graph writer
build collector and projector
collect repositories while projector drains source-local queue
backfill relationship evidence
wait for projector drain
materialize IaC reachability
reopen deployment_mapping
enqueue config_state_drift
exit
```

| Step | Why it matters |
| --- | --- |
| Collection plus source-local projection | Small repositories project while larger repositories are still collecting. |
| `BackfillAllRelationshipEvidence` | Publishes backward evidence so deployment mapping can create complete cross-repository truth. |
| Projector drain wait | Work emitted after reopen would otherwise miss the replay window. |
| `MaterializeIaCReachability` | Writes active-generation IaC usage rows for query and reducer paths. |
| `ReopenDeploymentMappingWorkItems` | Replays deployment mapping rows that ran before relationship evidence was available. |
| `EnqueueConfigStateDriftIntents` | Enqueues one `config_state_drift` intent per active `state_snapshot:*` scope. |

Do not reorder these calls. Any reducer domain that consumes
`resolved_relationships` needs a reopen or re-trigger mechanism after
deployment mapping reopens.

## Collection And Projection

`buildBootstrapCollector` wires the Git source, repository selector, and
snapshotter. It commits each collected generation with
`SkipRelationshipBackfill=true`; corpus-wide backfill runs later to avoid
quadratic per-repository backfill cost.

`buildBootstrapProjector` wires a Postgres-backed projector queue scoped to Git
source systems. Projection workers claim rows with `FOR UPDATE SKIP LOCKED`,
load facts, run `projector.Runtime`, ack success, and enqueue reducer intents.

`drainingWorkSource` exits projection after collection finishes and the queue is
empty for five consecutive polls. Any real claim resets that counter.

`ESHU_PROJECTION_WORKERS` controls bootstrap projection concurrency. The default
is `min(runtime.NumCPU(), 8)`. `1` is acceptable for comparison runs, not as a
shipped fix for race or idempotency bugs.

## Lease And Supersession

Bootstrap projection follows the steady-state projector rules:

- `ProjectorQueue.Heartbeat` renews long-running claims.
- Heartbeat interval is `leaseDuration / 3`, capped at one minute.
- Stale claim rejection fails the worker item.
- `projector.ErrWorkSuperseded` records `status=superseded`, skips stale graph
  ack, and returns to the claim loop.

## Graph Writer

Bootstrap uses the same canonical writer contract as source-local ingestion:

- Neo4j and NornicDB use `sourcecypher.NewCanonicalNodeWriter`.
- Backend-specific behavior stays in executor wiring.
- NornicDB uses bounded phase-group execution by default.
- `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` is a conformance switch, not the
  production default.
- Row-scoped batched entity containment is enabled for NornicDB so bootstrap and
  steady-state ingestion share the high-cardinality write shape.

Update [NornicDB tuning](../../../docs/public/reference/nornicdb-tuning.md)
when changing NornicDB batch, phase-group, grouped-write, timeout, or
containment knobs.

## Public Contract

- `--version` and `-v` print build version before opening telemetry, Postgres,
  or graph connections.
- Exit code `0` means every bootstrap step completed.
- Exit code `1` means collection, projection, schema bootstrap, graph setup, or
  a post-collection pass failed.
- The binary does not mount `/healthz`, `/readyz`, `/metrics`, or
  `/admin/status`.
- `ESHU_PPROF_ADDR` enables opt-in local pprof through `runtime.NewPprofServer`.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_POSTGRES_DSN` | required | Postgres connection string. |
| `ESHU_GRAPH_BACKEND` | `nornicdb` | `nornicdb` or `neo4j`; invalid values fail startup. |
| `NEO4J_URI` | required | Bolt URI for NornicDB or Neo4j. |
| `NEO4J_USERNAME` | required | Bolt username. |
| `NEO4J_PASSWORD` | required | Bolt password. |
| `DEFAULT_DATABASE` | `nornic` | Bolt database name. |
| `ESHU_PROJECTION_WORKERS` | `min(NumCPU, 8)` | Bootstrap projection worker count. |
| `ESHU_DISCOVERY_REPORT` | unset | Write one discovery advisory JSON array. |
| `ESHU_PPROF_ADDR` | unset | Enable pprof; bare ports bind to `127.0.0.1`. |
| `ESHU_NEO4J_BATCH_SIZE` | backend default | Generic canonical writer batch size. |
| `ESHU_NEO4J_PROFILE_GROUP_STATEMENTS` | `false` | Neo4j grouped-write timing diagnostics. |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | `30s` on NornicDB | Graph write transaction timeout. |
| `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS` | `500` | Broad NornicDB grouped-statement cap. |
| `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` | `5` | File-phase grouped-statement cap. |
| `ESHU_NORNICDB_FILE_BATCH_SIZE` | `100` | File upsert row cap. |
| `ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS` | `25` | Entity grouped-statement cap before label overrides. |
| `ESHU_NORNICDB_ENTITY_BATCH_SIZE` | `100` | Entity row cap before label overrides. |
| `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES` | `Function=15,K8sResource=1,Struct=50,Variable=100` | Label-specific entity row caps. |
| `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS` | `Function=5,K8sResource=1,Struct=15,Variable=5` | Label-specific grouped-statement caps. |
| `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` | `false` | Conformance-only grouped canonical writes on NornicDB. |

Repository selection, snapshot, parse-worker, discovery-overlay, and local data
root settings are shared with the ingester and CLI indexing path. See
[Environment Variables](../../../docs/public/reference/environment-variables.md)
and [CLI Indexing](../../../docs/public/reference/cli-indexing.md).

## Telemetry

Bootstrap uses normal OpenTelemetry providers but does not expose the shared
Prometheus HTTP endpoint.

Important signals:

- `telemetry.SpanCollectorObserve`
- `telemetry.SpanProjectorRun`
- `eshu_dp_facts_emitted_total`
- `eshu_dp_facts_committed_total`
- `eshu_dp_collector_observe_duration_seconds`
- `eshu_dp_queue_claim_duration_seconds{queue=projector}`
- `eshu_dp_projector_run_duration_seconds`
- `eshu_dp_projections_completed_total`
- `eshu_dp_pipeline_overlap_seconds`
- `eshu_dp_gomemlimit_bytes`
- `eshu_dp_correlation_drift_intents_enqueued_total`

Key failure classes include commit, deferred backfill, IaC reachability,
deployment-mapping reopen, drift enqueue, projection, and lease heartbeat
failures.

## Edge Cases

- Pending or claimed items during the reopen pass naturally see the open gate
  when they run.
- Items that succeed between relationship backfill and reopen are not replayed
  automatically today; use admin replay or incremental refresh.
- `errProjectorDrained` is a clean shutdown sentinel.
- Discovery advisory paths are operator artifacts, not metric labels.
- The binary has no signal cleanup path; future signal handling must define
  partial-phase recovery semantics first.

## Verification

```bash
cd go
go test ./cmd/bootstrap-index -count=1
go run ./cmd/eshu docs verify ../go/cmd/bootstrap-index --limit 1200 \
  --fail-on contradicted,missing_evidence
```

Run performance-evidence gates when changing workers, queues, leases, batching,
graph writes, NornicDB knobs, or hot-path runtime behavior.

## Related Docs

- [Bootstrap Runtime Services](../../../docs/public/deployment/service-runtimes-bootstrap.md)
- [Docker Compose](../../../docs/public/run-locally/docker-compose.md)
- [Architecture](../../../docs/public/architecture.md)
- [Local Testing](../../../docs/public/reference/local-testing.md)
- [Service Workflows](../../../docs/public/reference/service-workflows.md)
