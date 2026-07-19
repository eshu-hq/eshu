# Collector Environment

This page covers workflow coordinator, claim-driven collectors, direct
collector targets, and webhook listener variables.

## Workflow Coordinator

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE` | `dark` | workflow coordinator | Coordinator mode: `dark` or `active`. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED` | `false` | workflow coordinator | Enables workflow claims. |
| `ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS` | `false` | workflow coordinator | Backward-compatible claims flag. Prefer `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED`. |
| `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL` | `30s` | workflow coordinator | Desired collector-instance and scheduled-work planning interval. |
| `ESHU_WORKFLOW_COORDINATOR_RUN_RECONCILE_INTERVAL` | `30s` | workflow coordinator | Workflow-run status and completeness reconciliation interval. |
| `ESHU_WORKFLOW_COORDINATOR_REAP_INTERVAL` | workflow default | workflow coordinator | Expired-claim reap interval. |
| `ESHU_WORKFLOW_COORDINATOR_CLAIM_LEASE_TTL` | workflow default | workflow coordinator | Collector claim TTL. Must exceed heartbeat interval. |
| `ESHU_WORKFLOW_COORDINATOR_HEARTBEAT_INTERVAL` | workflow default | workflow coordinator | Collector claim heartbeat interval. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_LIMIT` | workflow default | workflow coordinator | Max expired claims reaped per cycle. |
| `ESHU_WORKFLOW_COORDINATOR_EXPIRED_CLAIM_REQUEUE_DELAY` | workflow default | workflow coordinator | Delay before requeueing expired work. |
| `ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON` | unset | workflow coordinator | Optional hosted collector scheduling egress policy. JSON mode is `restricted` or `broad`; restricted mode requires explicit `collector_kind` allow rules before active-mode claim-capable collectors can plan scheduled or freshness work, deny rules win, and broad mode must not include collector-specific rules. |
| `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` | unset | workflow coordinator | Hosted component-extension scheduling egress policy. Missing policy denies component-extension work; JSON mode is `restricted` or `broad`; restricted mode requires explicit component allow rules before active-mode component-extension work can be planned, deny rules win, and broad mode must not include extension-specific rules. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | unset | workflow coordinator and claim-aware collectors | Desired collector instance list. Claim-driven runtimes select enabled claim-capable instances from this JSON. |

Active coordinator mode is guarded. The process rejects
`ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=active` unless
`ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true` and at least one enabled
collector instance has `claims_enabled: true`.

No-Regression Evidence: `go test ./internal/coordinator -run 'Test(ParseCollectorEgressPolicyJSON|CollectorEgressPolicy|LoadConfigParsesCollectorEgressPolicy|ServiceRunActiveModeSkipsDeniedCollectorEgress|ServiceIncidentFreshnessSkipsDeniedCollectorEgress)' -count=1` proves collector egress policy parsing, restricted default-deny behavior, deny-over-allow precedence, broad-mode validation, config loading, scheduled work suppression, and incident freshness suppression. The gate filters scheduler inputs only; it does not change claim lease timing, worker counts, queue ordering, reducer graph writes, fact emission, or provider API calls.

Observability Evidence: denied collector egress creates no claimable row and
reuses coordinator reconcile metrics, workflow rows, claim status, and
`/api/v0/index-status`. The bounded structured log includes only
`collector_kind` and low-cardinality `reason`.

## Terraform State Collector

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_TFSTATE_REDACTION_KEY` | unset | collector-terraform-state | Deployment-scoped secret material for deterministic redaction markers. Required before parsing Terraform state. |
| `ESHU_TFSTATE_REDACTION_RULESET_VERSION` | unset | collector-terraform-state | Non-empty version string for the redaction rule set. Blank values fail startup. |
| `ESHU_TFSTATE_REDACTION_SENSITIVE_KEYS` | `password,secret,token,access_key,private_key,certificate,key_pair` | collector-terraform-state | Comma-separated leaf attribute keys treated as redacted secrets. |
| `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID` | required when more than one enabled Terraform-state instance exists | collector-terraform-state | Selects the claim-capable `terraform_state` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `ESHU_TFSTATE_COLLECTOR_OWNER_ID` | host/process-derived | collector-terraform-state | Owner label written into workflow claim rows. |
| `ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL` | `1s` | collector-terraform-state | Delay between empty claim polls. |
| `ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-terraform-state | Lease TTL used when claiming and refreshing work. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-terraform-state | Heartbeat interval for active workflow claims. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT` | workflow default | collector-terraform-state | Backward-compatible alias for `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL`. |
| `ESHU_TFSTATE_SOURCE_MAX_BYTES` | reader default | collector-terraform-state | Max bytes read from one local or S3 state source. |
| `ESHU_TERRAFORM_SCHEMA_DIR` | packaged schema default | collector-terraform-state | Optional override for the Terraform provider-schema bundle directory. |

