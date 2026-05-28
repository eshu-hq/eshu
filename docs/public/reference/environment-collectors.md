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
| `ESHU_COLLECTOR_INSTANCES_JSON` | unset | workflow coordinator and claim-aware collectors | Desired collector instance list. Claim-driven runtimes select enabled claim-capable instances from this JSON. |

Active coordinator mode is guarded. The process rejects
`ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=active` unless
`ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true` and at least one enabled
collector instance has `claims_enabled: true`.

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
| `ESHU_AWS_REDACTION_KEY` | unset | collector-aws-cloud | Deployment-scoped key for CloudFormation stack-output, CloudWatch alarm dimension, CodeDeploy on-premises-tag, Cognito free-text, ECS task-definition, Lambda environment, Security Hub action-target, Organizations account, and IAM Identity Center principal-name redaction markers. Required when a target scope enables `cloudformation`, `cloudwatch`, `codedeploy`, `cognito`, `ecs`, `lambda`, `organizations`, `securityhub`, or `ssoadmin`. |

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
targets currently support GitHub Dependabot repository alerts and must include
`token_env`, `repository`, and `allowed_repositories`. The runtime resolves the
credential from the named environment variable and emits only
`security_alert.repository_alert` facts. Optional `api_base_url` overrides must
use HTTPS because the runtime sends the bearer token to that endpoint.

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

## Vulnerability Intelligence Collector

The vulnerability intelligence collector is claim-only. It selects an enabled
`vulnerability_intelligence` instance from `ESHU_COLLECTOR_INSTANCES_JSON`.
Supported source targets are CISA KEV, FIRST EPSS, OSV, and NVD.
Collector configuration may define explicit `targets` or enable
`derive_from_owned_packages` so the coordinator derives bounded OSV npm
package-version targets from active owned dependency facts with exact versions.
Manifest ranges and aliases remain partial evidence and are skipped for exact
OSV collection. The default derived target limit is 100; full-corpus
deployments can raise `derive_from_owned_packages.target_limit` up to 5000.
The single OSV target query payload remains capped separately at 100 queries.

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
| `ESHU_CONFLUENCE_PAGE_LIMIT` | `100` | collector-confluence | Max pages fetched per bounded listing request. |
| `ESHU_CONFLUENCE_POLL_INTERVAL` | `5m` | collector-confluence | Interval between repeated Confluence sync attempts. |

Set exactly one of `ESHU_CONFLUENCE_SPACE_ID`, `ESHU_CONFLUENCE_SPACE_IDS`, or
`ESHU_CONFLUENCE_ROOT_PAGE_ID`.

## Webhook Listener

| Variable | Default | Read by | Purpose |
| --- | --- | --- | --- |
| `ESHU_WEBHOOK_GITHUB_SECRET` | unset | webhook-listener | Enables and verifies the GitHub webhook route using `X-Hub-Signature-256`. |
| `ESHU_WEBHOOK_GITLAB_TOKEN` | unset | webhook-listener | Enables and verifies the GitLab webhook route using `X-Gitlab-Token`. |
| `ESHU_WEBHOOK_BITBUCKET_SECRET` | unset | webhook-listener | Enables and verifies the Bitbucket webhook route using `X-Hub-Signature`. |
| `ESHU_WEBHOOK_GITHUB_PATH` | `/webhooks/github` | webhook-listener | HTTP path for GitHub webhook intake. |
| `ESHU_WEBHOOK_GITLAB_PATH` | `/webhooks/gitlab` | webhook-listener | HTTP path for GitLab webhook intake. |
| `ESHU_WEBHOOK_BITBUCKET_PATH` | `/webhooks/bitbucket` | webhook-listener | HTTP path for Bitbucket webhook intake. |
| `ESHU_WEBHOOK_MAX_BODY_BYTES` | `1 MiB` | webhook-listener | Maximum accepted webhook request body size. |
| `ESHU_WEBHOOK_DEFAULT_BRANCH` | unset | webhook-listener | Fallback default branch when provider payloads omit repository default branch. |
| `ESHU_AWS_FRESHNESS_TOKEN` | unset | webhook-listener | Enables AWS freshness intake and validates bearer or `X-Eshu-AWS-Freshness-Token` headers. |
| `ESHU_AWS_FRESHNESS_PATH` | `/webhooks/aws/eventbridge` | webhook-listener | HTTP path for AWS freshness intake. |
