# Service Runtimes

Use this page when you need the operator view of Eshu:

- which services exist
- what each service owns
- which command starts each service
- which service should be tuned or scaled
- where metrics are exposed
- where `ServiceMonitor` applies

For installation steps, use the
[Kubernetes deployment lane](../deploy/kubernetes/index.md). This page focuses
on runtime ownership and operator signals.

Every long-running runtime should also follow one operator principle:

- the service should expose a familiar admin/status story through the shared
  report seam
- the same service should be inspectable through CLI now and API/admin
  transport once mounted
- the exact counters may differ by runtime, but the operator experience should
  not

That shared admin contract is used by the core data-plane services, including
MCP, that mount `go/internal/runtime`:

- `/healthz` and `/readyz` describe process health and readiness
- `/metrics` exposes runtime and backlog signals
- `/admin/status` renders the shared status/report shape
- the CLI and HTTP/admin views should render the same underlying report
- live-versus-inferred state must be explicit in both views

Current platform reality:

- the platform runtime is implemented end to end in the checked-in services
- `collector-git`, `ingester`, and `bootstrap-index` own repository selection,
  repo sync, snapshot collection, parsing, content shaping, and fact emission
- local `eshu index`, `eshu workspace index`, `eshu watch`, and package indexing
  all hand off to Go binaries
- when `SCIP_INDEXER=true`, the collector owns SCIP language detection,
  external `scip-*` execution, protobuf reduction, and Go tree-sitter
  supplementation
- Terraform provider-schema assets are packaged and loaded from
  `go/internal/terraformschema/schemas/*.json.gz`
- graph-write coordination for shared edge domains now uses durable bounded
  readiness state in Postgres:
  `canonical_nodes_committed` is published by the projector,
  `semantic_nodes_committed` is published by semantic-entity materialization,
  and reducer-owned edge domains wait for that state before writing
- the API, MCP, ingester, workflow-coordinator, reducer, local verification
  runtimes, and bootstrap helpers emit structured JSON logs through the shared
  Go telemetry logger
- local Docker Compose does not start Jaeger or the OTEL collector by default;
  add `docker-compose.telemetry.yml` when you want laptop trace export

## Runtime Contract

| Runtime | Owns | Default command | Storage access | Metrics exposure | Kubernetes shape |
| --- | --- | --- | --- | --- | --- |
| DB Migrate | Postgres + graph schema DDL | `/usr/local/bin/eshu-bootstrap-data-plane` | Postgres DDL + graph DDL | none (exits immediately) | `initContainer` |
| API | HTTP API, query reads, admin endpoints | `eshu api start --host 0.0.0.0 --port 8080` | graph + content reads only | direct `/metrics`, optional `ServiceMonitor` | `Deployment` |
| MCP Server | MCP tool transport plus mounted query passthrough | `eshu mcp start` | graph + content reads only | direct `/metrics`, optional `ServiceMonitor` | `Deployment` |
| Ingester | repo sync, parsing, fact emission, workspace ownership | `/usr/local/bin/eshu-ingester` | workspace PVC + Postgres + graph backend | direct `/metrics`, optional `ServiceMonitor` | `StatefulSet` |
| Webhook Listener | public provider webhook intake and durable refresh triggers | `/usr/local/bin/eshu-webhook-listener` | Postgres trigger table only | direct `/metrics`, optional `ServiceMonitor` | `Deployment` |
| OCI Registry Collector | OCI registry scan, tag observation, manifest/referrer fact emission | `/usr/local/bin/eshu-collector-oci-registry` | Postgres fact store only | direct `/metrics`, optional `ServiceMonitor` | optional `Deployment` |
| Package Registry Collector | package metadata fetch, parser routing, package/version/dependency fact emission | `/usr/local/bin/eshu-collector-package-registry` | Postgres workflow + fact store only | direct `/metrics`, optional `ServiceMonitor` | optional `Deployment` |
| AWS Cloud Collector | AWS IAM-first cloud observation and fact emission | `/usr/local/bin/eshu-collector-aws-cloud` | Postgres workflow + fact store only | direct `/metrics`, optional `ServiceMonitor` | optional `Deployment` |
| Workflow Coordinator | scheduling, trigger intake, claims, completeness, run orchestration | `/usr/local/bin/eshu-workflow-coordinator` | Postgres + graph backend | internal admin/status service plus `/metrics`, optional `ServiceMonitor` | `Deployment` |
| Resolution Engine | queue draining, projection, retries, replay, recovery | `/usr/local/bin/eshu-reducer` | Postgres + graph backend | direct `/metrics`, optional `ServiceMonitor` | `Deployment` |
| Bootstrap Index | one-shot initial indexing | `/usr/local/bin/eshu-bootstrap-index` | workspace + Postgres + graph backend | OTEL export only; no mounted runtime `/metrics` endpoint | one-shot local helper |

