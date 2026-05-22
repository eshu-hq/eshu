# Helm Collector And Webhook Values

Use this page when enabling optional hosted collectors, the workflow
coordinator, or public webhook intake in the Helm chart. The source of truth is
`deploy/helm/eshu/values.yaml`, `values.schema.json`, and
`templates/validate.yaml`.

For runtime ownership, read
[Collector Runtime Services](../../deployment/service-runtimes-collectors.md)
and [Core Runtime Services](../../deployment/service-runtimes-core.md).

## Enable The Right Runtime

| Runtime | Chart block | Work source | Default |
| --- | --- | --- | --- |
| Workflow Coordinator | `workflowCoordinator` | reconciles collector instances and claims | disabled |
| Webhook Listener | `webhookListener` | provider webhook deliveries | disabled |
| Confluence Collector | `confluenceCollector` | explicit Confluence scope | disabled |
| OCI Registry Collector | `ociRegistryCollector` | explicit registry targets | disabled |
| Terraform State Collector | `terraformStateCollector` | workflow claims | disabled |
| AWS Cloud Collector | `awsCloudCollector` | workflow claims | disabled |
| Package Registry Collector | `packageRegistryCollector` | workflow claims | disabled |

Confluence and OCI registry are direct-target collectors in the public chart.
Terraform-state, AWS cloud, and package-registry are claim-driven collectors.

## Claim-Driven Collector Rule

Claim-driven collector Deployments require an active workflow coordinator.

```yaml
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets: []
```

