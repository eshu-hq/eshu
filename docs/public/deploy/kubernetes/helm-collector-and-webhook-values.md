# Helm Collector And Webhook Values

Use this page for optional collectors, workflow coordination, and webhook
intake. Runtime ownership lives in
[Collector Runtime Services](../../deployment/service-runtimes-collectors.md).

## Runtime Blocks

| Block | Runtime | Work source | Default |
| --- | --- | --- | --- |
| `workflowCoordinator` | Workflow coordinator | collector instance config and claim scheduling | disabled |
| `webhookListener` | Webhook listener | GitHub, GitLab, Bitbucket, AWS, PagerDuty, and Jira freshness deliveries | disabled |
| `confluenceCollector` | Confluence collector | direct Confluence crawl scope | disabled |
| `ociRegistryCollector` | OCI registry collector | direct target list | disabled |
| `terraformStateCollector` | Terraform-state collector | workflow claims | disabled |
| `awsCloudCollector` | AWS cloud collector | workflow claims | disabled |
| `gcpCloudCollector` | GCP cloud collector | workflow claims | disabled |
| `azureCloudCollector` | Azure cloud collector | workflow claims | disabled |
| `packageRegistryCollector` | Package-registry collector | workflow claims | disabled |
| `sbomAttestationCollector` | SBOM-attestation collector | workflow claims | disabled |
| `securityAlertCollector` | Provider security-alert collector | workflow claims | disabled |
| `pagerDutyCollector` | PagerDuty incident-context collector | workflow claims | disabled |
| `jiraCollector` | Jira work-item collector | workflow claims | disabled |
| `vulnerabilityIntelligenceCollector` | Vulnerability-intelligence collector | workflow claims | disabled |
| `componentExtensionCollector` | Component extension collector host | component-registry claims | disabled |

Direct collectors render from their own enabled block and required target
values. Claim-driven collectors also require active workflow coordination.
Community extension collectors must also pass the
[Hosted Extension Operator Policy](../../operate/hosted-extension-policy.md):
the chart can run the component extension host, but the full hosted-extension
policy document remains operator-owned. Use `componentExtensionCollector`
values for the mounted registry path, trust allowlist, and extension egress
policy JSON that both the workflow coordinator and worker must share.

## Claim-Driven Contract