Every direct service binary accepts `--version` and `-v` as a single argument.
That path prints the embedded application version and exits before telemetry,
datastore, graph, queue, or HTTP setup. Use it for container image checks,
support bundles, and install verification.

Deployment binaries do not embed NornicDB. Kubernetes, Helm, and Compose
service profiles connect to NornicDB or Neo4j as external Bolt-compatible graph
endpoints. Embedded NornicDB is only the local Eshu service path for
`eshu graph start`.

Local trace export is opt-in. For the NornicDB stack, run
`docker compose -f docker-compose.yaml -f docker-compose.telemetry.yml up
--build`. For the Neo4j stack, replace the base file with
`docker-compose.neo4j.yml`. Kubernetes deployments should wire telemetry
through the chart and cluster observability stack rather than the local Compose
overlay.

## Health, Status, And Completeness

- API, MCP, ingester, workflow-coordinator, reducer, and other long-running runtimes that mount
  `go/internal/runtime` use `/healthz`, `/readyz`, `/admin/status`, and
  `/metrics`.
- The MCP server also exposes `GET /health`, `GET /sse`, `POST /mcp/message`,
  and mounted `/api/*` routes for transport-specific behavior.
- Shared `/admin/status` reports the live runtime stage, backlog, and failure
  state where mounted.
- `GET /api/v0/status/index` is the normalized completeness route.
- `GET /api/v0/index-status` serves the same completeness payload.
- `GET /api/v0/repositories/{repo_id}/coverage` returns repository content-store
  coverage for files, entities, and languages that have been indexed for that
  repository.
- A service can be healthy while indexing is incomplete. Operators should use
  completeness routes before assuming a full run has finished.
- Code graph and dead-code query readiness also depends on shared projection
  domain backlog. `/admin/status` remains `progressing` while pending shared
  projection intents still need to make reducer-owned edges graph-visible.
- `bootstrap-index` remains a one-shot helper for empty or recovered
  environments. It does not mount the shared runtime HTTP admin surface and is
  not a steady-state health target.

## Local Verification Runtimes

The repo also has local verification runtimes that exercise the Go data plane
directly.

Most verification runtimes are not yet separate deployed Kubernetes workloads
in the public chart. `collector-oci-registry` is the exception in this branch:
it can run as an optional Helm `Deployment` when `ociRegistryCollector.enabled`
is true. Only the long-running hosted variants that mount `go/internal/runtime`
expose the shared `/healthz`, `/readyz`, optional `/metrics`, and optional
`/admin/status` contract:

- `collector-git`: `go run ./cmd/collector-git`
- `collector-aws-cloud`: `go run ./cmd/collector-aws-cloud`
- `collector-oci-registry`: `go run ./cmd/collector-oci-registry`
- `collector-package-registry`: `go run ./cmd/collector-package-registry`
- `collector-terraform-state`: `go run ./cmd/collector-terraform-state`
- `projector`: `go run ./cmd/projector`
- `reducer`: `go run ./cmd/reducer`