The chart fails during render when any claim-driven collector is enabled
without all three coordinator settings:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`

When `workflowCoordinator.claimsEnabled=true`, the chart also requires
`workflowCoordinator.collectorInstances` to contain at least one instance.

## Workflow Coordinator Values

| Value | Default | Purpose |
| --- | --- | --- |
| `workflowCoordinator.enabled` | `false` | Render the coordinator Deployment and metrics Service. |
| `workflowCoordinator.deploymentMode` | `dark` | `active` schedules claims; `dark` does not. |
| `workflowCoordinator.claimsEnabled` | `false` | Allow claim creation and claim reaping. |
| `workflowCoordinator.collectorInstances` | `[]` | Rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `workflowCoordinator.replicas` | `1` | Coordinator replica count. |
| `workflowCoordinator.env` | `{}` | Coordinator-specific environment overrides. |
| `workflowCoordinator.connectionTuning.postgres.*` | empty | Postgres pool and timeout overrides. |
| `workflowCoordinator.connectionTuning.neo4j.*` | empty | Graph client pool and timeout overrides. |

`workflowCoordinator.collectorInstances` is shared control-plane state. Keep the
instance IDs aligned with each collector's `instanceId` value.

## Direct Collectors

### Confluence

The Confluence collector reads Confluence documentation and writes
documentation facts. It is read-only against Confluence.

| Value | Default | Purpose |
| --- | --- | --- |
| `confluenceCollector.enabled` | `false` | Render the collector Deployment and metrics Service. |
| `confluenceCollector.baseUrl` | empty | Atlassian wiki base URL. Required when enabled. |
| `confluenceCollector.spaceId` | empty | Crawl one space by ID. |
| `confluenceCollector.spaceIds` | `[]` | Crawl an explicit space allowlist. |
| `confluenceCollector.rootPageId` | empty | Crawl under one root page. |
| `confluenceCollector.spaceKey` | empty | Optional space key env. |
| `confluenceCollector.pageLimit` | empty | Optional page limit. |
| `confluenceCollector.pollInterval` | `5m` | Poll interval. |
| `confluenceCollector.credentials.secretName` | empty | Secret containing auth material. Required when enabled. |
| `confluenceCollector.credentials.emailKey` | `email` | Email key for email/API-token auth. |
| `confluenceCollector.credentials.apiTokenKey` | `api-token` | API token key for email/API-token auth. |
| `confluenceCollector.credentials.bearerTokenKey` | empty | Bearer token key. Replaces email/API-token auth when set. |

The chart requires `baseUrl`, a credentials secret, and exactly one crawl
scope: `spaceId`, `spaceIds`, or `rootPageId`.

### OCI Registry

The OCI registry collector reads configured registry targets and writes
digest-addressed image facts.

| Value | Default | Purpose |
| --- | --- | --- |
| `ociRegistryCollector.enabled` | `false` | Render the collector Deployment and metrics Service. |
| `ociRegistryCollector.instanceId` | `oci-registry-primary` | Collector instance ID. |
| `ociRegistryCollector.pollInterval` | `5m` | Poll interval. |
| `ociRegistryCollector.targets` | `[]` | Direct registry targets. Required when enabled. |
| `ociRegistryCollector.aws.region` | empty | Optional default AWS region for ECR. |
| `ociRegistryCollector.extraEnv` | `[]` | Secret-backed env vars for target credential indirection. |

`values.schema.json` allows target providers `jfrog`, `ecr`, `dockerhub`, and
`ghcr`. Each target requires `provider` and `repository`.

Use workload identity for ECR on EKS. Do not put static registry passwords in
values; use target-level `*_env` fields plus `extraEnv` Secret references.

## Claim-Driven Collectors

### Terraform State

Terraform-state collection is claim-driven and redacts sensitive state evidence.

| Value | Default | Purpose |
| --- | --- | --- |
| `terraformStateCollector.enabled` | `false` | Render the collector Deployment and metrics Service. |
| `terraformStateCollector.instanceId` | `terraform-state-primary` | Selected collector instance. Required when enabled. |
| `terraformStateCollector.pollInterval` | `1s` | Claim poll interval. |
| `terraformStateCollector.claimLeaseTTL` | empty | Optional claim lease TTL. |
| `terraformStateCollector.heartbeatInterval` | empty | Optional claim heartbeat interval. |
| `terraformStateCollector.sourceMaxBytes` | empty | Optional state payload limit. |
| `terraformStateCollector.collectorInstances` | `[]` | Rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. Required when enabled. |
| `terraformStateCollector.redaction.secretName` | empty | Secret containing the redaction key. Required when enabled. |
| `terraformStateCollector.redaction.keyKey` | `redaction-key` | Secret key for the redaction key. |
| `terraformStateCollector.redaction.rulesetVersion` | empty | Redaction ruleset version. Required when enabled. |
| `terraformStateCollector.redaction.sensitiveKeys` | empty | Optional sensitive-key override list. |
| `terraformStateCollector.extraEnv` | `[]` | Extra Secret-backed environment variables. |

Use the [Terraform State Collector](../../services/collector-terraform-state.md)
runbook for target-scope and redaction details.

### AWS Cloud

AWS cloud collection is claim-driven. On EKS, prefer IRSA through
`awsCloudCollector.serviceAccount.annotations`.

| Value | Default | Purpose |
| --- | --- | --- |
| `awsCloudCollector.enabled` | `false` | Render the collector Deployment and metrics Service. |
| `awsCloudCollector.instanceId` | `aws-cloud-primary` | Selected collector instance. Required when enabled. |
| `awsCloudCollector.pollInterval` | `1s` | Claim poll interval. |
| `awsCloudCollector.claimLeaseTTL` | empty | Optional claim lease TTL. |
| `awsCloudCollector.heartbeatInterval` | empty | Optional claim heartbeat interval. |
| `awsCloudCollector.collectorInstances` | `[]` | Rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. Required when enabled. |
| `awsCloudCollector.serviceAccount.create` | `false` | Render a dedicated collector ServiceAccount. |
| `awsCloudCollector.serviceAccount.name` | empty | Existing ServiceAccount name override. |
| `awsCloudCollector.serviceAccount.annotations` | `{}` | IRSA or cloud-identity annotations. |
| `awsCloudCollector.redaction.secretName` | empty | Optional redaction key Secret. |
| `awsCloudCollector.redaction.keyKey` | `redaction-key` | Secret key for optional redaction material. |
| `awsCloudCollector.extraEnv` | `[]` | Extra Secret-backed environment variables. |

Use the [AWS Cloud Collector](../../services/collector-aws-cloud.md) runbook for
target-scope, permissions, and scanner coverage.

### Package Registry

Package registry collection is claim-driven and uses target-level credential
environment indirection.

| Value | Default | Purpose |
| --- | --- | --- |
| `packageRegistryCollector.enabled` | `false` | Render the collector Deployment and metrics Service. |
| `packageRegistryCollector.instanceId` | `package-registry-primary` | Selected collector instance. Required when enabled. |
| `packageRegistryCollector.pollInterval` | `1s` | Claim poll interval. |
| `packageRegistryCollector.claimLeaseTTL` | empty | Optional claim lease TTL. |
| `packageRegistryCollector.heartbeatInterval` | empty | Optional claim heartbeat interval. |
| `packageRegistryCollector.collectorInstances` | `[]` | Rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. Required when enabled. |
| `packageRegistryCollector.extraEnv` | `[]` | Secret-backed env vars for registry credentials. |

Supported parser families are documented in
[Collector Runtime Services](../../deployment/service-runtimes-collectors.md).

## Webhook Listener

The webhook listener verifies provider secrets and writes durable refresh
triggers to Postgres. It does not mount the repository workspace PVC and does
not connect to the graph backend.

| Value | Default | Purpose |
| --- | --- | --- |
| `webhookListener.enabled` | `false` | Render the listener Deployment and Service. |
| `webhookListener.maxBodyBytes` | `1048576` | Maximum webhook body size. |
| `webhookListener.defaultBranch` | empty | Optional fallback branch for refresh triggers. |
| `webhookListener.github.enabled` | `false` | Enable `/webhooks/github` by default. |
| `webhookListener.github.secretName` | empty | GitHub secret Secret name. Required when GitHub is enabled. |
| `webhookListener.gitlab.enabled` | `false` | Enable `/webhooks/gitlab` by default. |
| `webhookListener.gitlab.secretName` | empty | GitLab token Secret name. Required when GitLab is enabled. |
| `webhookListener.bitbucket.enabled` | `false` | Enable `/webhooks/bitbucket` by default. |
| `webhookListener.bitbucket.secretName` | empty | Bitbucket secret Secret name. Required when Bitbucket is enabled. |
| `webhookListener.awsFreshness.enabled` | `false` | Enable `/webhooks/aws/eventbridge` by default. |
| `webhookListener.awsFreshness.secretName` | empty | AWS freshness token Secret name. Required when AWS freshness is enabled. |
| `webhookListener.exposure.ingress.enabled` | `false` | Render provider-only Ingress paths. |
| `webhookListener.exposure.ingress.hosts` | `[]` | Hosts for provider-only webhook routing. |

When `webhookListener.enabled=true`, the chart requires at least one enabled
provider. The webhook Ingress renders only exact provider paths. Keep health,
status, and metrics routes internal unless you add separate protected routing.

## Shared Workload Settings

Each collector and the webhook listener supports:

- `replicas`
- `revisionHistoryLimit`
- `resources`
- `connectionTuning.postgres.*`
- global `podLabels`, `podAnnotations`, `nodeSelector`, `affinity`, and
  `tolerations`

Workflow coordinator also supports `connectionTuning.neo4j.*`. Workload
environment maps are rendered after global `env` when a workload has its own
`env` block.

## Render Checks

Run these before applying collector or webhook values:

```bash
helm template eshu ./deploy/helm/eshu -f values.eshu.yaml
helm lint ./deploy/helm/eshu -f values.eshu.yaml
```

The render fails for these common mistakes:

- claim-driven collectors without an active workflow coordinator
- empty `collectorInstances` for enabled claim-driven collectors
- missing Terraform-state redaction Secret or ruleset version
- OCI registry enabled with no targets
- webhook listener enabled with no provider
- webhook provider enabled without its Secret name
- Confluence enabled without base URL, credentials, or exactly one crawl scope

## Related Docs

- [Helm Values](helm-values.md)
- [Runtime Values](helm-runtime-values.md)
- [Routing And Storage Values](helm-routing-and-storage-values.md)
- [Collector Runtime Services](../../deployment/service-runtimes-collectors.md)
- [Workflow Coordinator Runtime](../../deployment/service-runtimes-core.md#workflow-coordinator)
- [Webhook Listener Runtime](../../deployment/service-runtimes-core.md#webhook-listener)
