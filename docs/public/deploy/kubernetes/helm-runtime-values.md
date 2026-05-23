# Helm Runtime Values

Use this page for values owned by steady-state runtime behavior: schema
bootstrap, global env, connection tuning, reducer lanes, and repository sync.

## Schema Bootstrap

`schemaBootstrap.enabled=true` renders one Job that runs
`/usr/local/bin/eshu-bootstrap-data-plane`. With
`schemaBootstrap.useHelmHooks=true`, it runs before install and upgrade
workloads.

Defaults: enabled, Helm hooks on, `backoffLimit=1`,
`activeDeadlineSeconds=600`, `ttlSecondsAfterFinished=86400`, empty service
account and annotations, and `100m/128Mi` requests with `1000m/1Gi` limits.

Do not combine Helm hooks with chart-managed NornicDB. Hooks run before normal
chart resources exist, so the NornicDB Service is unavailable. Deploy NornicDB
outside the release first, or set `schemaBootstrap.useHelmHooks=false` and
order the Job outside Helm.

For retained NornicDB graphs where graph schema objects already exist but the
Postgres `graph_schema_applications` marker is empty, bootstrap automatically
inspects existing constraints and indexes, records the compatible schema
fingerprint, and skips live DDL. Brand-new installs still create schema because
inspection finds missing objects. Set
`env.ESHU_GRAPH_SCHEMA_ADOPT_EXISTING: "false"` only to force live DDL.

No-Regression Evidence: default Helm rendering emits one schema-bootstrap Job
and no `db-migrate` init containers; the runtime DDL binary and environment
contract remain unchanged.

Observability Evidence: bootstrap logs emit schema start/apply/adoption events,
and Kubernetes exposes bounded Job state through `activeDeadlineSeconds`,
`backoffLimit`, and Job success/failure status.

## Env And Connection Tuning

Global `env` renders first. Workload-specific `env` renders second and can
override a global value.

| Block | Applies to |
| --- | --- |
| `env` | API, MCP, ingester, reducer, workflow coordinator, schema bootstrap, collectors. |
| `api.env`, `mcpServer.env`, `ingester.env`, `workflowCoordinator.env` | One named workload. |
| `resolutionEngine.env` | Single reducer Deployment when no lanes are configured. |
| `resolutionEngine.lanes[].env` | One reducer lane Deployment. |

Connection tuning renders environment variables only when values are non-empty.
The chart owns those mappings in `deploy/helm/eshu/templates/_helpers.tpl`; keep
operator docs at the values-block level unless a page needs one exact variable.

## Resolution Engine Lanes

`resolutionEngine.lanes=[]` renders one resolution-engine Deployment. Non-empty
lanes render one Deployment and metrics Service per lane, and each lane gets
`ESHU_REDUCER_CLAIM_DOMAINS` from its `domains` list.

Each lane needs a Kubernetes label-safe lowercase `name` and at least one
domain. `replicas`, `env`, `connectionTuning`, and `resources` fall back to the
parent `resolutionEngine` block when omitted.

Do not set global `env.ESHU_REDUCER_CLAIM_DOMAIN` or
`env.ESHU_REDUCER_CLAIM_DOMAINS` with lanes. The chart rejects that combination
because lane domains must be owned per lane.

Performance Impact Declaration: reducer lanes change queue claims from one
optional domain equality to an optional `ANY(text[])` allowlist over
`fact_work_items`. Expected cardinality is unchanged per lane. Stop threshold:
if claim duration for the same queue shape regresses by more than 10% or queue
age rises while workers are idle, profile the claim query and Postgres indexes
before increasing replicas.

No-Regression Evidence: focused reducer config, Postgres claim SQL, runtime
wiring, and Helm lane render tests passed on 2026-05-15, followed by the full
Go suite.

No-Observability-Change: lanes reuse existing reducer queue and runtime signals:
`reducer.batch_claim`, `eshu_dp_queue_claim_duration_seconds`,
`eshu_dp_reducer_queue_wait_seconds`, `eshu_dp_queue_depth`,
`eshu_dp_queue_oldest_age_seconds`, and `eshu_dp_reducer_executions_total`.

## Repository Sync

`repoSync` controls how the ingester discovers repositories.

Defaults: enabled, bootstrap on, `initialDelaySeconds=0`,
`intervalSeconds=900`, `source.mode=githubOrg`, `source.githubOrg=eshu-hq`,
`source.cloneDepth=1`, `source.limit=4000`, and `auth.method=githubApp`.

`repoSync.source.rules` renders to `ESHU_REPOSITORY_RULES_JSON`. SSH auth is
valid only for `explicit` or `filesystem` source modes, not `githubOrg`.