Terraform-state, AWS cloud, GCP cloud, Azure cloud, package-registry,
SBOM-attestation, provider
security-alert, PagerDuty, Jira, and vulnerability-intelligence collectors
require:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`
- `workflowCoordinator.collectorInstances` with at least one instance
- `<collector>.collectorInstances` with at least one instance
- non-empty `<collector>.instanceId`

The coordinator receives its instance list as `ESHU_COLLECTOR_INSTANCES_JSON`.
Each claim-driven collector also receives its own `ESHU_COLLECTOR_INSTANCES_JSON`;
keep the selected `instanceId` aligned with that list.

`componentExtensionCollector` is also claim-driven, but its instance source is
the trusted component registry mounted at `componentHome`, not a static
collector instance list. When it is enabled, the chart passes the same
component home, trust settings, and `extensionEgressPolicyJSON` to the
workflow coordinator and the worker. Set `trustMode=allowlist` with `allowIds`
and `allowPublishers` values that each contain at least one non-empty CSV token,
so the rendered worker can trust at least one component activation. Mount the
same registry volume into both workloads with
`workflowCoordinator.extraVolumes` /
`workflowCoordinator.extraVolumeMounts` and
`componentExtensionCollector.extraVolumes` /
`componentExtensionCollector.extraVolumeMounts`.

## Collector Values

| Collector | Required when enabled | Notes |
| --- | --- | --- |
| Confluence | `baseUrl`, credentials Secret, exactly one of `spaceId`, `spaceIds`, `rootPageId` | Read-only direct crawler. |
| OCI registry | at least one target with `provider` and `repository` | Use target env fields or `extraEnv` Secret refs for credentials. |
| Terraform state | `instanceId`, `collectorInstances`, redaction Secret, redaction key key, `redaction.rulesetVersion` | Redaction env is mandatory. See [Terraform State Collector](../../services/collector-terraform-state.md). |
| AWS cloud | `instanceId`, `collectorInstances` | Use `serviceAccount.*` for IRSA. Redaction Secret is optional in Helm but required by the binary when ECS, Lambda, or Security Hub scans are enabled. See [AWS Cloud Collector](../../services/collector-aws-cloud.md). |
| GCP cloud | `instanceId`, `collectorInstances` with a matching enabled `gcp` instance, `live_collection_enabled=true`, at least one enabled scope with `credential_ref`, and redaction Secret/key | Uses `serviceAccount.*` for Workload Identity annotations. Helm mounts redaction material as a read-only Secret file and starts `-mode claimed-live`; credential values must stay out of values. |
| Azure cloud | `instanceId`, `collectorInstances` with a matching enabled `azure` instance, `live_collection_enabled=true`, at least one enabled scope with `credential_ref`, and redaction Secret/key; `workflowCoordinator.collectorInstances` must also carry the enabled claim-driven `azure` instance | Uses `serviceAccount.*` for Workload Identity annotations. Helm mounts redaction material as a read-only Secret file and starts `-mode claimed-live`; the live credential is the ambient workload identity, so credential values must stay out of values. Claimed-live serves the `resource_graph` lane only. |
| Package registry | `instanceId`, `collectorInstances` | Claim-driven package metadata fetch. |
| SBOM attestation | `instanceId`, `collectorInstances` with a `sbom_attestation` instance matching `instanceId`, `workflowCoordinator.collectorInstances` with an enabled claim-driven `sbom_attestation` instance | Fetches configured HTTP(S) SBOM documents or OCI referrer blobs and emits typed source facts. Attachment, subject mismatch, parse warnings, and verification status are reducer/read-surface concerns. |
| Security alert | `instanceId`, `collectorInstances` with a `security_alert` instance matching `instanceId`, `workflowCoordinator.collectorInstances` with an enabled claim-driven `security_alert` instance | Fetches bounded GitHub Dependabot repository alert pages and emits only `security_alert.repository_alert` source facts. Target `token_env` values must resolve from `extraEnv` Secret refs; any `api_base_url` override must use HTTPS; repository names and tokens should stay out of public values files. |
| PagerDuty | `instanceId`, `collectorInstances` with a `pagerduty` instance matching `instanceId`, `workflowCoordinator.collectorInstances` with an enabled claim-driven `pagerduty` instance | Fetches bounded PagerDuty incidents, log entries, related change events, and optional live service/integration config facts. Target `token_env` values must resolve from `extraEnv` Secret refs; any `api_base_url` override must use HTTPS; incident titles, service names, integration names, routing keys, PagerDuty URLs, and tokens should stay out of public values files. |
| Jira | `instanceId`, `collectorInstances` with a `jira` instance matching `instanceId`, `workflowCoordinator.collectorInstances` with an enabled claim-driven `jira` instance | Polling-only mode enables `jiraCollector` and passes `token_env` plus optional `email_env` through `extraEnv` Secret refs. Webhook-enabled mode also enables `webhookListener.jira` with a matching `scopeId`; webhooks are freshness triggers only and polling remains the recovery path. |
| Vulnerability intelligence | `instanceId`, `collectorInstances` with a `vulnerability_intelligence` instance matching `instanceId`, `workflowCoordinator.collectorInstances` with an enabled claim-driven `vulnerability_intelligence` instance | Bounded source targets only (explicit CVE IDs, source snapshots, OSV package-version queries, NVD windows, or derived owned-package targets). API keys are referenced from `extraEnv` Secret refs via `api_key_env` and never embedded in values. |
| Component extension | `componentHome`, `trustMode=allowlist`, `allowIds` with at least one non-empty CSV token, `allowPublishers` with at least one non-empty CSV token, `extensionEgressPolicyJSON`, shared registry volume mounts for coordinator and worker | Runs verified claim-capable component activations through `/usr/local/bin/eshu-collector-component-extension`. Keep component config files, provider targets, and credentials in private mounts or Secrets; extension facts still enter through the collector commit boundary and reducers own graph truth. Strict trust mode is not charted until provenance verifier values are first-class chart inputs. |

All optional collectors support `replicas`, `revisionHistoryLimit`, `resources`,
Postgres connection tuning, global pod labels/annotations, and global
scheduling values.

## Webhook Listener

The webhook listener verifies provider secrets and writes durable refresh
triggers to Postgres. It does not mount the repository workspace PVC and does
not connect to the graph backend.

Defaults: disabled, `maxBodyBytes=1048576`, empty `defaultBranch`, all
providers disabled, provider paths `/webhooks/github`, `/webhooks/gitlab`,
`/webhooks/bitbucket`, `/webhooks/aws/eventbridge`,
`/webhook/gcp-freshness`, `/webhooks/pagerduty`, and `/webhooks/jira`.

When enabled, at least one provider must be enabled and each enabled provider
needs its Secret name. PagerDuty and Jira also require `scopeId`, which must
match the configured claim-driven collector target allowed to receive the
freshness wake-up. Webhook ingress renders only enabled provider paths as
`Exact` paths to the webhook listener Service.

`webhookListener.gcpFreshness` gates the GCP Cloud Asset Inventory (CAI)
change-feed intake route (`/webhook/gcp-freshness` by default). It is
default-off like every other provider and requires `secretName` (the shared
push token, mounted read-only via `secretKeyRef`) once enabled.

Current auth posture: the route accepts two independent, fail-closed auth
paths — either is sufficient, and enabling one never weakens the other.

- **Shared bearer token** (`X-Eshu-GCP-Freshness-Token` or
  `Authorization: Bearer`), compared with a constant-time `hmac.Equal` check
  performed before the request body is read. Fails closed when no token is
  configured.
- **Pub/Sub push OIDC** (`webhookListener.gcpFreshness.oidc`): validates the
  Google-signed push token's signature against Google's public certs (via
  `google.golang.org/api/idtoken`), checks the `aud` claim against
  `oidc.audience`, and checks the token's `email` claim against
  `oidc.allowedServiceAccountEmail` with `email_verified=true`. Fails closed
  on any missing token, signature failure, audience mismatch, unverified
  email, or disallowed principal. Default-off (`oidc.enabled: false`); setting
  it renders `ESHU_GCP_FRESHNESS_OIDC_AUDIENCE` and
  `ESHU_GCP_FRESHNESS_OIDC_ALLOWED_SA` as plain env values (these are a
  reference audience URL and a service-account email, never secret
  material — no `secretKeyRef` is needed for them).

If neither path is configured, the route is not mounted at all (no
regression from the prior shared-token-only behavior). See
[GCP Change Feed Operator Setup](gcp-freshness-operator-setup.md) for the
end-to-end provisioning walkthrough, including when a push-forwarder is still
useful versus pointing a native push subscription directly at this route.

## Render Checks

Rendering fails for inactive workflow coordination with claim-driven collectors,
empty collector instance lists, missing Confluence or Terraform-state required
values, OCI registry with no targets, webhook listener with no provider,
enabled webhook providers without Secret names, and PagerDuty or Jira webhook
providers without `scopeId`. For GCP cloud, Azure cloud, SBOM-attestation, provider
security-alert,
PagerDuty, Jira, and vulnerability-intelligence collectors, rendering
additionally fails when the
collector-local instance list does not contain a matching enabled claim-driven
instance, when required GCP or Azure live scope fields are absent, or when
`workflowCoordinator.collectorInstances` does not contain an enabled
claim-driven instance for that collector kind. Component extension rendering
fails if the workflow coordinator is not active with claims enabled, if
`componentHome` is empty, if trust mode is not `allowlist`, if the extension
egress policy JSON is missing, or if the coordinator and worker do not both
mount `componentHome`.

## Related Docs

- [Helm Values](helm-values.md)
- [Runtime Values](helm-runtime-values.md)
- [Routing And Storage Values](helm-routing-and-storage-values.md)