`collector-git` owns cycle orchestration, source-mode repository selection,
repo sync, durable fact commit, per-repo snapshot collection, content shaping,
the optional SCIP collector path, and the shared admin surface in Go.

`collector-terraform-state` is claim-driven. It selects one enabled
`terraform_state` collector instance from `ESHU_COLLECTOR_INSTANCES_JSON`, claims
workflow work for that instance, opens exact local or S3 state sources, and
commits redacted Terraform-state facts through the shared ingestion boundary.
The workflow coordinator remains the control plane; it does not parse state or
run collectors.

`collector-aws-cloud` is claim-driven. It selects one enabled `aws` collector
instance from `ESHU_COLLECTOR_INSTANCES_JSON`, claims `(account_id, region,
service_kind)` work for that instance, obtains claim-scoped AWS credentials
through central STS AssumeRole or local workload identity, and commits reported
AWS facts through the shared ingestion boundary. The scanner slices currently
cover IAM roles, managed policies, instance profiles, trust relationships, ECR
repositories, ECR lifecycle policies, ECR image references, ECS clusters, ECS
services, redacted ECS task definitions, ECS tasks, and ECS service
relationships, ELBv2 load balancers, listeners, listener rules, target groups,
routing relationships, Route 53 hosted zones, A/AAAA/CNAME/ALIAS DNS record
facts, and EC2 VPC/subnet/security-group/ENI network topology facts.

`collector-oci-registry` scans configured OCI registry repositories. In the
legacy local mode it reads `ESHU_OCI_REGISTRY_TARGETS_JSON`; when
`ESHU_COLLECTOR_INSTANCES_JSON` is present it selects a claim-enabled
`oci_registry` instance and claims one workflow work item per configured
repository target. It supports JFrog Docker/OCI, ECR, Docker Hub, GHCR, Harbor,
Google Artifact Registry, and Azure Container Registry client wiring, and
commits digest-addressed registry facts through the shared ingestion boundary.
It exposes `oci_registry.scan` and `oci_registry.api_call` spans plus the
`eshu_dp_oci_registry_*` metric family documented in the telemetry reference.

`collector-package-registry` is claim-driven. It selects one enabled
`package_registry` collector instance from `ESHU_COLLECTOR_INSTANCES_JSON`,
claims one workflow work item per configured target, fetches the target's
explicit `metadata_url`, parses npm, PyPI, Go module, Maven, NuGet, or generic
metadata, and commits reported package-registry facts through the shared
ingestion boundary. It exposes `package_registry.observe` and
`package_registry.fetch` spans plus the `eshu_dp_package_registry_*` metric
family documented in the telemetry reference.

## Admin Contract

The platform rule is a consistent operator/admin contract across long-running
services:

- one shared status/report seam
- one CLI surface for local and on-host inspection
- one reusable HTTP/admin adapter that can be mounted by a runtime without
  redefining the report shape
- one API/admin surface when the transport is mounted
- explicit live-versus-inferred labeling
- stage, backlog, success, and failure summaries in a familiar shape

This is intentionally a platform rule, not a one-off `admin-status` feature.
Operators should not need a different mental model for collector, projector,
reducer, or future background services.

Current runtime status:

- `go/internal/status/` owns the shared reader/report seam
- `go/cmd/admin-status/` renders that report through the local CLI
- `go/internal/status/http.go` provides the reusable HTTP transport adapter
- `go/internal/runtime/admin.go` provides the shared runtime probe and admin
  route mount for `/healthz`, `/readyz`, optional `/metrics`, and optional
  `/admin/status`
- hosted Go runtimes can now compose that shared admin server into their
  lifecycle without bespoke HTTP bootstrap code
- the MCP runtime now composes that shared admin surface alongside its
  transport-specific routes
- the API runtime mounts that shared contract today
- the workflow-coordinator runtime mounts that shared contract in dark mode
- `collector-git`, `projector`, and `reducer` all mount that shared admin
  surface in their local verification lanes
