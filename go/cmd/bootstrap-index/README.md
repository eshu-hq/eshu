# bootstrap-index

`eshu-bootstrap-index` is the one-shot runtime that seeds an empty or recovered
Eshu environment. It collects a finite repository set, commits facts to
Postgres, runs source-local projection, and drives the post-collection passes
that let the steady-state reducer finish cross-repository truth.

Use this README when changing `go/cmd/bootstrap-index/`. Use the public
[Bootstrap Index service page](../../../docs/public/services/bootstrap-index.md)
when operating it.

## Runtime Role

Bootstrap indexing exists because steady-state services are incremental:

- `eshu-ingester` owns ongoing repository sync, parsing, and fact emission.
- `eshu-reducer` owns queued cross-domain materialization and repair.
- `eshu-bootstrap-index` owns the first finite pass over a known repository
  set, then exits.

The binary is not a steady-state Kubernetes workload in the public Helm chart.
It is packaged for Compose and can run manually as a direct process. Repeated
restarts or long-running bootstrap activity are incidents; normal freshness
belongs to the ingester, workflow coordinator, hosted collectors, and reducer.

## Current Flow

```text
open telemetry
open Postgres
apply Postgres bootstrap schema
open graph writer
build Git collector
build source-local projector
run finite collection and projection
backfill relationship evidence
wait for source-local projector drain
materialize IaC reachability
reopen deployment_mapping reducer work
enqueue config_state_drift reducer work
exit
```

The source-local projector writes canonical graph and content state and emits
reducer intents. After bootstrap exits, `eshu-reducer` drains the reducer
domains that need cross-repository evidence or shared materialization.

## Facts-First Ordering

The ordering in `runPipelined` is a correctness contract.

| Step | What runs | Why it matters |
| --- | --- | --- |
| Collection and source-local projection | `drainCollector` and `drainProjectorPipelined` run concurrently. | Small repositories can project while larger repositories are still being collected. |
| Relationship-evidence backfill | `BackfillAllRelationshipEvidence` populates `relationship_evidence_facts` and publishes `backward_evidence_committed`. | Deployment mapping needs the backward evidence gate before it can create complete cross-repository truth. |
| Projector drain wait | The pipeline waits for the source-local projector goroutine to finish. | Work emitted after a reopen pass would otherwise miss reopening. |
| IaC reachability | `MaterializeIaCReachability` writes active-generation IaC usage rows. | Query and reducer paths need current corpus-wide IaC classification. |
| Deployment mapping reopen | `ReopenDeploymentMappingWorkItems` reopens succeeded `deployment_mapping` rows that ran before the gate was open. | The reducer can re-claim them and create `resolved_relationships`. |
| Drift intent enqueue | `EnqueueConfigStateDriftIntents` enqueues one `config_state_drift` intent per active `state_snapshot:*` scope. | Terraform drift consumes both config-side parser facts and state-side collector facts. |

Do not reorder or merge these calls. Any domain that consumes
`resolved_relationships` needs a reopen or re-trigger mechanism after
deployment mapping reopens.

## Collection And Projection

`buildBootstrapCollector` wires a `collector.GitSource` with the native
repository selector and snapshotter. It commits each collected generation with
`postgres.IngestionStore.SkipRelationshipBackfill=true`. That flag is required:
running relationship backfill per committed repository would turn the bootstrap
cost quadratic across the corpus.

`buildBootstrapProjector` wires a Postgres-backed projector queue scoped to git
source systems. Projection workers claim queue rows with `FOR UPDATE SKIP
LOCKED`, load facts for the claimed generation, run `projector.Runtime`, ack
succeeded work, and emit reducer intents through the reducer queue.

`drainingWorkSource` controls shutdown:

- while collection is still running, empty queue claims sleep briefly and retry
- after collection finishes, five consecutive empty claims end projection
- any real claim resets the empty-poll counter

`ESHU_PROJECTION_WORKERS` controls bootstrap projection concurrency. The default
is `min(runtime.NumCPU(), 8)`. Setting it to `1` is useful for comparison runs,
but it must not be shipped as a fix for a race or idempotency bug.

## Lease And Supersession Rules

Bootstrap projection uses the same stale-work discipline as the steady-state
projector service:

- `ProjectorQueue.Heartbeat` renews long-running claimed work.
- The heartbeat interval is `leaseDuration / 3`, capped at one minute.
- A stale claim rejection fails the worker item.
- `projector.ErrWorkSuperseded` means a newer same-scope generation replaced
  the current work. The worker records `status=superseded`, does not ack stale
  graph state, and returns to the claim loop.

This behavior is covered by bootstrap projector tests and the Postgres
projector queue lifecycle tests.

## Graph Writer

Bootstrap uses the same canonical writer contract as source-local ingestion:

- Neo4j and NornicDB both flow through `sourcecypher.NewCanonicalNodeWriter`.
- Backend-specific differences stay in executor wiring.
- NornicDB uses bounded phase-group execution by default instead of one
  oversized grouped transaction.
- `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` remains a conformance switch, not a
  production default.
- Row-scoped batched entity containment is enabled for NornicDB so bootstrap
  and steady-state ingestion share the same high-cardinality entity write
  shape.

Update [NornicDB tuning](../../../docs/public/reference/nornicdb-tuning.md) if
you change any NornicDB batch, phase-group, grouped-write, timeout, or
containment knob.

## Public Contract

The binary has no importable API. Its public contract is:

- `eshu-bootstrap-index --version` and `-v` print the embedded build version
  before opening telemetry, Postgres, or the graph backend.
