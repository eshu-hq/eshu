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

Do not set `ESHU_GENERATION_RETENTION_ENABLED=false` in global,
`resolutionEngine.env`, or `resolutionEngine.lanes[].env`. The chart rejects
that production render because generation retention must run beside reducer
work. The binary-level disable path is reserved for explicit local or test runs.

Connection tuning renders environment variables only when values are non-empty.
The chart owns those mappings in `deploy/helm/eshu/templates/_helpers.tpl`; keep
operator docs at the values-block level unless a page needs one exact variable.

The default global `env` also renders the hosted reducer partition defaults:
`ESHU_SHARED_PROJECTION_PARTITION_COUNT=8`,
`ESHU_SHARED_PROJECTION_WORKERS=8`,
`ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT=8`, and
`ESHU_CODE_CALL_PROJECTION_WORKERS=4`. Change code-call partition count only
between clean drains or after old shared-projection leases have released or
expired; the runtime serializes same-domain lease claims with a
transaction-scoped advisory lock and refuses active leases from two partition
counts so remapped file partitions cannot overlap.

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

## Scanner Worker

`scannerWorker.enabled=true` renders a separate `eshu-scanner-worker`
Deployment for CPU-heavy and memory-heavy security analyzers. It selects one
claim-capable `scanner_worker` collector instance from
`scannerWorker.collectorInstances`, applies analyzer resource limits, emits
source facts only, and leaves user-facing vulnerability findings to reducers.

Defaults: disabled, one replica, `instanceId=scanner-worker-source`,
`analyzer=source_analysis`, `pollInterval=30s`, `claimLeaseTTL=5m`,
`heartbeatInterval=55s`, `cpuMillis=4000`, `memoryBytes=4294967296`,
`timeout=10m`, `maxInputBytes=2147483648`, `maxFiles=250000`, and
`maxFacts=50000`. Kubernetes defaults request `cpu=1`, `memory=2Gi` and limit
the pod at `cpu=4`, `memory=4Gi`.

The chart rejects scanner-worker rendering unless the workflow coordinator is
enabled with active claims:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`

Use separate scanner-worker releases or values overlays when analyzer classes
need different CPU, memory, timeout, input-size, file-count, or fact-count
limits. Do not raise reducer resources to host SBOM generation, image
unpacking, source analysis, OS package extraction, secret scanning, license
scanning, or misconfiguration analysis.

Analyzer-specific target configuration lives in
`scannerWorker.collectorInstances[].configuration`. For
`analyzer=sbom_generation`, configure `sbom_targets[]` with a bounded
`scope_id`, runtime-local `root_path`, and optional `subject_digest`. The
scanner-worker pod reads repository manifests from that root and emits
`sbom.*` source facts only; reducers still own attachment and vulnerability
finding truth. Do not put private repository paths in public docs, metrics, or
failure payloads.

Observability Evidence: the Deployment exposes `/healthz`, `/readyz`,
`/metrics`, `/admin/status`, and optional private pprof through
`ESHU_PPROF_ADDR`. Scanner-worker metrics include queue wait, scan duration,
target count, result count, retry count, dead-letter count, CPU seconds, memory
bytes, and source facts emitted.

## Vulnerability Intelligence Collector

`vulnerabilityIntelligenceCollector.enabled=true` renders a separate
`eshu-vulnerability-intelligence-collector` Deployment that claims
`vulnerability_intelligence` work from the workflow coordinator and fetches
bounded vulnerability source snapshots (CISA KEV, FIRST EPSS, NVD windows, OSV
package-version queries, GitLab Gemnasium, GHSA) or derived owned-package
targets.

Defaults: disabled, one replica, `instanceId=vulnerability-intelligence-primary`,
`pollInterval=2s`, empty `claimLeaseTTL`/`heartbeatInterval` (binary defaults
apply), and Kubernetes defaults requesting `cpu=250m`, `memory=512Mi` and
limiting the pod at `cpu=1000m`, `memory=2Gi`.

The chart rejects vulnerability-intelligence rendering unless:

- `workflowCoordinator.enabled=true`, `deploymentMode=active`, and
  `claimsEnabled=true`.
- `vulnerabilityIntelligenceCollector.collectorInstances` contains a
  `vulnerability_intelligence` instance whose `instance_id` matches
  `vulnerabilityIntelligenceCollector.instanceId`.
- `workflowCoordinator.collectorInstances` contains an enabled,
  claim-driven `vulnerability_intelligence` instance.

Source targets are bounded by design: only explicit CVE IDs, source-level
snapshots (CISA KEV, FIRST EPSS), OSV package-version queries, NVD modified
windows, or derived owned-package targets are accepted. Credentials such as
NVD API keys are referenced by `api_key_env` in the instance configuration and
provided to the pod through `vulnerabilityIntelligenceCollector.extraEnv`
Secret references; never embed tokens in chart values.

Observability Evidence: the Deployment exposes `/healthz`, `/readyz`,
`/metrics`, and `/admin/status`. Collector metrics include queue wait, fetch
duration, retries, advisory source freshness, dead letters, and emitted
`vulnerability.*` facts.

## Repository Sync

`repoSync` controls how the ingester discovers repositories.

Defaults: enabled, bootstrap on, `initialDelaySeconds=0`,
`intervalSeconds=900`, `source.mode=githubOrg`, `source.githubOrg=eshu-hq`,
`source.cloneDepth=1`, `source.limit=4000`, and `auth.method=githubApp`.

`repoSync.source.rules` renders to `ESHU_REPOSITORY_RULES_JSON`. SSH auth is
valid only for `explicit` or `filesystem` source modes, not `githubOrg`.

`ingester.scipWorkers` defaults to `4` and renders `SCIP_WORKERS` for the
ingester. This keeps SCIP language/package-root indexing concurrent by default
inside the repository snapshot parse stage while preserving `SCIP_WORKERS=1` as
an explicit serial fallback for memory-constrained deployments.

Set `ingester.replicas` above `1` to run charted horizontal ingesters. The
chart maps `ESHU_REPO_SHARD_COUNT` to the replica count and maps
`ESHU_REPO_SHARD_INDEX` from the StatefulSet
`apps.kubernetes.io/pod-index` label, so operators must not set either shard
env var manually in global or ingester-specific `env`. Horizontal ingesters
require Kubernetes `1.32` or newer because the chart relies on the stable
StatefulSet pod-index label.

Horizontal ingesters require the default StatefulSet `volumeClaimTemplates`
workspace shape so each shard owns a distinct repository cache. The chart
rejects `ingester.persistence.existingClaim` when `ingester.replicas > 1`
because a shared PVC would let multiple shards mutate one checkout tree.

The ingester's deferred relationship-maintenance hook is fleet-safe under
multi-replica sharding: each shard records its local batch drain in the
Postgres `deferred_maintenance_barriers` epoch, only the shard that completes
the epoch runs global backfill and `deployment_mapping` reopen, and the
maintenance transaction takes an exclusive advisory lock that blocks
next-cycle source commits until maintenance commits or rolls back. Let a
barrier epoch complete before changing `ingester.replicas`; shard-count changes
while an epoch is open fail closed to protect graph truth.