- the collector verification lane now uses native Go selection, repo sync,
  snapshot collection, content shaping, and optional SCIP execution/parsing
- the collector now emits parser follow-up facts for workload identity
  and canonical code-call materialization, and the reducer owns the resulting
  `CALLS` edge reconciliation path
- the projector and reducer now coordinate edge-domain writes through the
  durable `graph_projection_phase_state` table instead of assuming the
  canonical and semantic node phases finished in lock-step
- parser, admin, runtime, and workflow-coordinator behavior live in the current platform services

## Incremental Refresh And Reconciliation

Eshu should refresh incrementally by default and reconcile instead of forcing a
full re-index whenever possible.

- the `ingester` should reconcile only the scopes and generations that changed
- the `webhook-listener` should enqueue provider refresh triggers without
  cloning repositories, parsing files, or writing graph truth directly
- the `workflow-coordinator` should stay dark until claim ownership is enabled
- the `resolution-engine` should drain queued follow-up work and shared
  corrections from durable state
- `bootstrap-index` remains the one-shot escape hatch for empty environments or
  operator recovery
- future collector services should follow the same scope/generation contract
  rather than inventing a second freshness model

This means operators should use status, queue age, and generation state before
choosing to restart or reindex. A full re-index is a recovery tool, not the
normal freshness path.

## Naming Note

The public runtime names remain `eshu`, `mcp-server`,
`ingester`, `workflow-coordinator`, and `resolution-engine`. Operators should
still scale, monitor, and troubleshoot those service identities, and the
deployed processes are the Go runtime binaries documented on this page.

## Deployed Flow

```mermaid
flowchart LR
  A["Ingester"] --> B["Parse repository snapshot"]
  B --> C["Postgres fact store"]
  C --> D["Fact work queue"]
  D --> E["Resolution Engine"]
  E --> F["Graph backend"]
  E --> G["Postgres content store"]
  H["API"] --> F
  H --> G
  I["MCP Server"] --> F
  I --> G
```

## Local Full-Stack Flow

```mermaid
flowchart LR
  A["eshu-bootstrap-index"] --> B["Initial one-shot indexing"]
  C["ingester"] --> D["Ongoing sync and fact emission"]
  D --> E["Fact work queue"]
  E --> F["eshu-reducer"]
  G["eshu API"] --> H["Graph + content reads"]
```

## API

### Responsibilities

- serve HTTP query and admin requests
- read canonical graph state from the configured graph backend
- read file and entity content from Postgres
- serve operator/admin endpoints

### Does not own

- repository sync
- parsing
- fact emission
- queued projection work

### Deployments

- Compose service: `eshu`
- Helm template: `deploy/helm/eshu/templates/deployment.yaml`
- IaC chart template: `chart/templates/deployment.yaml`

### Signals to watch

- request latency and error rate
- content read latency
- graph query latency

Scale the API when request traffic rises. Do not scale it to fix queue backlog.

## MCP Server

### Responsibilities

- serve MCP SSE and JSON-RPC transport
- dispatch MCP tool calls over the mounted Go query surface
- expose the shared runtime admin surface plus MCP-specific health and session
  endpoints

### Does not own

- repository sync
- parsing
- fact emission
- queued projection work

### Deployments

- separate Go runtime started with `eshu mcp start`

### Signals to watch

- MCP session establishment and tool latency
- backend graph and content query latency
- transport health through `GET /health` plus shared readiness and status
  through `/healthz`, `/readyz`, `/admin/status`, and `/metrics`

Treat MCP as a separate query transport runtime, not as part of the API
process.

## Ingester

### Responsibilities

- discover and sync repositories
- own the shared workspace in Kubernetes
- parse repository snapshots
- emit facts into Postgres
- hand off durable projection work to the current write plane

### Why it stays stateful

