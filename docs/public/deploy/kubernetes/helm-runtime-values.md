# Helm runtime values

Use this page for schema bootstrap, runtime environment overrides, reducer
lanes, and repository sync. The chart source is `deploy/helm/eshu`.

## Schema bootstrap

The chart renders one `schema-bootstrap` `Job` when
`schemaBootstrap.enabled=true`. The job runs
`/usr/local/bin/eshu-bootstrap-data-plane` and owns Postgres plus graph schema
setup for the release. Runtime pods then start without revalidating graph
schema in parallel.

| Value | Default | Purpose |
| --- | --- | --- |
| `schemaBootstrap.enabled` | `true` | Render the schema bootstrap `Job`. |
| `schemaBootstrap.useHelmHooks` | `true` | Add Helm `pre-install,pre-upgrade` hook annotations. |
| `schemaBootstrap.backoffLimit` | `1` | Retry budget for the job. |
| `schemaBootstrap.activeDeadlineSeconds` | `600` | Upper bound for one job attempt. |
| `schemaBootstrap.ttlSecondsAfterFinished` | `86400` | Cleanup window for non-hook job history. |
| `schemaBootstrap.serviceAccountName` | empty | Optional ServiceAccount for the bootstrap job. |
| `schemaBootstrap.resources` | `{}` | Resource requests and limits for the job. |

By default Helm waits for the hook before continuing the release, and Argo CD
maps Helm hooks to its `PreSync` flow. Existing Postgres, graph, and credential
dependencies must exist before the hook runs.

Do not combine `schemaBootstrap.useHelmHooks=true` with chart-managed NornicDB
(`nornicdb.enabled=true`). Helm pre-install hooks run before normal chart
resources are created, so the in-chart NornicDB Service and Deployment do not
exist yet. Deploy NornicDB separately first, point `neo4j.uri` at an existing
backend, or set `schemaBootstrap.useHelmHooks=false` and provide ordering
outside this chart.

When `schemaBootstrap.useHelmHooks=false`, the job is a normal chart resource.
Helm and Argo CD will not wait for it before creating API, MCP, ingester,
collector, or reducer workloads unless the caller supplies ordering outside the
chart, such as a split release or explicit GitOps sync wave.

For upgrades from older deployments where graph schema objects already exist
but the Postgres `graph_schema_applications` marker is empty, the NornicDB
bootstrap path automatically inspects existing constraints and indexes. When
all expected schema object names are present it records the current
backend/schema fingerprint and skips the live DDL pass. Brand-new installs still
create the schema normally because the inspection finds missing objects and
falls through to DDL. Set `env.ESHU_GRAPH_SCHEMA_ADOPT_EXISTING: "false"` only
when an operator intentionally wants to bypass adoption and force live DDL.

```yaml
schemaBootstrap:
  enabled: true
  useHelmHooks: true
  activeDeadlineSeconds: 600
  ttlSecondsAfterFinished: 86400
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
```

No-Regression Evidence: Helm template rendering emits exactly one
`eshu-schema-bootstrap` Job and no `db-migrate` init containers in the default
chart output; the runtime DDL binary and environment contract remain unchanged.

Observability Evidence: bootstrap logs emit `bootstrap.schema.started`,
`bootstrap.postgres.applied`, `bootstrap.graph.applied`,
`bootstrap.graph.adopted`, `bootstrap.graph.adoption_incomplete`, and
`runtime.startup.failed`; Kubernetes also exposes bounded rollout state through
`activeDeadlineSeconds`, `backoffLimit`, and Job success/failure status.

## Runtime environment and connection tuning

The chart renders global `env` first and workload-specific `env` second. Use
that merge order to set shared defaults globally and override only the workload
that needs a different value.

| Block | Applies to |
| --- | --- |
| `env` | API, MCP, ingester, reducer, workflow coordinator, and collectors. |
| `api.env` | API only. |
| `mcpServer.env` | MCP only. |
| `ingester.env` | Ingester only. |
| `resolutionEngine.env` | Reducer only unless a lane overrides it. |
| `workflowCoordinator.env` | Workflow coordinator only. |

Connection tuning follows the same pattern. Each runtime can tune Postgres pool
values and Bolt driver values through `connectionTuning.postgres` and
`connectionTuning.neo4j`.