## AWS Cloud Collector

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_AWS_COLLECTOR_INSTANCE_ID` | required when more than one enabled AWS instance exists | collector-aws-cloud | Selects the claim-capable `aws` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `ESHU_AWS_COLLECTOR_OWNER_ID` | host/process-derived | collector-aws-cloud | Owner label written into workflow claim rows. |
| `ESHU_AWS_COLLECTOR_POLL_INTERVAL` | `1s` | collector-aws-cloud | Delay between empty claim polls. |
| `ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-aws-cloud | Lease TTL used when claiming and refreshing work. |
| `ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-aws-cloud | Heartbeat interval for active workflow claims. |
| `ESHU_AWS_REDACTION_KEY` | unset | collector-aws-cloud | Deployment-scoped key for Batch container-environment, CloudWatch alarm dimension, CodeBuild environment-variable PLAINTEXT value, Cognito free-text, ECS task-definition, Lambda environment, Security Hub action-target, and Organizations account redaction markers. Required when a target scope enables `batch`, `cloudwatch`, `codebuild`, `cognito`, `ecs`, `lambda`, `organizations`, or `securityhub`. |

## GCP Cloud Collector

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_GCP_COLLECTOR_INSTANCE_ID` | required when more than one enabled GCP instance exists | collector-gcp-cloud | Selects the claim-capable `gcp` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `ESHU_GCP_COLLECTOR_OWNER_ID` | host/process-derived | collector-gcp-cloud | Owner label written into workflow claim rows. |
| `ESHU_GCP_COLLECTOR_POLL_INTERVAL` | `1s` | collector-gcp-cloud | Delay between empty claim polls. |
| `ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-gcp-cloud | Lease TTL used when claiming and refreshing work. |
| `ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-gcp-cloud | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_GCP_COLLECTOR_QUOTA_PROJECT_ID` | unset | collector-gcp-cloud | Optional billing/quota project id sent as `x-goog-user-project` on Cloud Asset Inventory requests. Leave unset for service-account/Workload Identity Federation ADC. Set it when the resolved ADC is a user credential (e.g. local `gcloud auth application-default login`), which otherwise gets a 403 quota-project error. Name/id only, never credential material. |

The GCP collector also requires `-mode claimed-live` and
`-redaction-key-file` at process startup. Helm supplies the redaction key path
from `gcpCloudCollector.redaction.*` as a read-only Secret volume mount. The
collector instance configuration must set `live_collection_enabled=true`, and
enabled scopes must reference credentials by name with `credential_ref`.

## Azure Cloud Collector

The Azure collector runs in fixture mode by default. Fixture mode reads
`ESHU_AZURE_TARGETS_JSON` (and the optional `ESHU_AZURE_FIXTURE_PAGES_JSON` /
`ESHU_AZURE_REDACTION_KEY_FILE`). The claim-driven environment below applies to
`-mode claimed-live`.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_AZURE_COLLECTOR_INSTANCE_ID` | required when more than one enabled Azure instance exists | collector-azure-cloud | Selects the claim-capable `azure` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. |
| `ESHU_AZURE_COLLECTOR_OWNER_ID` | host/process-derived | collector-azure-cloud | Owner label written into workflow claim rows. |
| `ESHU_AZURE_POLL_INTERVAL` | `5m` | collector-azure-cloud | Delay between empty claim polls. |
| `ESHU_AZURE_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-azure-cloud | Lease TTL used when claiming and refreshing work. |
| `ESHU_AZURE_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-azure-cloud | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |

The Azure collector also requires `-mode claimed-live` and
`-redaction-key-file` at process startup. Helm supplies the redaction key path
from `azureCloudCollector.redaction.*` as a read-only Secret volume mount. The
collector instance configuration must set `live_collection_enabled=true`, and
enabled scopes must reference credentials by name with `credential_ref`.
Claimed-live serves the `resource_graph` source lane only; the live credential
is the ambient Azure workload identity.

## Vault Live Collector

The Vault live collector is claim-only. It selects an enabled `vault_live`
instance from `ESHU_COLLECTOR_INSTANCES_JSON`, resolves each target's
read-only token from `token_env`, calls metadata-only Vault endpoints, and
commits `secrets_iam_posture` source facts. It never reads KV `/data` paths or
secret values. `ESHU_VAULT_LIVE_REDACTION_KEY` is required because Vault paths,
names, accessors, aliases, and policy hashes can reveal trust topology; those
values are persisted as deterministic HMAC markers.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID` | required when more than one enabled Vault live instance exists | collector-vault-live | Selects the claim-capable `vault_live` instance. |
| `ESHU_VAULT_LIVE_REDACTION_KEY` | unset | collector-vault-live | Deployment-scoped key for deterministic Vault metadata markers. Required. |
| `ESHU_VAULT_LIVE_POLL_INTERVAL` | `5m` | collector-vault-live | Delay between empty workflow-claim polls. |
| `ESHU_VAULT_LIVE_CLAIM_LEASE_TTL` | workflow default | collector-vault-live | Lease TTL used when claiming and refreshing work. |
| `ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL` | workflow default | collector-vault-live | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID` | host/process-derived | collector-vault-live | Owner label written into workflow claim rows. |

