# Helm Collector And Webhook Values

Use this page as the operator map for optional collectors, workflow
coordination, and webhook intake. The chart contract lives in
`deploy/helm/eshu/values.yaml`, `deploy/helm/eshu/values.schema.json`, and
`deploy/helm/eshu/templates/validate.yaml`.

For runtime ownership, read
[Collector Runtime Services](../../deployment/service-runtimes-collectors.md)
and [Core Runtime Services](../../deployment/service-runtimes-core.md).

## Runtime map

| Runtime | Chart block | Work source | Default |
| --- | --- | --- | --- |
| Workflow coordinator | `workflowCoordinator` | collector instance config and claim scheduling | disabled |
| Webhook listener | `webhookListener` | GitHub, GitLab, Bitbucket, or AWS freshness deliveries | disabled |
| Confluence collector | `confluenceCollector` | direct Confluence scope | disabled |
| OCI registry collector | `ociRegistryCollector` | direct registry target list | disabled |
| Terraform-state collector | `terraformStateCollector` | workflow claims | disabled |
| AWS cloud collector | `awsCloudCollector` | workflow claims | disabled |
| Package-registry collector | `packageRegistryCollector` | workflow claims | disabled |

Direct collectors render when their own block is enabled and required target
values are present. Claim-driven collectors also require an active workflow
coordinator because they poll collector claims instead of a static target list.

## Claim-driven guardrail

Enabling `terraformStateCollector`, `awsCloudCollector`, or
`packageRegistryCollector` requires all of these values:

| Value | Required value |
| --- | --- |
| `workflowCoordinator.enabled` | `true` |
| `workflowCoordinator.deploymentMode` | `active` |
| `workflowCoordinator.claimsEnabled` | `true` |
| `workflowCoordinator.collectorInstances` | at least one instance |
| `<collector>.collectorInstances` | at least one instance |
| `<collector>.instanceId` | non-empty |

`workflowCoordinator.collectorInstances` is rendered to
`ESHU_COLLECTOR_INSTANCES_JSON` for the coordinator. Each claim-driven
collector also receives its own `<collector>.collectorInstances` as
`ESHU_COLLECTOR_INSTANCES_JSON`; keep the selected `instanceId` aligned with
the instance list.

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

