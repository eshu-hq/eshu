# Helm values

The chart lives at `deploy/helm/eshu`.

## Values to review first

| Value | Default | Purpose |
| --- | --- | --- |
| `image.repository` | `ghcr.io/eshu-hq/eshu` | Runtime image. |
| `image.tag` | `v0.0.2` | Runtime image tag. |
| `service.type` | `ClusterIP` | API service type. |
| `api.replicas` | `1` | API replica count. |
| `api.env` | `{}` | API-only environment overrides merged after global `env`. |
| `mcpServer.enabled` | `true` | Deploy the MCP runtime. |
| `mcpServer.env` | `{}` | MCP-only environment overrides merged after global `env`. |
| `ingester.persistence.size` | `100Gi` | Workspace PVC size. |
| `ingester.env` | `{}` | Ingester-only environment overrides merged after global `env`. |
| `resolutionEngine.enabled` | `true` | Deploy the reducer runtime. |
| `resolutionEngine.lanes` | `[]` | Optional domain-specific reducer deployments with independent replicas, resources, and claim allowlists. |
| `resolutionEngine.env` | `{}` | Reducer-only environment overrides merged after global `env`. |
| `schemaBootstrap.enabled` | `true` | Render the one-shot schema bootstrap Job. |
| `schemaBootstrap.useHelmHooks` | `true` | Annotate the Job as a Helm `pre-install,pre-upgrade` hook. Argo CD maps these Helm hooks to `PreSync`. |
| `schemaBootstrap.activeDeadlineSeconds` | `600` | Upper bound for one schema bootstrap Job attempt. |
| `schemaBootstrap.ttlSecondsAfterFinished` | `300` | Cleanup window when the Job is not deleted by hook policy. Successful hook Jobs are deleted by `hook-succeeded`. |
| `workflowCoordinator.enabled` | `false` | Deploy dark-mode workflow coordinator. |
| `workflowCoordinator.deploymentMode` | `dark` | Keep coordinator claim ownership dark. The chart rejects active mode in this branch. |
| `workflowCoordinator.claimsEnabled` | `false` | Keep workflow claims off in Helm. Use Compose for active proof runs. |
| `workflowCoordinator.collectorInstances` | `[]` | Declarative collector instances for dark reconciliation only. |
| `workflowCoordinator.env` | `{}` | Workflow-coordinator-only environment overrides merged after global `env`. |
| `confluenceCollector.enabled` | `false` | Deploy the Confluence documentation collector. |
| `confluenceCollector.baseUrl` | empty | Atlassian wiki base URL, for example `https://example.atlassian.net/wiki`. |
| `confluenceCollector.spaceId` | empty | Confluence space ID to crawl. Set this or `rootPageId`, not both. |
| `confluenceCollector.rootPageId` | empty | Root page ID for a bounded crawl. Set this or `spaceId`, not both. |
| `confluenceCollector.credentials.secretName` | empty | Secret containing Confluence auth material. |
| `ociRegistryCollector.enabled` | `false` | Deploy the OCI registry collector. |
| `ociRegistryCollector.instanceId` | `oci-registry-primary` | Collector instance ID used in emitted scope metadata. |
| `ociRegistryCollector.targets` | `[]` | OCI registry repositories to scan. Supports `jfrog`, `ecr`, `dockerhub`, and `ghcr`. |
| `ociRegistryCollector.aws.region` | empty | Optional AWS region env for ECR targets; EKS should use IRSA through `serviceAccount.annotations`. |
| `ociRegistryCollector.extraEnv` | `[]` | Extra env entries, usually Secret refs for JFrog, Docker Hub, or GHCR credentials named by target env indirection. |
| `terraformStateCollector.enabled` | `false` | Deploy the claim-driven Terraform-state collector. |
| `terraformStateCollector.collectorInstances` | `[]` | Desired `terraform_state` collector instances rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `terraformStateCollector.redaction.secretName` | empty | Secret containing `ESHU_TFSTATE_REDACTION_KEY`. Required when enabled. |
| `awsCloudCollector.enabled` | `false` | Deploy the claim-driven AWS cloud collector. |
| `awsCloudCollector.collectorInstances` | `[]` | Desired `aws` collector instances rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `awsCloudCollector.serviceAccount.create` | `false` | Create a dedicated AWS collector service account. Use this for IRSA so AWS permissions do not attach to every Eshu pod. |
| `awsCloudCollector.serviceAccount.name` | empty | Existing or generated AWS collector service-account name. Defaults to the AWS collector fullname when `create=true`; otherwise falls back to the shared release service account. |
| `awsCloudCollector.serviceAccount.annotations` | `{}` | Annotations for the AWS collector service account, including `eks.amazonaws.com/role-arn` for IRSA. |
| `awsCloudCollector.redaction.secretName` | empty | Optional secret containing `ESHU_AWS_REDACTION_KEY`; required by the binary when ECS or Lambda scans are enabled. |
| `packageRegistryCollector.enabled` | `false` | Deploy the claim-driven package registry collector. |
| `packageRegistryCollector.collectorInstances` | `[]` | Desired `package_registry` collector instances rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `packageRegistryCollector.extraEnv` | `[]` | Secret-backed env vars named by package-registry target credential indirection. |
| `webhookListener.enabled` | `false` | Deploy the public GitHub/GitLab/Bitbucket webhook intake runtime. |
| `webhookListener.github.enabled` | `false` | Enable the GitHub route. Requires `github.secretName`. |
| `webhookListener.gitlab.enabled` | `false` | Enable the GitLab route. Requires `gitlab.secretName`. |
| `webhookListener.bitbucket.enabled` | `false` | Enable the Bitbucket route. Requires `bitbucket.secretName`. |
| `webhookListener.exposure.ingress.enabled` | `false` | Render provider-only ingress paths for webhook delivery. |
| `contentStore.dsn` | empty | Postgres DSN. |
| `neo4j.uri` | `bolt://neo4j:7687` | Bolt URI for NornicDB or Neo4j. |
| `neo4j.auth.secretName` | `eshu-neo4j` | Secret for Bolt auth. Set to empty only for bundled NornicDB no-auth installs. |
| `neo4j.auth.username/password` | `neo4j` / `change-me` | Literal Bolt client credentials used when `neo4j.auth.secretName` is empty. |
| `env.ESHU_GRAPH_BACKEND` | `nornicdb` | Active graph adapter. |
| `observability.prometheus.serviceMonitor.enabled` | `false` | Render `ServiceMonitor` resources. |