Vault tokens must come from private environment variables referenced by
`token_env`; do not commit token values, private Vault URLs, Vault paths, policy
names, entity names, alias names, or copied provider payloads to public values
files or docs.

## OCI Registry Collector

The OCI collector supports direct targets and claim-aware mode.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID` | required for direct mode; required when multiple enabled OCI claim instances exist | collector-oci-registry | Names or selects the OCI collector instance. |
| `ESHU_OCI_REGISTRY_TARGETS_JSON` | unset | collector-oci-registry direct mode | JSON array of provider targets for direct registry polling. |
| `ESHU_OCI_REGISTRY_POLL_INTERVAL` | `5m` in direct mode; `1s` in claim-aware mode | collector-oci-registry | Delay between direct scans or empty workflow-claim polls. |
| `ESHU_OCI_REGISTRY_CLAIM_LEASE_TTL` | `60s` | collector-oci-registry claim-aware mode | Lease TTL used when claiming and refreshing OCI registry work. |
| `ESHU_OCI_REGISTRY_HEARTBEAT_INTERVAL` | `20s` | collector-oci-registry claim-aware mode | Heartbeat interval for active workflow claims. |
| `ESHU_OCI_REGISTRY_COLLECTOR_OWNER_ID` | host/process-derived | collector-oci-registry claim-aware mode | Owner label written into workflow claim rows. |

OCI target JSON may reference JFrog, ECR, Docker Hub, GHCR, Harbor, Google
Artifact Registry, or Azure Container Registry. Credential fields name
environment variables; the collector reads the secret values from those named
variables instead of storing credentials in facts or status.

Each target may also customize transport trust for registries served with a
private or self-signed CA, such as a local `registry:2` over TLS used to prove
the supply-chain image-identity hop without a cloud account:

| Field | Default | Purpose |
| --- | --- | --- |
| `tls_ca_cert_path` | unset | Filesystem path to a PEM bundle trusted in addition to the system pool. Use this to scan a registry served with a private or self-signed CA. |
| `tls_insecure_skip_verify` | `false` | Disables server certificate verification for this target. Test/local-only, never a production default, and rejected when `tls_ca_cert_path` is also set so trust cannot be silently weakened. |

Prefer `tls_ca_cert_path` over `tls_insecure_skip_verify`: pinning a CA bundle
keeps certificate verification on. When no TLS field is set the collector uses
the system trust pool, the safe default for public cloud registries. The
resolved trust posture is surfaced as a low-cardinality `tls_mode`
(`system_roots`, `custom_ca`, or `insecure_skip_verify`) on OCI scan spans and
logs, and a misconfigured CA bundle fails the scan loudly rather than silently
falling back to system trust.

## Package Registry Collector

The package-registry collector is claim-only. It selects an enabled
`package_registry` instance from `ESHU_COLLECTOR_INSTANCES_JSON`.
Collector configuration may define explicit `targets` or enable
`derive_from_owned_packages` so the coordinator derives bounded npm metadata
targets from active owned Git dependency facts. Derived collection is currently
npm only and still uses workflow claims. The default derived target limit is
100; full-corpus deployments can raise `derive_from_owned_packages.target_limit`
up to 5000. Package-registry derivation uses one target per package identity,
not one target per observed version. When `derive_from_owned_packages.version_limit`
is omitted, derived npm targets default to one version so full-corpus
vulnerability enrichment records package identity without projecting every
registry version and dependency edge for heavily reused packages. Explicit
package-registry targets can still request higher version limits for targeted
registry metadata exploration.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID` | required when more than one enabled package-registry instance exists | collector-package-registry | Selects the claim-capable `package_registry` instance. |
| `ESHU_PACKAGE_REGISTRY_POLL_INTERVAL` | `1s` | collector-package-registry | Delay between empty workflow-claim polls. |
| `ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL` | `60s` | collector-package-registry | Lease TTL used when claiming and refreshing work. |
| `ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL` | `20s` | collector-package-registry | Heartbeat interval for active workflow claims. |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID` | host/process-derived | collector-package-registry | Owner label written into workflow claim rows. |

## SBOM Attestation Collector

The SBOM attestation collector is claim-only. It selects an enabled
`sbom_attestation` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. Collector
configuration defines explicit `targets` with `source_type=configured_source`
for HTTP(S) document URLs or `source_type=oci_referrer` for registry referrer
documents. Supported document formats are CycloneDX, SPDX, and in-toto
statements.

`oci_referrer` targets set `provider`, `registry`, `repository`,
`subject_digest`, and `referrer_digest`. Static-credential providers read
secrets from the `username_env`, `password_env`, and `bearer_token_env`
variables. A `provider=ecr` target needs no static credentials: the collector
mints short-lived OCI Distribution basic-auth from the AWS
`GetAuthorizationToken` exchange using the AWS default credential chain, the
same auth path the OCI registry collector uses. Optional target fields `region`
and `aws_profile` select the AWS config, and `registry_host` overrides the
registry host when it differs from `registry`. Decoded ECR tokens are used only
as request credentials and are never logged.

Collectors emit typed source facts only. Reducer-owned
`reducer_sbom_attestation_attachment` facts decide whether a document subject
is attached, mismatched, unverified, parse-only, unknown, ambiguous, or
unparseable. Signature verification status remains separate from subject
attachment status.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID` | required when more than one enabled SBOM-attestation instance exists | collector-sbom-attestation | Selects the claim-capable `sbom_attestation` instance. |
| `ESHU_SBOM_ATTESTATION_POLL_INTERVAL` | `1s` | collector-sbom-attestation | Delay between empty workflow-claim polls. |
| `ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL` | `60s` | collector-sbom-attestation | Lease TTL used when claiming and refreshing work. |
| `ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL` | `20s` | collector-sbom-attestation | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID` | host/process-derived | collector-sbom-attestation | Owner label written into workflow claim rows. |

## Security Alert Collector

The security-alert collector is claim-only. It selects an enabled
`security_alert` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. Provider
targets support GitHub Dependabot alerts in one of two scopes, selected by the
optional `scope` field (default `repository`):

- `scope: "repository"` polls one repository via
  `GET /repos/{owner}/{repo}/dependabot/alerts` and must include `token_env`,
  `repository`, and `allowed_repositories`.
- `scope: "org"` polls one organization via
  `GET /orgs/{org}/dependabot/alerts` and must include `token_env` and
  `organization`. It must not set `repository` or `allowed_repositories`. A
  single org target fans the response out into per-repository
  `security_alert.repository_alert` facts (one fact per repository that owns an
  alert), so reducer reconciliation is identical to the per-repository path.

The runtime resolves the credential from the named environment variable and
emits only `security_alert.repository_alert` facts. Optional `api_base_url`
overrides must use HTTPS because the runtime sends the bearer token to that
endpoint.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_SECURITY_ALERT_COLLECTOR_INSTANCE_ID` | required when more than one enabled security-alert instance exists | collector-security-alerts | Selects the claim-capable `security_alert` instance. |
| `ESHU_SECURITY_ALERT_POLL_INTERVAL` | `1s` | collector-security-alerts | Delay between empty workflow-claim polls. |
| `ESHU_SECURITY_ALERT_CLAIM_LEASE_TTL` | `60s` | collector-security-alerts | Lease TTL used when claiming and refreshing work. |
| `ESHU_SECURITY_ALERT_HEARTBEAT_INTERVAL` | `20s` | collector-security-alerts | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_SECURITY_ALERT_COLLECTOR_OWNER_ID` | host/process-derived | collector-security-alerts | Owner label written into workflow claim rows. |

Provider tokens must come from private environment variables referenced by
`token_env`; do not commit token values, private repository names, alert URLs,
or copied provider payloads to public values files or docs.

`eshu-collector-security-alerts --preflight-provider-access` can be run as a
one-shot access check before starting workflow fanout. It uses the same
collector instance JSON, token env resolution, repository allowlist, and
provider client as the hosted runtime, makes one bounded request per target
(the per-repository endpoint for repository targets and the org-wide endpoint
for org targets), and reports only sanitized failure classes.

## CI/CD Run Collector

The CI/CD run collector is claim-only. It selects an enabled `ci_cd_run`
instance from `ESHU_COLLECTOR_INSTANCES_JSON`. GitHub Actions targets must
include `token_env`, `repository`, `allowed_repositories`, and bounded
`max_runs`, `max_jobs`, and `max_artifacts` limits. The runtime resolves the
credential from the named environment variable and emits only `ci.*` source
facts. Optional `api_base_url` overrides must use HTTPS because the runtime
sends the bearer token to that endpoint.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` | required when more than one enabled CI/CD run instance exists | collector-cicd-run | Selects the claim-capable `ci_cd_run` instance. |
| `ESHU_CICD_RUN_POLL_INTERVAL` | `1s` | collector-cicd-run | Delay between empty workflow-claim polls. |
| `ESHU_CICD_RUN_CLAIM_LEASE_TTL` | `60s` | collector-cicd-run | Lease TTL used when claiming and refreshing work. |
| `ESHU_CICD_RUN_HEARTBEAT_INTERVAL` | `20s` | collector-cicd-run | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_CICD_RUN_COLLECTOR_OWNER_ID` | host/process-derived | collector-cicd-run | Owner label written into workflow claim rows. |

Provider tokens must come from private environment variables referenced by
`token_env`; do not commit token values, private repository names, run URLs,
artifact names, or copied provider payloads to public values files or docs.

## PagerDuty Collector

The PagerDuty collector is claim-only. It selects an enabled `pagerduty`
instance from `ESHU_COLLECTOR_INSTANCES_JSON`. Provider targets must include
`provider`, `scope_id`, `account_id`, and `token_env`. The runtime resolves the
credential from the named environment variable and emits incident/change
source facts. When `config_validation_enabled` is true on a target, it also
emits optional live PagerDuty service and service-integration incident-routing
source facts plus coverage warnings.
Optional `api_base_url` overrides must use HTTPS because the runtime sends the
PagerDuty token to that endpoint. Optional target fields bound collection with
`incident_lookback`, `incident_limit`, `log_entry_limit`,
`change_event_limit`, `allowed_service_ids`, `config_validation_enabled`, and
`config_resource_limit`.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID` | required when more than one enabled PagerDuty instance exists | collector-pagerduty | Selects the claim-capable `pagerduty` instance. |
| `ESHU_PAGERDUTY_POLL_INTERVAL` | `1s` | collector-pagerduty | Delay between empty workflow-claim polls. |
| `ESHU_PAGERDUTY_CLAIM_LEASE_TTL` | `60s` | collector-pagerduty | Lease TTL used when claiming and refreshing work. |
| `ESHU_PAGERDUTY_HEARTBEAT_INTERVAL` | `20s` | collector-pagerduty | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_PAGERDUTY_COLLECTOR_OWNER_ID` | host/process-derived | collector-pagerduty | Owner label written into workflow claim rows. |

PagerDuty tokens must come from private environment variables referenced by
`token_env`; do not commit token values, incident titles, service names,
integration names, routing keys, PagerDuty URLs, or copied provider payloads to
public values files or docs.

Helm deployments use `pagerDutyCollector.collectorInstances` for the
collector-local claim configuration and `pagerDutyCollector.extraEnv` for
Secret-backed token variables referenced by target `token_env`. Keep the
selected `pagerDutyCollector.instanceId` aligned with an enabled, claim-driven
`pagerduty` entry in both the collector block and
`workflowCoordinator.collectorInstances`. If signed PagerDuty webhooks are
enabled, `webhookListener.pagerDuty.scopeId` must match a configured target
scope; the webhook listener only enqueues freshness and never emits PagerDuty
facts.

## Jira Collector

The Jira collector is claim-only. It selects an enabled `jira` instance from
`ESHU_COLLECTOR_INSTANCES_JSON`. Targets currently support Jira Cloud and must
include `provider: "jira_cloud"`, `scope_id`, `site_id`, `token_env`, and a
bounded JQL scope such as a project or label filter. Set either direct `jql` or
`jql_env`; prefer `jql_env` for hosted Compose and Helm values so normal JQL
with spaces, quotes, and operators is resolved from the runtime environment
instead of interpolated into JSON. `base_url` defaults to `https://<site_id>`
when omitted. The runtime resolves the API token from the named environment
variable, resolves optional basic-auth email from `email_env`, resolves JQL
from `jql_env` when configured, and emits only `work_item.*` source facts. The
optional `metadata_limit` target setting bounds project, issue-type, status,
workflow, and field definition reads; omitted values use the collector default.
PagerDuty incidents, GitHub pull requests, deployments, and graph truth are not
collected by this runtime.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_JIRA_COLLECTOR_INSTANCE_ID` | required when more than one enabled Jira instance exists | collector-jira | Selects the claim-capable `jira` instance. |
| `ESHU_JIRA_POLL_INTERVAL` | `1s` | collector-jira | Delay between empty workflow-claim polls. |
| `ESHU_JIRA_CLAIM_LEASE_TTL` | `60s` | collector-jira | Lease TTL used when claiming and refreshing work. |
| `ESHU_JIRA_HEARTBEAT_INTERVAL` | `20s` | collector-jira | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_JIRA_COLLECTOR_OWNER_ID` | host/process-derived | collector-jira | Owner label written into workflow claim rows. |

