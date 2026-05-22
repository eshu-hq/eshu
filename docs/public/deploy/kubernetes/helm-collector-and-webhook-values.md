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

## Workflow coordinator

Defaults: disabled, `deploymentMode=dark`, `claimsEnabled=false`, empty
`collectorInstances`, one replica, empty `env`, and empty Postgres/Bolt
connection-tuning values. Claims require active mode.

## Direct collectors

### Confluence

Defaults: disabled, empty `baseUrl`, empty crawl scope, empty `spaceKey`, empty
`pageLimit`, `pollInterval=5m`, empty credentials Secret, `emailKey=email`,
`apiTokenKey=api-token`, and empty bearer token key. When enabled, the chart
requires credentials and exactly one crawl scope: `spaceId`, `spaceIds`, or
`rootPageId`.

### OCI registry

Defaults: disabled, `instanceId=oci-registry-primary`, `pollInterval=5m`, no
targets, no default AWS region, and no `extraEnv`. When enabled, at least one
target is required. Each target needs `provider` and `repository`; the schema
allows `jfrog`, `ecr`, `dockerhub`, and `ghcr`. Use target-level `*_env`
fields plus `extraEnv` Secret references for credentials; do not put static
passwords in values.

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

Defaults: disabled, one replica, `maxBodyBytes=1048576`, empty
`defaultBranch`, all providers disabled, provider paths
`/webhooks/github`, `/webhooks/gitlab`, `/webhooks/bitbucket`, and
`/webhooks/aws/eventbridge`, empty provider Secret names, Secret keys
`secret` for GitHub/Bitbucket and `token` for GitLab/AWS freshness, disabled
webhook Ingress, and no webhook hosts.

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

The render fails for common mistakes such as inactive workflow coordination for
claim-driven collectors, empty collector instance lists, missing Confluence or
Terraform-state required values, OCI registry with no targets, webhook listener
with no provider, or provider enablement without its Secret name.

## Related docs

- [Helm Values](helm-values.md)
- [Runtime Values](helm-runtime-values.md)
- [Routing And Storage Values](helm-routing-and-storage-values.md)