```yaml
env:
  ESHU_GRAPH_BACKEND: nornicdb
  ESHU_CANONICAL_WRITE_TIMEOUT: 120s
  ESHU_SHARED_PROJECTION_WORKERS: "8"

api:
  env:
    GOMEMLIMIT: "1536MiB"

resolutionEngine:
  connectionTuning:
    postgres:
      maxOpenConns: "40"
      pingTimeout: 15s
    neo4j:
      maxConnectionPoolSize: "150"
      connectionAcquisitionTimeout: 20s
```

## Resolution engine lanes

By default Helm renders one `resolution-engine` `Deployment` that can claim all
reducer domains. Set `resolutionEngine.lanes` when a cluster needs separate
scaling lanes for different reducer domains. When lanes are configured, the
chart does not render the undifferentiated deployment; it renders one
deployment and metrics service per lane and sets `ESHU_REDUCER_CLAIM_DOMAINS`
inside each pod.

```yaml
resolutionEngine:
  lanes:
    - name: code-graph
      domains:
        - sql_relationship_materialization
        - inheritance_materialization
      replicas: 3
      env:
        ESHU_PPROF_ADDR: "127.0.0.1:6062"
      resources:
        requests:
          cpu: 750m
          memory: 1Gi
    - name: cloud-drift
      domains:
        - aws_cloud_runtime_drift
      replicas: 2
```

Use lanes to keep optional collector domains optional. For example, a cluster
that runs only Git and Terraform can omit AWS, OCI, Package Registry, and
Confluence lanes without leaving an all-domain reducer competing for that work.
The queue still enforces conflict keys and lease ownership; lanes only bound
which domains a reducer replica can claim.

Do not set global `env.ESHU_REDUCER_CLAIM_DOMAIN` or
`env.ESHU_REDUCER_CLAIM_DOMAINS` when `resolutionEngine.lanes` is non-empty.
The chart rejects that combination because each lane owns its domain allowlist.

Performance Impact Declaration: this changes reducer queue claims from one
optional domain equality to an optional `ANY(text[])` allowlist. The affected
stage is Postgres `fact_work_items` reducer claim selection over pending,
retrying, claimed, and running reducer rows. Expected cardinality is unchanged
per lane except that operators can split claim pressure by domain family. Stop
threshold: if claim duration for the same queue shape regresses by more than
10% or queue age rises while workers are idle, profile the claim query and
Postgres indexes before increasing replicas.

No-Regression Evidence: `go test ./cmd/reducer ./internal/storage/postgres ./internal/runtime -run 'TestLoadReducerClaimDomains|TestBuildReducerServiceWiresReducerClaimDomains|TestReducerQueueClaimCanFilterByMultipleDomains|TestClaimBatchCanFilterByMultipleDomains|TestHelmResolutionEngineCanRenderDomainSpecificLanes' -count=1`
and `go test ./...` passed on 2026-05-15 for the config parser, reducer service
wiring, Postgres claim SQL contract, Helm lane render contract, and broader Go
suite.

No-Observability-Change: reducer lanes reuse existing reducer queue and runtime
signals: `reducer.batch_claim` span, `eshu_dp_queue_claim_duration_seconds`,
`eshu_dp_reducer_queue_wait_seconds`, `eshu_dp_queue_depth`,
`eshu_dp_queue_oldest_age_seconds`, and `eshu_dp_reducer_executions_total`.
No new metric label was added because lane name would be deployment topology,
not a durable data-domain attribute.

## Repository sync

`repoSync` controls how the ingester discovers repositories.

| Value | Default | Purpose |
| --- | --- | --- |
| `repoSync.enabled` | `true` | Enable the recurring repo-sync loop. |
| `repoSync.bootstrap` | `true` | Run an initial sync at startup. |
| `repoSync.intervalSeconds` | `900` | Recurring sync interval. |
| `repoSync.auth.method` | `githubApp` | Auth mode: `githubApp`, `token`, `ssh`, or `none`. |
| `repoSync.source.mode` | `githubOrg` | Source mode: `githubOrg`, `explicit`, or `filesystem`. |
| `repoSync.source.rules` | `[]` | Exact or regex repository filters. |

`repoSync.source.rules` is rendered to `ESHU_REPOSITORY_RULES_JSON`. Use
`type: exact` or `type: regex` with a `value` field so the chart schema can
validate the value before install.

```yaml
repoSync:
  source:
    rules:
      - type: exact
        value: myorg/my-repo
      - type: regex
        value: myorg/platform-.*
```

The chart rejects `repoSync.auth.method=ssh` with
`repoSync.source.mode=githubOrg`; use `explicit` or `filesystem` for SSH-based
sync paths.