- exit code `0` means every bootstrap step completed
- exit code `1` means collection, projection, graph writer setup, schema
  bootstrap, or a post-collection pass failed
- the binary does not mount `/healthz`, `/readyz`, `/metrics`, or
  `/admin/status`
- `ESHU_PPROF_ADDR` enables an opt-in local pprof endpoint through
  `runtime.NewPprofServer`

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_POSTGRES_DSN` | required | Postgres connection string. |
| `ESHU_GRAPH_BACKEND` | `nornicdb` | Graph backend, `nornicdb` or `neo4j`. Invalid values fail startup. |
| `NEO4J_URI` | required | Bolt URI for NornicDB or Neo4j. |
| `NEO4J_USERNAME` | required | Bolt username. |
| `NEO4J_PASSWORD` | required | Bolt password. |
| `DEFAULT_DATABASE` | `nornic` | Bolt database name. |
| `ESHU_PROJECTION_WORKERS` | `min(NumCPU, 8)` | Bootstrap projection worker count. |
| `ESHU_DISCOVERY_REPORT` | unset | Write one discovery advisory JSON array for the collected repositories. |
| `ESHU_PPROF_ADDR` | unset | Enable opt-in pprof. Bare ports bind to `127.0.0.1`. |
| `ESHU_NEO4J_BATCH_SIZE` | backend default | Generic canonical writer batch size. |
| `ESHU_NEO4J_PROFILE_GROUP_STATEMENTS` | `false` | Neo4j grouped-write timing diagnostics. |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | `30s` for NornicDB | Graph write transaction timeout. |
| `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS` | `500` | Broad NornicDB grouped-statement cap. |
| `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` | `5` | File-phase grouped-statement cap. |
| `ESHU_NORNICDB_FILE_BATCH_SIZE` | `100` | File upsert row cap. |
| `ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS` | `25` | Canonical entity grouped-statement cap before label overrides. |
| `ESHU_NORNICDB_ENTITY_BATCH_SIZE` | `100` | Canonical entity row cap before label overrides. |
| `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES` | `Function=15,K8sResource=1,Struct=50,Variable=100` | Label-specific entity row caps. |
| `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS` | `Function=5,K8sResource=1,Struct=15,Variable=5` | Label-specific grouped-statement caps. |
| `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` | `false` | Conformance-only switch for Neo4j-style grouped canonical writes on NornicDB. |

Repository selection, snapshot, parse-worker, discovery-overlay, and local data
root settings are shared with the ingester and CLI indexing path. See
[Environment Variables](../../../docs/public/reference/environment-variables.md)
and [CLI Indexing](../../../docs/public/reference/cli-indexing.md).

## Telemetry

Bootstrap exports OpenTelemetry through the normal providers. It does not expose
the shared Prometheus HTTP endpoint.

| Signal | Where it is used |
| --- | --- |
| `telemetry.SpanCollectorObserve` | One collect and commit cycle. |
| `telemetry.SpanProjectorRun` | One claim, project, and ack cycle. |
| `eshu_dp_facts_emitted_total` | Facts emitted by bootstrap collection. |
| `eshu_dp_facts_committed_total` | Facts committed to Postgres. |
| `eshu_dp_collector_observe_duration_seconds` | Per-scope collection duration. |
| `eshu_dp_queue_claim_duration_seconds{queue=projector}` | Projector queue claim duration. |
| `eshu_dp_projector_run_duration_seconds` | Per-work-item projection duration. |
| `eshu_dp_projections_completed_total` | Projection completion count by status. |
| `eshu_dp_pipeline_overlap_seconds` | Time collection and projection overlapped. |
| `eshu_dp_gomemlimit_bytes` | Configured Go memory limit. |
| `eshu_dp_correlation_drift_intents_enqueued_total` | Phase 3.5 drift intents enqueued. |

Important failure classes:

- `commit_failure`
- `backfill_deferred_failure`
- `iac_reachability_materialization_failure`
- `reopen_deployment_mapping_failure`
- `enqueue_config_state_drift_failure`
- `projection_failure`
- `lease_heartbeat_failure`

## Known Edge Cases

- Items still pending or claimed during the reopen pass naturally see the open
  readiness gate when they run.
- Items that succeed in the small window between relationship backfill and the
  reopen pass are not automatically replayed today. Use admin replay or wait
  for incremental refresh.
- `errProjectorDrained` means the wrapped projector queue is empty after the
  collector finished. It is a clean shutdown sentinel, not a pipeline error.
- A discovery advisory report is an operator artifact. Do not turn its
  repository paths or file names into metric labels.
- The binary has no signal cleanup path. If a future change adds signal
  handling, it must define partial-phase recovery semantics first.

## Verification

Docs-only changes in this directory require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Runtime or code changes require focused command tests first:

```bash
cd go
go test ./cmd/bootstrap-index -count=1
go test ./cmd/bootstrap-index ./cmd/ingester ./internal/storage/postgres -count=1
```

Run the performance-evidence gates when changing workers, queues, leases,
batching, graph writes, NornicDB knobs, or any hot-path runtime behavior:

```bash
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
```

## Related Docs

- [Bootstrap Runtime Services](../../../docs/public/deployment/service-runtimes-bootstrap.md)
- [Docker Compose](../../../docs/public/run-locally/docker-compose.md)
- [Architecture](../../../docs/public/architecture.md)
- [Local Testing](../../../docs/public/reference/local-testing.md)
- [Profiling And Concurrency](../../../docs/public/reference/local-testing/profiling-and-concurrency.md)
- [Service Workflows](../../../docs/public/reference/service-workflows.md)
