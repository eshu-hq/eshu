# Helm Collector And Webhook Values

Use this page for optional collectors, workflow coordination, and webhook
intake. Runtime ownership lives in
[Collector Runtime Services](../../deployment/service-runtimes-collectors.md).

## Runtime Blocks

| Block | Runtime | Work source | Default |
| --- | --- | --- | --- |
| `workflowCoordinator` | Workflow coordinator | collector instance config and claim scheduling | disabled |
| `webhookListener` | Webhook listener | GitHub, GitLab, Bitbucket, AWS freshness deliveries | disabled |
| `confluenceCollector` | Confluence collector | direct Confluence crawl scope | disabled |
| `ociRegistryCollector` | OCI registry collector | direct target list | disabled |
| `terraformStateCollector` | Terraform-state collector | workflow claims | disabled |
| `awsCloudCollector` | AWS cloud collector | workflow claims | disabled |
| `packageRegistryCollector` | Package-registry collector | workflow claims | disabled |

Direct collectors render from their own enabled block and required target
values. Claim-driven collectors also require active workflow coordination.

## Claim-Driven Contract

Terraform-state, AWS cloud, and package-registry collectors require:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`
- `workflowCoordinator.collectorInstances` with at least one instance
- `<collector>.collectorInstances` with at least one instance
- non-empty `<collector>.instanceId`

The coordinator receives its instance list as `ESHU_COLLECTOR_INSTANCES_JSON`.
Each claim-driven collector also receives its own `ESHU_COLLECTOR_INSTANCES_JSON`;
keep the selected `instanceId` aligned with that list.

## Collector Values

| Collector | Required when enabled | Notes |
| --- | --- | --- |
| Confluence | `baseUrl`, credentials Secret, exactly one of `spaceId`, `spaceIds`, `rootPageId` | Read-only direct crawler. |
| OCI registry | at least one target with `provider` and `repository` | Use target env fields or `extraEnv` Secret refs for credentials. |
| Terraform state | `instanceId`, `collectorInstances`, redaction Secret, redaction key key, `redaction.rulesetVersion` | Redaction env is mandatory. See [Terraform State Collector](../../services/collector-terraform-state.md). |
| AWS cloud | `instanceId`, `collectorInstances` | Use `serviceAccount.*` for IRSA. Redaction Secret is optional in Helm but required by the binary when ECS or Lambda scans are enabled. See [AWS Cloud Collector](../../services/collector-aws-cloud.md). |
| Package registry | `instanceId`, `collectorInstances` | Claim-driven package metadata fetch. |

All optional collectors support `replicas`, `revisionHistoryLimit`, `resources`,
Postgres connection tuning, global pod labels/annotations, and global
scheduling values.

## Webhook Listener

The webhook listener verifies provider secrets and writes durable refresh
triggers to Postgres. It does not mount the repository workspace PVC and does
not connect to the graph backend.

Defaults: disabled, `maxBodyBytes=1048576`, empty `defaultBranch`, all
providers disabled, provider paths `/webhooks/github`, `/webhooks/gitlab`,
`/webhooks/bitbucket`, and `/webhooks/aws/eventbridge`.

When enabled, at least one provider must be enabled and each enabled provider
needs its Secret name. Webhook ingress renders only enabled provider paths as
`Exact` paths to the webhook listener Service.

## Render Checks

Rendering fails for inactive workflow coordination with claim-driven collectors,
empty collector instance lists, missing Confluence or Terraform-state required
values, OCI registry with no targets, webhook listener with no provider, and
enabled webhook providers without Secret names.

## Related Docs

- [Helm Values](helm-values.md)
- [Runtime Values](helm-runtime-values.md)
- [Routing And Storage Values](helm-routing-and-storage-values.md)