Each runtime has `resources`, `env`, and `connectionTuning` blocks. Connection
tuning supports Postgres pool settings and Bolt driver settings per workload.
Workload-specific `env` maps are rendered after global `env`, so a runtime can
override a global value or enable a diagnostic knob such as `ESHU_PPROF_ADDR`
without turning it on for every pod.

The workflow coordinator chart is deliberately dark-only right now. Do not use
Helm values to promote coordinator-owned claims before the fenced claim,
fairness, Git collector, and remote full-corpus proof gates pass.

## Schema bootstrap

The chart renders one `schema-bootstrap` Job instead of repeating
`eshu-bootstrap-data-plane` from every workload pod. The Job owns Postgres and
graph schema DDL for the release; runtime pods then start without revalidating
graph schema in parallel.

By default the Job is a Helm `pre-install,pre-upgrade` hook. Helm waits for the
hook before continuing the release, and Argo CD treats those Helm hooks as
`PreSync` hooks. Existing Postgres, graph, and credential dependencies must be
available before the hook runs.

Do not combine `schemaBootstrap.useHelmHooks=true` with chart-managed NornicDB
(`nornicdb.enabled=true`). Helm pre-install hooks run before normal chart
resources are created, so the in-chart NornicDB Service and Deployment would not
exist yet. Deploy NornicDB separately first, point `neo4j.uri` at an existing
graph backend, or disable hook mode and provide an external ordering mechanism.

When `schemaBootstrap.useHelmHooks=false`, the Job is a normal chart resource.
Helm and Argo CD will not wait for it before creating the API, MCP, ingester,
collector, or reducer workloads unless the caller supplies ordering outside the
chart, such as a split release or explicit GitOps hook/wave policy. Use non-hook
mode only when that ordering is already handled.