The ingester is the only long-running runtime that should mount the workspace
PVC in Kubernetes.

### Deployments

- Compose service: `ingester`
- Helm template: `deploy/helm/eshu/templates/statefulset.yaml`
- IaC chart template: `chart/templates/statefulset.yaml`

### Signals to watch

- repository queue wait
- parse duration
- fact emission duration
- fact-store SQL latency
- workspace disk pressure

Scale or tune the ingester when parsing is the bottleneck or workspace pressure
is rising.

### Concurrency tuning

The ingester collector uses a goroutine worker pool for concurrent repository
snapshots. Set `ESHU_SNAPSHOT_WORKERS` to control parallelism (default:
`min(NumCPU, 8)`; the local-authoritative owner injects `NumCPU` on developer
machines). In Kubernetes, align CPU requests with the worker count to avoid CPU
throttling under concurrent parsing load.

## Resolution Engine

### Responsibilities

- claim fact work items
- load facts from Postgres
- project repository, file, entity, relationship, workload, and platform state
- persist projection decisions and bounded evidence
- manage retry, replay, dead-letter, and recovery workflows

### Deployments

- Compose service: `resolution-engine`
- Helm template:
  `deploy/helm/eshu/templates/deployment-resolution-engine.yaml`
- IaC chart template: `chart/templates/deployment-resolution-engine.yaml`

### Signals to watch

- queue depth and queue age
- claim latency
- per-stage projection duration
- per-stage output counts
- retry and dead-letter pressure
- Postgres queue and fact-store saturation

Scale the resolution-engine when queue age rises and workers remain busy. If
queue age rises together with Postgres contention, fix database pressure before
adding more workers.

### Concurrency tuning

The reducer supports concurrent intent execution (`ESHU_REDUCER_WORKERS`,
default NornicDB `NumCPU`, Neo4j `min(NumCPU, 4)`) and concurrent shared
projection partition processing (`ESHU_SHARED_PROJECTION_WORKERS`, default 1).
Increase these when queue age rises and single-worker CPU is not saturated. On
NornicDB, lower reducer workers only with queue and graph-write telemetry in
view because long graph writes can make lease ownership and backend contention
the limiting factor before CPU.

Additional shared projection config:

- `ESHU_SHARED_PROJECTION_PARTITION_COUNT` (default 8) — partitions per domain
- `ESHU_SHARED_PROJECTION_BATCH_LIMIT` (default 100) — intents per batch
- `ESHU_SHARED_PROJECTION_POLL_INTERVAL` (default 5s) — cycle poll interval
- `ESHU_SHARED_PROJECTION_LEASE_TTL` (default 60s) — partition lease TTL
- `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` (default 250000) — maximum
  code-call shared intents scanned or loaded for one accepted repo/run before
  failing safely instead of projecting partial CALLS truth
- `ESHU_CODE_CALL_EDGE_BATCH_SIZE` (default 1000) — code-call edge rows per
  grouped graph-write statement. Keep `eshu_dp_code_call_edge_batch_duration_seconds`
  and shared projection completion logs in view when overriding this value; it
  is a graph-write throughput knob, not a reducer worker-count substitute.

In Kubernetes, size the Postgres connection pool to accommodate the total
concurrent workers across all reducer replicas. Each worker holds one
connection during claim/execute/ack.

## Workflow Coordinator

### Responsibilities

- normalize triggers and schedule requests
- claim workflow work items and fenced claim epochs
- publish run state and completeness summaries
- expose the shared admin/status contract during dark rollout

### Does not own

- canonical graph reconciliation
- reducer-owned convergence truth
- repository sync or parsing
- permanent production claim ownership during the dark rollout slice

### Deployments

- Compose service profile: `workflow-coordinator`
- Helm template:
  `deploy/helm/eshu/templates/deployment-workflow-coordinator.yaml`
- Internal admin/metrics service:
  `deploy/helm/eshu/templates/service-workflow-coordinator.yaml`