packageRegistryCollector:
  enabled: true
  instanceId: package-registry-primary
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets: []
```

## Workflow coordinator

| Value | Default | Operator note |
| --- | --- | --- |
| `workflowCoordinator.enabled` | `false` | Renders the coordinator Deployment and metrics Service. |
| `workflowCoordinator.deploymentMode` | `dark` | Must be `active` before claims are scheduled. |
| `workflowCoordinator.claimsEnabled` | `false` | Requires `deploymentMode=active` when true. |
| `workflowCoordinator.collectorInstances` | `[]` | JSON control-plane state for collector instances. |
| `workflowCoordinator.replicas` | `1` | Coordinator replica count. |
| `workflowCoordinator.env` | `{}` | Coordinator-specific env overrides merged after global `env`. |
| `workflowCoordinator.connectionTuning.postgres.*` | empty | Postgres pool and timeout env. |
| `workflowCoordinator.connectionTuning.neo4j.*` | empty | Bolt driver pool and timeout env. |

## Direct collectors

### Confluence

| Value | Default | Operator note |
| --- | --- | --- |
| `confluenceCollector.enabled` | `false` | Renders the collector Deployment and metrics Service. |
| `confluenceCollector.baseUrl` | empty | Required when enabled. |
| `confluenceCollector.spaceId` | empty | One allowed crawl scope. |
| `confluenceCollector.spaceIds` | `[]` | One allowed crawl scope. |
| `confluenceCollector.rootPageId` | empty | One allowed crawl scope. |
| `confluenceCollector.spaceKey` | empty | Optional env for space-key workflows. |
| `confluenceCollector.pageLimit` | empty | Optional page limit. |
| `confluenceCollector.pollInterval` | `5m` | Poll interval. |
| `confluenceCollector.credentials.secretName` | empty | Required when enabled. |
| `confluenceCollector.credentials.emailKey` | `email` | Used with `apiTokenKey` unless `bearerTokenKey` is set. |
| `confluenceCollector.credentials.apiTokenKey` | `api-token` | Used with `emailKey` unless `bearerTokenKey` is set. |
| `confluenceCollector.credentials.bearerTokenKey` | empty | Uses bearer-token auth instead of email/API-token auth. |

The chart requires exactly one crawl scope: `spaceId`, `spaceIds`, or
`rootPageId`.

### OCI registry

| Value | Default | Operator note |
| --- | --- | --- |
| `ociRegistryCollector.enabled` | `false` | Renders the collector Deployment and metrics Service. |
| `ociRegistryCollector.instanceId` | `oci-registry-primary` | Collector instance identifier. |
| `ociRegistryCollector.pollInterval` | `5m` | Poll interval. |
| `ociRegistryCollector.targets` | `[]` | Required when enabled. |
| `ociRegistryCollector.aws.region` | empty | Optional default AWS region for ECR. |
| `ociRegistryCollector.extraEnv` | `[]` | Secret-backed target credential env. |

Each target requires `provider` and `repository`. The schema allows `jfrog`,
`ecr`, `dockerhub`, and `ghcr`. Use target-level `*_env` fields plus
`extraEnv` Secret references for credentials; do not put static passwords in
values.

## Claim-driven collectors

| Collector | Required values | Optional values |
| --- | --- | --- |
| Terraform state | `enabled`, `instanceId`, `collectorInstances`, `redaction.secretName`, `redaction.keyKey`, `redaction.rulesetVersion` | `pollInterval`, `claimLeaseTTL`, `heartbeatInterval`, `sourceMaxBytes`, `redaction.sensitiveKeys`, `extraEnv`, Postgres tuning |
| AWS cloud | `enabled`, `instanceId`, `collectorInstances` | `pollInterval`, `claimLeaseTTL`, `heartbeatInterval`, `serviceAccount.*`, optional `redaction.secretName`, `extraEnv`, Postgres tuning |
| Package registry | `enabled`, `instanceId`, `collectorInstances` | `pollInterval`, `claimLeaseTTL`, `heartbeatInterval`, `extraEnv`, Postgres tuning |

Terraform-state redaction values map to `ESHU_TFSTATE_REDACTION_KEY`,
`ESHU_TFSTATE_REDACTION_RULESET_VERSION`, and optional
`ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS` env vars. AWS redaction material is
optional and renders only when
`awsCloudCollector.redaction.secretName` is set. On EKS, prefer IRSA through
`awsCloudCollector.serviceAccount.annotations`.

Use these runbooks for target and permission details:

- [Terraform State Collector](../../services/collector-terraform-state.md)
- [AWS Cloud Collector](../../services/collector-aws-cloud.md)
- [Collector Runtime Services](../../deployment/service-runtimes-collectors.md)

## Webhook listener

The webhook listener verifies provider secrets and writes durable refresh
triggers to Postgres. It does not mount the repository workspace PVC and does
not connect to the graph backend.

| Value | Default | Operator note |
| --- | --- | --- |
| `webhookListener.enabled` | `false` | Renders the listener Deployment and Service. |
| `webhookListener.maxBodyBytes` | `1048576` | Maximum accepted body size. |
| `webhookListener.defaultBranch` | empty | Optional fallback branch. |
| `webhookListener.github.enabled` | `false` | Enables the GitHub path. |
| `webhookListener.github.path` | `/webhooks/github` | GitHub request path. |
| `webhookListener.github.secretName` | empty | Required when GitHub is enabled. |
| `webhookListener.github.secretKey` | `secret` | Secret key for GitHub verification. |
| `webhookListener.gitlab.enabled` | `false` | Enables the GitLab path. |
| `webhookListener.gitlab.path` | `/webhooks/gitlab` | GitLab request path. |
| `webhookListener.gitlab.secretName` | empty | Required when GitLab is enabled. |
| `webhookListener.gitlab.tokenKey` | `token` | Secret key for GitLab verification. |
| `webhookListener.bitbucket.enabled` | `false` | Enables the Bitbucket path. |
| `webhookListener.bitbucket.path` | `/webhooks/bitbucket` | Bitbucket request path. |
| `webhookListener.bitbucket.secretName` | empty | Required when Bitbucket is enabled. |
| `webhookListener.bitbucket.secretKey` | `secret` | Secret key for Bitbucket verification. |
| `webhookListener.awsFreshness.enabled` | `false` | Enables the AWS freshness path. |
| `webhookListener.awsFreshness.path` | `/webhooks/aws/eventbridge` | AWS freshness request path. |
| `webhookListener.awsFreshness.secretName` | empty | Required when AWS freshness is enabled. |
| `webhookListener.awsFreshness.tokenKey` | `token` | Secret key for AWS freshness verification. |
| `webhookListener.exposure.ingress.enabled` | `false` | Renders provider-only Ingress paths. |
| `webhookListener.exposure.ingress.hosts` | `[]` | Host list for webhook routing. |

When `webhookListener.enabled=true`, at least one provider must be enabled.
Webhook ingress renders only the enabled provider paths as `Exact` paths to the
webhook listener Service.

## Shared knobs

All optional collector and webhook workloads support `replicas`,
`revisionHistoryLimit`, `resources`, `connectionTuning.postgres.*`, global
`podLabels`, global `podAnnotations`, and global scheduling values. Workflow
coordinator also supports `connectionTuning.neo4j.*`.

Global `env` is rendered for workflow coordinator and collector Deployments,
but not for the webhook listener. Workload-specific `env` exists for workflow
coordinator only in this group.

## Render checks

Run the default chart checks plus focused overlays for any enabled optional
runtime:

```bash
helm lint ./deploy/helm/eshu
helm template eshu ./deploy/helm/eshu
helm template eshu ./deploy/helm/eshu -f values.collectors.yaml
helm template eshu ./deploy/helm/eshu -f values.webhooks.yaml
```

The render fails for common mistakes such as inactive workflow coordination for
claim-driven collectors, empty collector instance lists, missing Confluence or
Terraform-state required values, OCI registry with no targets, webhook listener
with no provider, or provider enablement without its Secret name.

## Related docs

- [Helm Values](helm-values.md)
- [Runtime Values](helm-runtime-values.md)
- [Routing And Storage Values](helm-routing-and-storage-values.md)