If `schemaBootstrap.serviceAccountName` is set while
`schemaBootstrap.useHelmHooks=true`, the named ServiceAccount must already exist
before Helm runs the hook. Leave it empty to use the namespace default
ServiceAccount for the pre-install/pre-upgrade bootstrap job.

No-Regression Evidence: `helm template` renders exactly one
`eshu-schema-bootstrap` Job and no `db-migrate` init containers in the default
chart output; the runtime DDL binary and environment contract remain unchanged.

Observability Evidence: the existing bootstrap logs emit
`bootstrap.schema.started`, `bootstrap.postgres.applied`,
`bootstrap.graph.applied`, and `runtime.startup.failed`; the Kubernetes Job adds
bounded rollout status through `activeDeadlineSeconds`, `backoffLimit`, and Job
success/failure state.

```yaml
schemaBootstrap:
  enabled: true
  activeDeadlineSeconds: 600
  ttlSecondsAfterFinished: 300
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
```

## Resolution engine lanes

By default Helm renders one `resolution-engine` `Deployment` that can claim all
reducer domains. Set `resolutionEngine.lanes` when an EKS installation wants
separate reducer scaling lanes for different collector or graph-write domains.
When lanes are configured, the chart does not render the undifferentiated
deployment; it renders one deployment and metrics service per lane and sets
`ESHU_REDUCER_CLAIM_DOMAINS` inside each pod.

```yaml
resolutionEngine:
  env:
    ESHU_PPROF_ADDR: "127.0.0.1:6061"
  lanes:
    - name: code-graph
      domains:
        - sql_relationship_materialization
        - inheritance_materialization
      env:
        ESHU_PPROF_ADDR: "127.0.0.1:6062"
      replicas: 3
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
Confluence lanes without leaving an all-domain reducer that competes for their
work. The queue still enforces conflict keys and lease ownership; lanes only
bound which domains a reducer replica can claim.

Performance Impact Declaration: this changes reducer queue claims from one
optional domain equality to an optional `ANY(text[])` allowlist. The affected
stage is Postgres `fact_work_items` reducer claim selection over pending,
retrying, claimed, and running reducer rows. Expected cardinality is unchanged
per lane except that operators can split claim pressure by domain family.
Stop threshold: if claim duration for the same queue shape regresses by more
than 10% or queue age rises while workers are idle, profile the claim query and
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

## Confluence collector

The Confluence collector is off by default. When enabled, it stores
documentation sections in the configured Postgres content store and keeps the
runtime read-only against Confluence.

Use email/API-token credentials:

```yaml
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  spaceId: "123456789"
  credentials:
    secretName: confluence-collector-credentials
    emailKey: email
    apiTokenKey: api-token
```

Or use a bearer token:

```yaml
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  rootPageId: "987654321"
  credentials:
    secretName: confluence-collector-credentials
    bearerTokenKey: token
```

The chart rejects installs where the collector is enabled without a base URL,
credential Secret, or exactly one crawl scope.

## OCI registry collector

The OCI registry collector is off by default. When enabled, it scans configured
registry repositories and writes digest-addressed image facts to Postgres. ECR
on EKS should use IAM Roles for Service Accounts through
`serviceAccount.annotations`; do not set `aws_profile` in Kubernetes values.
This chart keeps OCI registry in explicit direct-target mode through
`ociRegistryCollector.targets`. Claim-aware OCI mode remains a runtime feature
for local or custom deployments that set `ESHU_COLLECTOR_INSTANCES_JSON`
directly.

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-oci-registry-collector

ociRegistryCollector:
  enabled: true
  instanceId: oci-registry-primary
  aws:
    region: us-east-1
  targets:
    - provider: ecr
      registry_id: "123456789012"
      region: us-east-1
      repository: team/api
      references: ["latest"]
    - provider: dockerhub
      repository: library/busybox
      references: ["latest"]
```

Private JFrog, Docker Hub, and GHCR targets should use target-level env
indirection plus `extraEnv` Secret refs:

```yaml
ociRegistryCollector:
  enabled: true
  targets:
    - provider: jfrog
      base_url: https://artifacts.example.test
      repository_key: docker-local
      repository: team/app
      username_env: JFROG_USERNAME
      password_env: JFROG_PASSWORD
  extraEnv:
    - name: JFROG_USERNAME
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: username
    - name: JFROG_PASSWORD
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: password
```