### Signals to watch

- active runs
- stale claims
- queue age
- claim contention
- collection-complete versus reducer-converged state

The coordinator ships dark by default in this slice:

- `workflowCoordinator.enabled` defaults to `false`
- `workflowCoordinator.collectorInstances` defaults to `[]`
- Helm keeps `workflowCoordinator.deploymentMode=dark` and
  `workflowCoordinator.claimsEnabled=false`; active claim ownership is blocked
  in the chart for this branch
- `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=dark`
- `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=false`
- the shared `/admin/status` surface stays on so operators can validate the
  control plane before ownership is enabled

For proof runs, Compose may run the coordinator in active mode only when the
operator sets both claim flags and an explicit claim-enabled collector instance.
That proof path is for fenced claim validation, API/MCP truth checks, and
remote full-corpus timing; it is not the Kubernetes production default.

## DB Migrate (Schema Init Container)

`eshu-bootstrap-data-plane` applies all Postgres and graph backend schema DDL then
exits. It uses `CREATE TABLE IF NOT EXISTS` and `CREATE CONSTRAINT IF NOT
EXISTS` so it is safe to run repeatedly (idempotent).

### What it does

1. Connects to Postgres and runs `ApplyBootstrap` — creates all tables and
   indexes for facts, scopes, generations, content store, work queue, audit,
   relationships, shared intents, and projection decisions.
2. Connects to the graph backend and runs schema bootstrap — creates all node constraints,
   uniqueness indexes, performance indexes, and full-text indexes.
3. Exits with code 0 on success.

### Why it exists

Without this service, downstream runtimes (API, MCP, ingester, reducer)
had to wait for `bootstrap-index` to finish its full data population run
(50+ minutes on 895 repos) before starting. The schema init container
decouples DDL from data: services come up within seconds and serve traffic
on an empty-but-valid schema while bootstrap-index populates data in the
background.

### Docker Compose

In Compose, `db-migrate` is a short-lived service that exits after applying
schemas. All other services depend on it with
`condition: service_completed_successfully`:

```yaml
db-migrate:
  image: eshu:dev
  command: ["/usr/local/bin/eshu-bootstrap-data-plane"]
  environment:
    ESHU_GRAPH_BACKEND: nornicdb
    DEFAULT_DATABASE: nornic
    ESHU_NEO4J_DATABASE: nornic
    NEO4J_URI: bolt://nornicdb:7687
    NEO4J_USERNAME: neo4j
    NEO4J_PASSWORD: change-me
    ESHU_POSTGRES_DSN: postgresql://eshu:change-me@postgres:5432/eshu
  depends_on:
    nornicdb:
      condition: service_healthy
    postgres:
      condition: service_healthy
```

### EKS / Kubernetes Init Container

In EKS, add `eshu-bootstrap-data-plane` as an `initContainer` on every
Deployment and StatefulSet that needs database access. The init container
runs before the main container starts, ensuring schemas exist. The public Helm
chart now follows that contract for the API, MCP server, ingester,
workflow-coordinator, and resolution-engine workloads.

```yaml
# Example: API Deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu
spec:
  template:
    spec:
      initContainers:
        - name: db-migrate
          image: {{ .Values.image.repository }}:{{ .Values.image.tag }}
          command: ["/usr/local/bin/eshu-bootstrap-data-plane"]
          env:
            - name: ESHU_POSTGRES_DSN
              valueFrom:
                secretKeyRef:
                  name: eshu-db-credentials
                  key: dsn
            - name: NEO4J_URI
              value: bolt://nornicdb:7687
            - name: NEO4J_USERNAME
              valueFrom:
                secretKeyRef:
                  name: eshu-neo4j-credentials
                  key: username
            - name: NEO4J_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: eshu-neo4j-credentials
                  key: password
            - name: ESHU_GRAPH_BACKEND
              value: nornicdb
            - name: DEFAULT_DATABASE
              value: nornic
      containers:
        - name: api
          image: {{ .Values.image.repository }}:{{ .Values.image.tag }}
          command: ["/usr/local/bin/eshu-api"]
          # ...
```

