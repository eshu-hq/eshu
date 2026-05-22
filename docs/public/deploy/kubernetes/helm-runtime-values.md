# Helm Runtime Values

Use this page as the operator map for schema bootstrap, runtime env, reducer
lanes, and repository sync. The chart contract lives in `deploy/helm/eshu`.

## Schema bootstrap

`schemaBootstrap.enabled=true` renders one `Job` named
`<release>-eshu-schema-bootstrap`. The job runs
`/usr/local/bin/eshu-bootstrap-data-plane` and applies Postgres plus graph
schema before runtime pods start when Helm hooks are enabled.

Defaults: `enabled=true`, `useHelmHooks=true`, `backoffLimit=1`,
`activeDeadlineSeconds=600`, `ttlSecondsAfterFinished=86400`,
`serviceAccountName=""`, empty annotations, empty pod annotations, and
resources of `100m/128Mi` requests with `1000m/1Gi` limits.

Do not combine `schemaBootstrap.useHelmHooks=true` with
`nornicdb.enabled=true`. Helm pre-install hooks run before normal chart
resources exist, so the bundled NornicDB Service and Deployment are not ready
for the hook. Deploy NornicDB outside the release first, or set
`schemaBootstrap.useHelmHooks=false` and provide ordering outside the chart.

For retained NornicDB graphs where graph schema objects already exist but the
Postgres `graph_schema_applications` marker is empty, bootstrap automatically
inspects existing constraints and indexes, records the compatible schema
fingerprint, and skips live DDL. Brand-new installs still create schema because
inspection finds missing objects. Set
`env.ESHU_GRAPH_SCHEMA_ADOPT_EXISTING: "false"` only to force live DDL.

No-Regression Evidence: Helm template rendering emits exactly one
`eshu-schema-bootstrap` Job and no `db-migrate` init containers in the default
chart output; the runtime DDL binary and environment contract remain unchanged.

Observability Evidence: bootstrap logs emit `bootstrap.schema.started`,
`bootstrap.postgres.applied`, `bootstrap.graph.applied`,
`bootstrap.graph.adopted`, `bootstrap.graph.adoption_incomplete`, and
`runtime.startup.failed`; Kubernetes also exposes bounded rollout state through
`activeDeadlineSeconds`, `backoffLimit`, and Job success/failure status.

## Runtime env and connection tuning

The chart renders global `env` first and workload-specific `env` second where
the workload has its own env block. Use global values for shared runtime
defaults and workload values for deliberate overrides.

| Block | Applies to |
| --- | --- |
| `env` | API, MCP, ingester, reducer, workflow coordinator, schema bootstrap, and collectors. |
| `api.env` | API only. |
| `mcpServer.env` | MCP server only. |
| `ingester.env` | Ingester only. |
| `resolutionEngine.env` | Reducer only when no lanes are configured. |
| `resolutionEngine.lanes[].env` | One reducer lane. |
| `workflowCoordinator.env` | Workflow coordinator only. |

Connection tuning maps to env vars only when values are non-empty.

| Tuning block | Env prefix |
| --- | --- |
| `connectionTuning.postgres.maxOpenConns` | `ESHU_POSTGRES_MAX_OPEN_CONNS` |
| `connectionTuning.postgres.maxIdleConns` | `ESHU_POSTGRES_MAX_IDLE_CONNS` |
| `connectionTuning.postgres.connMaxLifetime` | `ESHU_POSTGRES_CONN_MAX_LIFETIME` |
| `connectionTuning.postgres.connMaxIdleTime` | `ESHU_POSTGRES_CONN_MAX_IDLE_TIME` |
| `connectionTuning.postgres.pingTimeout` | `ESHU_POSTGRES_PING_TIMEOUT` |
| `connectionTuning.neo4j.maxConnectionPoolSize` | `ESHU_NEO4J_MAX_CONNECTION_POOL_SIZE` |
| `connectionTuning.neo4j.maxConnectionLifetime` | `ESHU_NEO4J_MAX_CONNECTION_LIFETIME` |
| `connectionTuning.neo4j.connectionAcquisitionTimeout` | `ESHU_NEO4J_CONNECTION_ACQUISITION_TIMEOUT` |
| `connectionTuning.neo4j.socketConnectTimeout` | `ESHU_NEO4J_SOCKET_CONNECT_TIMEOUT` |
| `connectionTuning.neo4j.verifyTimeout` | `ESHU_NEO4J_VERIFY_TIMEOUT` |

## Resolution engine lanes

By default Helm renders one resolution-engine Deployment that can claim all
reducer domains. Set `resolutionEngine.lanes` to render one Deployment and
metrics Service per lane. Each lane receives `ESHU_REDUCER_CLAIM_DOMAINS` with
the comma-separated lane domain list.

Each lane needs a Kubernetes label-safe lowercase `name` and at least one
domain in `domains`. `replicas`, `env`, `connectionTuning`, and `resources`
are optional and fall back to the parent `resolutionEngine` block.

Do not set global `env.ESHU_REDUCER_CLAIM_DOMAIN` or
`env.ESHU_REDUCER_CLAIM_DOMAINS` when `resolutionEngine.lanes` is non-empty.
The chart rejects that combination because each lane owns its domain allowlist.
Lanes bound which domains each reducer can claim; queue conflict keys and lease
ownership still control correctness.

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

Defaults: `enabled=true`, `bootstrap=true`, `initialDelaySeconds=0`,
`intervalSeconds=900`, `source.mode=githubOrg`, `source.githubOrg=eshu-hq`,
empty explicit repositories and rules, `source.filesystemRoot=/fixtures`,
`source.cloneDepth=1`, `source.limit=4000`, and `auth.method=githubApp`.

`repoSync.source.rules` renders to `ESHU_REPOSITORY_RULES_JSON`. The chart
rejects `repoSync.auth.method=ssh` with `repoSync.source.mode=githubOrg`; use
`explicit` or `filesystem` for SSH-based sync paths.