Jira API tokens must come from private environment variables referenced by
`token_env`. JQL referenced by `jql_env` is resolved by `collector-jira` at
startup; a missing value fails the enabled runtime before provider calls. Do
not commit token values, private issue summaries, user names, issue URLs, JQL
queries, or copied provider payloads to public values files or docs.
In Helm, use `jiraCollector.collectorInstances` for polling-only collection and
`jiraCollector.extraEnv` for Secret-backed `token_env` and optional
`email_env` and `jql_env` values. To add webhook freshness, also enable
`webhookListener.jira` with a `scopeId` that matches the configured Jira target;
the webhook listener enqueues freshness work only and does not emit
`work_item.*` facts.

## Live Grafana-Stack Collectors

The Grafana, Prometheus/Mimir, Loki, and Tempo collectors are claim-only. Each
selects one enabled collector instance from `ESHU_COLLECTOR_INSTANCES_JSON`.
Target configuration uses `targets` and references credentials through
environment-variable names. The runtime resolves those secrets in process
memory and commits metadata-only source facts.

Use source-controlled IaC/GitOps evidence first when it is current. Live
provider collection is fallback and validation evidence for no-IaC
environments, drift, freshness, and effective target, rule, log-signal, or
trace-signal metadata. These collectors do not emit graph truth directly.