Apply the same init container to:

| Workload | Kind | Why |
| --- | --- | --- |
| `eshu` (API) | `Deployment` | Reads from Postgres + graph backend |
| `mcp-server` | `Deployment` | Reads from Postgres + graph backend |
| `ingester` | `StatefulSet` | Writes facts to Postgres |
| `resolution-engine` (reducer) | `Deployment` | Writes to Postgres + graph backend |

### Environment variables

| Variable | Required | Purpose |
| --- | --- | --- |
| `ESHU_POSTGRES_DSN` | Yes | Postgres connection string |
| `ESHU_GRAPH_BACKEND` | No | Graph adapter, default `nornicdb` |
| `NEO4J_URI` | Yes | Bolt URI for NornicDB or Neo4j |
| `NEO4J_USERNAME` | Yes | Bolt auth username |
| `NEO4J_PASSWORD` | Yes | Bolt auth password |
| `DEFAULT_DATABASE` | No | Bolt database name, default `nornic` |

### Operational notes

- **Idempotent**: safe to run on every pod start — all DDL uses `IF NOT EXISTS`.
- **Fast**: completes in under 5 seconds on a warm database.
- **No data dependency**: does not populate any data, only creates empty tables
  and indexes. Data is populated by `bootstrap-index` or `ingester`.
- **Failure handling**: if the init container fails (database unreachable),
  Kubernetes will retry the pod according to `restartPolicy`. The main
  container will not start until schema migration succeeds.
- **Rolling updates**: when deploying a new version with schema changes, the
  init container on the first pod to roll applies the new DDL. Subsequent pods
  see it already applied (idempotent).

## Bootstrap Index

Bootstrap indexing is a one-shot operator activity, not a long-running
Kubernetes workload in the public Helm chart.

Use it when you want to:

- materialize an initial repository set quickly
- reduce cold-start time on a brand-new environment
- validate end-to-end indexing against a known repository set

Today it is packaged directly in Docker Compose and can also be run manually as
a direct process. Treat repeated restarts or long-running bootstrap activity as
an incident.

## Metrics And ServiceMonitor

### Local Compose

Compose exposes direct runtime scrape endpoints you can curl:

- API: `http://localhost:19464/metrics`
- Ingester: `http://localhost:19465/metrics`
- Resolution Engine: `http://localhost:19466/metrics`
- Webhook Listener: `http://localhost:${ESHU_WEBHOOK_LISTENER_HTTP_PORT:-18083}/metrics`
  when the optional `webhook-listener` profile is enabled
- Workflow Coordinator: `http://localhost:19469/metrics` when the optional
  profile is enabled

### Kubernetes

Helm can expose the same runtime metrics over dedicated ports and can also
render `ServiceMonitor` resources for:

- API
- Ingester
- MCP Server
- Workflow Coordinator
- Resolution Engine
- Webhook Listener
- Confluence Collector
- OCI Registry Collector

`ServiceMonitor` does not apply to the bootstrap helper because it is not a
steady-state Kubernetes service in the public chart.

## Operator Defaults

- treat API, ingester, workflow-coordinator, and resolution-engine as separate scaling units
- keep the workspace mounted only on the ingester in Kubernetes
- use direct `/metrics` endpoints for local verification
- use `ServiceMonitor` only for the long-running Kubernetes runtimes
- treat the local verification runtimes as operator and service-validation
  tools
- treat the shared admin/status report as the first place to look for live,
  inferred, backlog, and failure state
- prefer incremental scope refresh and reconciliation over platform-wide
  re-indexing
- use the [Telemetry Overview](../reference/telemetry/index.md) to decide which
  signal to inspect first
- use the [Local Testing Runbook](../reference/local-testing.md) before calling
  a change ready