This collector currently proves registry-to-Postgres fact ingestion. Graph
projection and API/MCP image-correlation answers are a separate promotion step.

## Claim-driven collectors

Terraform-state, AWS cloud, and Package Registry collectors are off by default.
When enabled, each workload selects one claim-capable collector instance from
its own `collectorInstances` value and polls for durable workflow work. The
chart renders `ESHU_COLLECTOR_INSTANCES_JSON`, the instance selector, claim
lease TTL, heartbeat interval, pod-name owner ID, Postgres env, OTEL env,
Prometheus env, probes, metrics Service, ServiceMonitor, NetworkPolicy, and
PodDisruptionBudget for each enabled collector.

The workflow coordinator chart still remains dark-only in this branch. These
collector deployments are ready to claim work created by an approved control
plane path, but Helm values do not promote the coordinator to active claim
ownership yet.

Terraform-state uses secret-backed redaction. Do not put redaction keys or
state credentials in values:

```yaml
terraformStateCollector:
  enabled: true
  instanceId: terraform-state-primary
  redaction:
    secretName: tfstate-redaction
    keyKey: redaction-key
    rulesetVersion: schema-v1
  collectorInstances:
    - instance_id: terraform-state-primary
      collector_kind: terraform_state
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - target_scope_id: aws-prod
            provider: aws
            deployment_mode: central
            credential_mode: local_workload_identity
            allowed_regions: [us-east-1]
            allowed_backends: [s3]
```

AWS on EKS should use workload identity, not static access keys:

```yaml
awsCloudCollector:
  enabled: true
  instanceId: aws-primary
  serviceAccount:
    create: true
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-collector
  collectorInstances:
    - instance_id: aws-primary
      collector_kind: aws
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - account_id: "123456789012"
            allowed_regions: [us-east-1]
            allowed_services: [iam, ecr]
            credentials:
              mode: local_workload_identity
```

Package registry credentials use target-level env indirection plus Secret refs:

```yaml
packageRegistryCollector:
  enabled: true
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: jfrog
            ecosystem: npm
            registry: https://artifacts.example.test
            scope_id: npm://artifacts.example.test/team/app
            metadata_url: https://artifacts.example.test/api/npm/team/app
            username_env: PACKAGE_REGISTRY_USERNAME
            password_env: PACKAGE_REGISTRY_PASSWORD
  extraEnv:
    - name: PACKAGE_REGISTRY_USERNAME
      valueFrom:
        secretKeyRef:
          name: package-registry-credentials
          key: username
    - name: PACKAGE_REGISTRY_PASSWORD
      valueFrom:
        secretKeyRef:
          name: package-registry-credentials
          key: password
```

## Webhook listener

The webhook listener is off by default. When enabled, it accepts provider
webhook deliveries, verifies provider secrets, and writes refresh triggers to
Postgres. It does not mount the repository workspace PVC or graph credentials.

```yaml
webhookListener:
  enabled: true
  github:
    enabled: true
    secretName: github-webhook-secret
  bitbucket:
    enabled: true
    secretName: bitbucket-webhook-secret
  exposure:
    ingress:
      enabled: true
      hosts:
        - host: hooks.example.com
```

Only provider webhook paths are routed by the chart ingress. Set those paths
with `webhookListener.github.path`, `webhookListener.gitlab.path`, and
`webhookListener.bitbucket.path`; ingress hosts only select hostnames. Runtime
health, status, and metrics endpoints stay on the internal service unless an
operator adds separate protected routing.

## Repository sync

`repoSync.source.rules` is rendered to `ESHU_REPOSITORY_RULES_JSON`. Use
`type: exact` or `type: regex` with a `value` field so the chart schema can
validate the file before install.

## Exposure

The default service type is `ClusterIP`. For external traffic, use one of:

- `service.type=LoadBalancer`
- `exposure.ingress.enabled=true`
- `exposure.gateway.enabled=true`

Do not enable ingress and gateway at the same time. Each ingress or gateway
block routes to one backend: `api` or `mcp`.