Grafana targets include `provider: "grafana"`, `scope_id`, `instance_id`,
`base_url`, required `token_env`, optional `resource_limit`, optional
`stale_after`, optional `declared_uids`, and `enabled: true`.

Prometheus/Mimir targets include `provider: "prometheus"` or `"mimir"`,
`scope_id`, `instance_id`, `base_url`, optional `path_prefix`, optional
`token_env`, optional `tenant_id` or `tenant_id_env`, optional
`resource_limit`, optional `stale_after`, optional `declared_ids`, and
`enabled: true`.

The API process reuses the enabled `prometheus_mimir` target as the read source
for the console trend charts (`GET /api/v0/metrics/timeseries`). Those charts
query Eshu's own `eshu_dp_*` and `eshu_http_*` self-metrics, so the configured
`base_url` must point at a Prometheus or Mimir that scrapes Eshu's `/metrics`
endpoints. A target that ingests an external monitoring system into the graph
but does not scrape Eshu itself leaves the trend charts empty. See
[Status and Admin API](http-api/status-admin.md#trend-source-requirements).

Loki targets include `scope_id`, `instance_id`, `base_url`, optional
`path_prefix`, optional `token_env`, optional `tenant_id` or `tenant_id_env`,
optional `resource_limit`, optional `label_value_names`, optional
`max_label_values_per_label`, optional `series_matchers`, optional
`series_lookback`, optional `stale_after`, optional `declared_ids`, and
`enabled: true`. `series_lookback` bounds the `/loki/api/v1/series` query's
`start` window and is an independent knob: it defaults to a generous 24h when
unset and does not inherit `stale_after` (a rule-staleness setting).
**Coverage consequence:** series last active before the resolved
`series_lookback` window are not observed in the current generation, and Loki
reports no coverage warning for a time-window exclusion (unlike the
`resource_limit` truncation warning, a `/series` time filter is silent).
Widen `series_lookback` if you rely on full historical series visibility.

Tempo targets include `scope_id`, `instance_id`, `base_url`, optional
`path_prefix`, optional `token_env`, optional `tenant_id` or `tenant_id_env`,
optional `resource_limit`, optional `tag_value_names`, optional
`max_tag_values_per_tag`, optional `stale_after`, optional `lookback`,
optional `freshness_probe_enabled`, optional `declared_ids`, and
`enabled: true`.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_GRAFANA_COLLECTOR_INSTANCE_ID` | required when more than one enabled Grafana instance exists | collector-grafana | Selects the claim-capable `grafana` instance. |
| `ESHU_GRAFANA_COLLECTOR_POLL_INTERVAL` | `1s` | collector-grafana | Delay between empty workflow-claim polls. |
| `ESHU_GRAFANA_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-grafana | Lease TTL used when claiming and refreshing work. |
| `ESHU_GRAFANA_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-grafana | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_GRAFANA_COLLECTOR_OWNER_ID` | host/process-derived | collector-grafana | Owner label written into workflow claim rows. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_INSTANCE_ID` | required when more than one enabled Prometheus/Mimir instance exists | collector-prometheus-mimir | Selects the claim-capable `prometheus_mimir` instance. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_POLL_INTERVAL` | `1s` | collector-prometheus-mimir | Delay between empty workflow-claim polls. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-prometheus-mimir | Lease TTL used when claiming and refreshing work. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-prometheus-mimir | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_PROMETHEUS_MIMIR_COLLECTOR_OWNER_ID` | host/process-derived | collector-prometheus-mimir | Owner label written into workflow claim rows. |
| `ESHU_LOKI_COLLECTOR_INSTANCE_ID` | required when more than one enabled Loki instance exists | collector-loki | Selects the claim-capable `loki` instance. |
| `ESHU_LOKI_COLLECTOR_POLL_INTERVAL` | `1s` | collector-loki | Delay between empty workflow-claim polls. |
| `ESHU_LOKI_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-loki | Lease TTL used when claiming and refreshing work. |
| `ESHU_LOKI_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-loki | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_LOKI_COLLECTOR_OWNER_ID` | host/process-derived | collector-loki | Owner label written into workflow claim rows. |
| `ESHU_TEMPO_COLLECTOR_INSTANCE_ID` | required when more than one enabled Tempo instance exists | collector-tempo | Selects the claim-capable `tempo` instance. |
| `ESHU_TEMPO_COLLECTOR_POLL_INTERVAL` | `1s` | collector-tempo | Delay between empty workflow-claim polls. |
| `ESHU_TEMPO_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | collector-tempo | Lease TTL used when claiming and refreshing work. |
| `ESHU_TEMPO_COLLECTOR_HEARTBEAT_INTERVAL` | workflow default | collector-tempo | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_TEMPO_COLLECTOR_OWNER_ID` | host/process-derived | collector-tempo | Owner label written into workflow claim rows. |

Do not commit Grafana tokens, Prometheus/Mimir bearer tokens, Loki tokens,
Tempo tokens, tenant IDs, private URLs, query bodies, label values, tag
values, log lines, spans, traces, or copied provider payloads to public values
files or docs. Pass token and tenant values through private environment
variables or Kubernetes Secrets referenced by `extraEnv`.

## Vulnerability Intelligence Collector

The vulnerability intelligence collector is claim-only. It selects an enabled
`vulnerability_intelligence` instance from `ESHU_COLLECTOR_INSTANCES_JSON`.
Supported source targets are CISA KEV, FIRST EPSS, OSV, and NVD.
Collector configuration may define explicit `targets` or enable
`derive_from_owned_packages` so the coordinator derives bounded OSV npm, Pub,
and Swift package-version targets from active owned dependency facts with exact
versions. Swift targets keep Eshu's canonical `swift` ecosystem internally and
are sent to OSV as `SwiftURL`; Pub targets keep canonical `pub`. Manifest
ranges, aliases, Pub git/path/private-hosted rows, Pub dependency overrides,
branch-only Swift pins, revision-only Swift pins, and local/path Swift pins
remain partial evidence and are skipped for exact OSV collection. The default derived target
limit is 100; full-corpus deployments can raise
`derive_from_owned_packages.target_limit` up to 5000. The single OSV target
query payload remains capped separately at 100 queries.

Inside `ESHU_COLLECTOR_INSTANCES_JSON`, vulnerability targets may set
`fallback_urls` for source mirrors. The instance configuration may also set
`source_cache.directory`, `source_cache.mode` (`refresh` or `offline`),
`source_cache.freshness_ttl`, `source_cache.retention`, and
`source_cache.use_cached_on_fetch_error`. Offline mode never calls live
upstreams and fails closed when the cached artifact is missing or stale.

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_INSTANCE_ID` | required when more than one enabled vulnerability-intelligence instance exists | collector-vulnerability-intelligence | Selects the claim-capable `vulnerability_intelligence` instance. |
| `ESHU_VULNERABILITY_INTELLIGENCE_POLL_INTERVAL` | `1s` | collector-vulnerability-intelligence | Delay between empty workflow-claim polls. |
| `ESHU_VULNERABILITY_INTELLIGENCE_CLAIM_LEASE_TTL` | `60s` | collector-vulnerability-intelligence | Lease TTL used when claiming and refreshing work. |
| `ESHU_VULNERABILITY_INTELLIGENCE_HEARTBEAT_INTERVAL` | `20s` | collector-vulnerability-intelligence | Heartbeat interval for active workflow claims. Must be less than the claim lease TTL. |
| `ESHU_VULNERABILITY_INTELLIGENCE_COLLECTOR_OWNER_ID` | host/process-derived | collector-vulnerability-intelligence | Owner label written into workflow claim rows. |

## Confluence Collector

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_CONFLUENCE_BASE_URL` | unset | collector-confluence | Base Confluence Cloud URL, trimmed of trailing slash. |
| `ESHU_CONFLUENCE_SPACE_ID` | unset | collector-confluence | Numeric Confluence space ID for bounded single-space sync. |
| `ESHU_CONFLUENCE_SPACE_IDS` | unset | collector-confluence | Comma-separated allowlist of numeric Confluence space IDs for one-space-per-generation polling. |
| `ESHU_CONFLUENCE_ROOT_PAGE_ID` | unset | collector-confluence | Root page ID for bounded page-tree sync. |
| `ESHU_CONFLUENCE_SPACE_KEY` | unset | collector-confluence | Optional human-readable space key carried with source metadata and local smoke setup. |
| `ESHU_CONFLUENCE_EMAIL` | unset | collector-confluence | Basic-auth email for read-only Confluence API access. |
| `ESHU_CONFLUENCE_API_TOKEN` | unset | collector-confluence | Basic-auth API token. Required with email unless bearer token is set. |
| `ESHU_CONFLUENCE_BEARER_TOKEN` | unset | collector-confluence | Bearer token alternative for read-only Confluence API access. |
| `ESHU_CONFLUENCE_PAGE_LIMIT` | `100` | collector-confluence | Max pages fetched per bounded listing request (per-page size only). |
| `ESHU_CONFLUENCE_MAX_TOTAL_PAGES` | `5000` | collector-confluence | Max total pages assembled across a space/page-tree cursor walk. The walk also stops defensively after 200 paginated fetches or a repeated cursor. Raise this on large wikis that legitimately exceed the default; the source fact's `source_metadata.coverage_warning` reads `truncated` (vs. `complete`) when the cap or a defensive stop dropped pages the provider still had to return. |
| `ESHU_CONFLUENCE_POLL_INTERVAL` | `5m` | collector-confluence | Interval between repeated Confluence sync attempts. |

Set exactly one of `ESHU_CONFLUENCE_SPACE_ID`, `ESHU_CONFLUENCE_SPACE_IDS`, or
`ESHU_CONFLUENCE_ROOT_PAGE_ID`.

## Webhook Listener

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_WEBHOOK_GITHUB_SECRET` | unset | webhook-listener | Enables and verifies the GitHub webhook route using `X-Hub-Signature-256`. |
| `ESHU_WEBHOOK_GITLAB_TOKEN` | unset | webhook-listener | Enables and verifies the GitLab webhook route using `X-Gitlab-Token`. |
| `ESHU_WEBHOOK_BITBUCKET_SECRET` | unset | webhook-listener | Enables and verifies the Bitbucket webhook route using `X-Hub-Signature`. |
| `ESHU_WEBHOOK_PAGERDUTY_SECRET` | unset | webhook-listener | Enables and verifies the PagerDuty incident freshness route using `X-PagerDuty-Signature`. |
| `ESHU_WEBHOOK_JIRA_SECRET` | unset | webhook-listener | Enables and verifies the Jira incident freshness route using `X-Hub-Signature`. |
| `ESHU_WEBHOOK_GITHUB_PATH` | `/webhooks/github` | webhook-listener | HTTP path for GitHub webhook intake. |
| `ESHU_WEBHOOK_GITLAB_PATH` | `/webhooks/gitlab` | webhook-listener | HTTP path for GitLab webhook intake. |
| `ESHU_WEBHOOK_BITBUCKET_PATH` | `/webhooks/bitbucket` | webhook-listener | HTTP path for Bitbucket webhook intake. |
| `ESHU_WEBHOOK_PAGERDUTY_PATH` | `/webhooks/pagerduty` | webhook-listener | HTTP path for PagerDuty incident freshness intake. |
| `ESHU_WEBHOOK_JIRA_PATH` | `/webhooks/jira` | webhook-listener | HTTP path for Jira incident freshness intake. |
| `ESHU_WEBHOOK_PAGERDUTY_SCOPE_ID` | unset | webhook-listener | Required with `ESHU_WEBHOOK_PAGERDUTY_SECRET`; names the configured PagerDuty collector target allowed for webhook wake-ups. |
| `ESHU_WEBHOOK_JIRA_SCOPE_ID` | unset | webhook-listener | Required with `ESHU_WEBHOOK_JIRA_SECRET`; names the configured Jira collector target allowed for webhook wake-ups. |
| `ESHU_WEBHOOK_MAX_BODY_BYTES` | `1 MiB` | webhook-listener | Maximum accepted webhook request body size. |
| `ESHU_WEBHOOK_DEFAULT_BRANCH` | unset | webhook-listener | Fallback default branch when provider payloads omit repository default branch. |
| `ESHU_AWS_FRESHNESS_TOKEN` | unset | webhook-listener | Enables AWS freshness intake and validates bearer or `X-Eshu-AWS-Freshness-Token` headers. |
| `ESHU_AWS_FRESHNESS_PATH` | `/webhooks/aws/eventbridge` | webhook-listener | HTTP path for AWS freshness intake. |
| `ESHU_GCP_FRESHNESS_TOKEN` | unset | webhook-listener | Enables GCP Cloud Asset Inventory (CAI) feed freshness intake and validates bearer or `X-Eshu-GCP-Freshness-Token` headers. One of two independent accepted auth paths (see `ESHU_GCP_FRESHNESS_OIDC_*` below); either is sufficient. |
| `ESHU_GCP_FRESHNESS_PATH` | `/webhook/gcp-freshness` | webhook-listener | HTTP path for GCP freshness intake. |
| `ESHU_GCP_FRESHNESS_OIDC_AUDIENCE` | unset | webhook-listener | Enables Pub/Sub push OIDC verification as a second accepted auth path for GCP freshness intake; must equal the `aud` claim Google's Pub/Sub push mints (the push subscription's `--push-auth-token-audience`). Must be set together with `ESHU_GCP_FRESHNESS_OIDC_ALLOWED_SA`. |
| `ESHU_GCP_FRESHNESS_OIDC_ALLOWED_SA` | unset | webhook-listener | The push service-account email the verified OIDC token's `email` claim must match (with `email_verified=true`) for the request to authenticate. Must be set together with `ESHU_GCP_FRESHNESS_OIDC_AUDIENCE`. |

PagerDuty and Jira webhook variables only enqueue bounded incident freshness
triggers. They do not store provider payloads or emit facts directly; the
workflow coordinator must still authorize the configured `scope_id` and create
normal collector work, and polling remains the backfill path for missed
deliveries.
