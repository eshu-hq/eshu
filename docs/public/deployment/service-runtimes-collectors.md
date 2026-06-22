# Collector Runtime Services

This page covers the collector runtimes that feed Eshu's fact store. Use
[Service Runtimes](service-runtimes.md) for the full runtime matrix and
[Collector Authoring](../guides/collector-authoring.md) for contributor rules.

## Collector Control Plane

Hosted collectors emit facts through the shared ingestion boundary. They do not
write graph truth directly. Graph-visible state appears only after the
resolution engine projects queued work into the configured graph backend.
Community extensions follow the same facts-first boundary, but hosted
claim-capable execution also needs the
[Hosted Extension Operator Policy](../operate/hosted-extension-policy.md):
install, enable, and claim-capable are separate decisions; revocation stops new
work; credentials are references only; and isolation, egress, resources, and
security review must be approved before the workflow coordinator can schedule
extension claims.

When `ESHU_COMPONENT_HOME` is set on the workflow coordinator, the coordinator
re-verifies the installed component manifest against the configured
`ESHU_COMPONENT_TRUST_MODE`, allowlist, revocation list, core version, and
strict-mode Cosign provenance settings before materializing enabled
claim-capable activations as durable collector instances. It still requires
`ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` before planning component-extension
workflow rows; missing policy denies extension claims, restricted mode needs a
matching component allow rule, deny rules win, and broad mode is explicit.
The durable instance configuration carries component identity, manifest digest,
runtime protocol, adapter, and a stable config handle only. It does not persist
operator config paths, credential values, or provider targets.

The public Helm chart supports two collector styles:

- Direct collectors with explicit targets in their own values.
- Claim-driven collectors that receive `ESHU_COLLECTOR_INSTANCES_JSON`, select
  one enabled instance, claim durable work, and commit facts.

Charted claim-driven Terraform-state, AWS cloud, GCP cloud, Azure cloud,
package-registry, SBOM-attestation, provider security-alert, CI/CD run, PagerDuty,
Jira work-item, live Grafana-stack,
scanner-worker, and vulnerability-intelligence Deployments require:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`
- at least one matching collector instance

Disabled PagerDuty and Jira collector instances may remain declared with blank
private target fields and the claim-capable flag. The workflow coordinator
validates those rows as durable registrations only; enabling the instance makes
target validation strict before work can be planned.

The chart fails at render time when those prerequisites are missing.
For observability, source-controlled IaC/GitOps evidence is preferred when it
is current. Live Grafana, Prometheus/Mimir, Loki, and Tempo collection is the
fallback and validation lane for no-IaC environments, drift detection,
freshness, and effective target, rule, log-signal, or trace-signal metadata.

## Collector Matrix

| Runtime | Collector kind | Work source | Command | Helm template |
| --- | --- | --- | --- | --- |
| Confluence Collector | `documentation` | bounded Confluence space, space allowlist, or page tree | `/usr/local/bin/eshu-collector-confluence` | `deploy/helm/eshu/templates/deployment-confluence-collector.yaml` |
| OCI Registry Collector | `oci_registry` | explicit registry targets; runtime also supports claim-aware mode | `/usr/local/bin/eshu-collector-oci-registry` | `deploy/helm/eshu/templates/deployment-oci-registry-collector.yaml` |
| Terraform State Collector | `terraform_state` | workflow claims for configured state sources | `/usr/local/bin/eshu-collector-terraform-state` | `deploy/helm/eshu/templates/deployment-terraform-state-collector.yaml` |
| AWS Cloud Collector | `aws` | workflow claims for account, region, and service slices | `/usr/local/bin/eshu-collector-aws-cloud` | `deploy/helm/eshu/templates/deployment-aws-cloud-collector.yaml` |
| GCP Cloud Collector | `gcp` | workflow claims for configured Cloud Asset Inventory scopes | `/usr/local/bin/eshu-collector-gcp-cloud` | `deploy/helm/eshu/templates/deployment-gcp-cloud-collector.yaml` |
| Azure Cloud Collector | `azure` | workflow claims for configured Resource Graph scopes | `/usr/local/bin/eshu-collector-azure-cloud` | `deploy/helm/eshu/templates/deployment-azure-cloud-collector.yaml` |
| Kubernetes Live Collector | `kubernetes_live` | configured read-only cluster targets (in-cluster or kubeconfig auth); not claim-driven | `/usr/local/bin/eshu-collector-kubernetes-live` | `deploy/helm/eshu/templates/deployment-kubernetes-live-collector.yaml` |
| Vault Live Collector | `vault_live` | workflow claims for configured Vault cluster/namespace metadata targets | `/usr/local/bin/eshu-collector-vault-live` | `deploy/helm/eshu/templates/deployment-vault-live-collector.yaml` |
| Package Registry Collector | `package_registry` | workflow claims for configured or derived package metadata targets | `/usr/local/bin/eshu-collector-package-registry` | `deploy/helm/eshu/templates/deployment-package-registry-collector.yaml` |
| SBOM Attestation Collector | `sbom_attestation` | workflow claims for configured SBOM document URLs or OCI referrer documents | `/usr/local/bin/eshu-collector-sbom-attestation` | `deploy/helm/eshu/templates/deployment-sbom-attestation-collector.yaml` |
| Security Alert Collector | `security_alert` | workflow claims for configured GitHub Dependabot repository-alert targets | `/usr/local/bin/eshu-collector-security-alerts` | `deploy/helm/eshu/templates/deployment-security-alert-collector.yaml` |
| CI/CD Run Collector | `ci_cd_run` | workflow claims for configured GitHub Actions repository run targets | `/usr/local/bin/eshu-collector-cicd-run` | `deploy/helm/eshu/templates/deployment-cicd-run-collector.yaml` |
| PagerDuty Collector | `pagerduty` | workflow claims for configured PagerDuty account or service-allowlist targets | `/usr/local/bin/eshu-collector-pagerduty` | `deploy/helm/eshu/templates/deployment-pagerduty-collector.yaml` |
| Jira Collector | `jira` | workflow claims for configured Jira Cloud work-item targets | `/usr/local/bin/eshu-collector-jira` | `deploy/helm/eshu/templates/deployment-jira-collector.yaml` |
| Grafana Collector | `grafana` | workflow claims for configured live Grafana API targets | `/usr/local/bin/eshu-collector-grafana` | `deploy/helm/eshu/templates/deployment-grafana-collector.yaml` |
| Prometheus/Mimir Collector | `prometheus_mimir` | workflow claims for configured live Prometheus or Mimir API targets | `/usr/local/bin/eshu-collector-prometheus-mimir` | `deploy/helm/eshu/templates/deployment-prometheus-mimir-collector.yaml` |
| Loki Collector | `loki` | workflow claims for configured live Loki API targets | `/usr/local/bin/eshu-collector-loki` | `deploy/helm/eshu/templates/deployment-loki-collector.yaml` |
| Tempo Collector | `tempo` | workflow claims for configured Tempo query-frontend metadata targets | `/usr/local/bin/eshu-collector-tempo` | `deploy/helm/eshu/templates/deployment-tempo-collector.yaml` |
| Scanner Worker | `scanner_worker` | workflow claims for CPU-heavy or memory-heavy security analyzer targets | `/usr/local/bin/eshu-scanner-worker` | `deploy/helm/eshu/templates/deployment-scanner-worker.yaml` |
| Vulnerability Intelligence Collector | `vulnerability_intelligence` | workflow claims for bounded vulnerability source targets (CISA KEV, FIRST EPSS, NVD windows, OSV queries, GitLab Gemnasium, GHSA) or derived owned-package targets | `/usr/local/bin/eshu-collector-vulnerability-intelligence` | `deploy/helm/eshu/templates/deployment-vulnerability-intelligence-collector.yaml` |
| Component Extension Collector | manifest-declared collector kind | workflow claims for verified, claim-capable process-adapter component activations | `/usr/local/bin/eshu-collector-component-extension` | `deploy/helm/eshu/templates/deployment-component-extension-collector.yaml` |

All hosted collector runtimes expose `/healthz`, `/readyz`, `/metrics`, and
`/admin/status` through the shared runtime admin surface.

No-Regression Evidence: `go test ./internal/coordinator -run 'Test(LoadConfig(AddsTrustedClaimCapableComponentActivation|AddsActivationHostClaimMetadata|SkipsRevokedComponentActivation|SkipsUntrustedAndIncompatibleComponentActivations|SkipsComponentActivationWithoutClaims)|Service(RunSchedulesComponentExtensionWork|ComponentExtensionReconcileIsIdempotentAcrossRestart)|ComponentExtensionPlannerPlansActivationScopedWork|ShouldScheduleComponentExtension(SurfacesInvalidActivationConfig|IgnoresUnrelatedSchemaVersionConfig))' -count=1` proves the coordinator admits trusted claim-capable component activations, copies only safe host source/scope metadata, withholds revoked, untrusted, incompatible, and non-claimable activations, rejects unsupported SDK protocols before scheduling, avoids misclassifying unrelated collector configs, and creates one idempotent activation-scoped work item. This is a coordinator planning change only: it uses the existing open-target admission guard, does not change claim lease timing, worker counts, queue ordering, reducer graph writes, fact emission, or provider calls.

No-Regression Evidence: `go test ./cmd/collector-component-extension -run 'TestLoadRuntimeConfig|TestBuildClaimedService' -count=1` proves the process-backed worker selects one trusted claim-capable activation, rejects untrusted and unsupported OCI activations, applies activation host scope metadata, wires `extensionhost.Source`, and keeps claim retries, heartbeats, stale fencing, and completion on `collector.ClaimedService`.

No-Observability-Change: component extension scheduling and collection reuse
existing coordinator reconcile metrics, `collector_instances`,
`workflow_runs`, `workflow_work_items`, claim status rows, duplicate-work logs,
collector `/admin/status`, failure classes, commit counters, and
`/api/v0/index-status`. Component config handles are stable identifiers and do
not add credential or private path material to metric labels.

Central collector inventory comes from durable workflow coordinator registration
rows, direct collector status rows that a runtime persists, and active persisted
source or reducer fact metadata. `GET /api/v0/status/collectors` and the MCP
`list_collectors` tool classify these as coordinator-managed, direct-mode,
disabled, or unregistered while keeping evidence bounded to source names and
counts. A deployed pod that is neither registered with the coordinator, writing
a durable status row, nor emitting active persisted facts is not discoverable
from the central API alone; use the pod-local `/admin/status`, `/metrics`, and
the deployment platform inventory for that unsupported mode.

## Collector Contracts

| Collector | Contract |
| --- | --- |
| Confluence | Read-only Confluence Cloud `GET` collection. Exactly one crawl scope is required: `ESHU_CONFLUENCE_SPACE_ID`, `ESHU_CONFLUENCE_SPACE_IDS`, or `ESHU_CONFLUENCE_ROOT_PAGE_ID`. |
| OCI Registry | Observes tags, manifests, and referrers from explicit `ociRegistryCollector.targets`; runtime also supports claim-aware `oci_registry` instances when `ESHU_COLLECTOR_INSTANCES_JSON` is present. |
| Terraform State | Claim-driven. Selects one enabled `terraform_state` instance, opens exact local or S3 state sources, redacts sensitive values, and refuses to start without `ESHU_TFSTATE_REDACTION_KEY` and `ESHU_TFSTATE_REDACTION_RULESET_VERSION`. |
| AWS Cloud | Claim-driven. Selects one enabled `aws` instance, claims account/region/service work, obtains claim-scoped credentials, and commits reported AWS facts for IAM, ECR, ECS, ELBv2, Route 53, EC2 networking, Lambda, EKS, SQS, SNS, EventBridge, GuardDuty, S3, RDS, Redshift (provisioned and Serverless), DynamoDB, CloudWatch Logs, CloudFront, Secrets Manager, SSM Parameter Store, Security Hub, Glue Data Catalog, ElastiCache, MemoryDB, MSK, and Step Functions. |
| GCP Cloud | Claim-driven and charted. Selects one enabled `gcp` instance, requires `live_collection_enabled=true`, receives generation/fencing identity from workflow claims, reads Cloud Asset Inventory through ADC-backed read-only credentials, and commits GCP source facts only. Helm mounts the redaction key as a read-only Secret file and keeps chart exposure default-off. |
| Azure Cloud | Claim-driven and charted. Selects one enabled `azure` instance, requires `live_collection_enabled=true`, receives generation/fencing identity from workflow claims, reads Azure Resource Graph through the ambient read-only workload-identity credential, and commits Azure source facts only. Claimed-live serves the `resource_graph` lane only. Helm mounts the redaction key as a read-only Secret file and keeps chart exposure default-off. Live-smoke promotion remains gated pending operator-run live proof. |
| Vault Live | Claim-driven. Selects one enabled `vault_live` instance, resolves each target's read-only `token_env`, calls metadata-only Vault auth, policy, identity, mount, and KV v2 metadata endpoints, and commits only `secrets_iam_posture` source facts. It refuses to start without `ESHU_VAULT_LIVE_REDACTION_KEY`, never reads KV `/data`, and fingerprints Vault paths, names, accessors, aliases, policy hashes, and warning metadata with deterministic HMAC markers. |
| Package Registry | Claim-driven. Selects one enabled `package_registry` instance, fetches the explicit `metadata_url` or a coordinator-derived npm/PyPI metadata target from owned dependency evidence, and commits package, version, dependency, artifact, and source-hint facts for npm, PyPI, Go module, Maven, NuGet, and generic metadata shapes. Derived Go module, Maven, NuGet, Composer, RubyGems, and Cargo package identities stay bounded to observed owned dependency evidence and emit missing-evidence warnings when no native metadata adapter URL is available. |
| SBOM Attestation | Claim-driven. Selects one enabled `sbom_attestation` instance, fetches configured CycloneDX/SPDX SBOMs or in-toto attestations from HTTP(S) document URLs or OCI referrer blobs, and commits typed `sbom.*` and `attestation.*` facts. It redacts source URIs, preserves parse warnings as source facts, and keeps signature verification status separate from subject attachment truth. |
| Security Alert | Claim-driven. Selects one enabled `security_alert` instance, resolves the target `token_env` from the pod environment, refuses non-allowlisted repositories, requires HTTPS for any `api_base_url` override, fetches bounded GitHub Dependabot alert pages, and commits only `security_alert.repository_alert` facts. Provider state remains source evidence; reducers own reconciliation and impact truth. |
| CI/CD Run | Claim-driven and charted. Selects one enabled `ci_cd_run` instance, resolves the target `token_env` from the pod environment, refuses non-allowlisted repositories, requires HTTPS for any `api_base_url` override, fetches bounded GitHub Actions workflow-run, job, and artifact metadata, strips token-bearing artifact download URLs, and commits only `ci.*` source facts. Provider state remains source evidence; reducers own source-to-image bridge truth. |
| PagerDuty | Claim-driven. Selects one enabled `pagerduty` instance, resolves target `token_env` from the runtime environment, requires HTTPS for any configured `api_base_url`, fetches bounded incident, log-entry, and related change-event evidence, and optionally fetches bounded live service/integration configuration when `config_validation_enabled` is set. If PagerDuty allows incident/log-entry reads but hides optional related change events, the collector keeps the readable evidence and emits a coverage warning for the missing enrichment. It commits `incident.record`, `incident.lifecycle_event`, `change.record`, `incident_routing.observed_pagerduty_service`, `incident_routing.observed_pagerduty_integration`, and coverage-warning source facts only. Signed PagerDuty webhooks can wake the same configured `scope_id`, but they do not emit facts. PagerDuty state remains source evidence; the incident-context read model owns declared/applied/observed routing slots, while reducers and query surfaces own runtime, image, build/commit, PR, and work-item correlation. |
| Jira | Claim-driven and charted. Selects one enabled `jira` instance, resolves `token_env`, optional `email_env`, and optional `jql_env` from the pod environment, searches a bounded Jira Cloud updated window, fetches issue changelogs and remote links, collects bounded project/status/workflow/field metadata, and commits only `work_item.*` source facts. Direct `jql` remains supported for JSON-native configs, but hosted Compose and Helm deployments should use `jql_env` so private JQL is not interpolated into collector JSON. Signed Jira webhooks can wake the same configured `scope_id`, but they do not emit facts. Polling-only mode runs only `jiraCollector`; webhook-enabled mode also enables the shared webhook listener with a matching Jira `scopeId`. Work-item state remains source evidence; reducers and query surfaces own incident, runtime, code, and pull-request correlation truth. See [Jira Evidence Contract](../reference/jira-evidence.md) for identity, freshness, redaction, and fixture expectations. |
| Grafana | Claim-driven. Selects one enabled `grafana` instance, resolves a bounded live Grafana API target, reads folder/dashboard search, datasources, and alert-rule provisioning metadata, and commits only `observability.source_instance`, `observability.observed_dashboard`, `observability.observed_rule`, and `observability.coverage_warning` facts. It drops or fingerprints dashboard titles, dashboard URLs, datasource URLs, alert query models, contacts, notification destinations, credentials, private URLs, and token values. Live Grafana state is fallback and validation evidence for no-IaC, drift, and freshness; declared GitOps evidence remains preferred when current. |
| Prometheus/Mimir | Claim-driven. Selects one enabled `prometheus_mimir` instance, resolves a bounded live Prometheus-compatible API target, reads Prometheus active-target metadata and Prometheus/Mimir rule metadata, and commits only `observability.source_instance`, `observability.observed_target`, `observability.observed_rule`, and `observability.coverage_warning` facts. It drops or fingerprints scrape target URLs, target label values, discovered label values, raw PromQL, annotations, tenant IDs, tenant headers, credentials, and token values. Live metric provider state is fallback and validation evidence for no-IaC, drift, and freshness; declared GitOps evidence remains preferred when current. |
| Loki | Claim-driven. Selects one enabled `loki` instance, resolves a bounded live Loki API target, reads label metadata, explicitly allowlisted bounded label-value metadata, series metadata, and ruler rule metadata, and commits only `observability.source_instance`, `observability.observed_log_signal`, `observability.observed_rule`, and `observability.coverage_warning` facts. It does not call log query endpoints that can return streams. It drops or fingerprints label values, raw LogQL, tenant IDs, tenant headers, credentials, token values, private URLs, and provider response bodies. Live Loki state is fallback and validation evidence for no-IaC, drift, and freshness; declared GitOps evidence remains preferred when current. |
| Tempo | Claim-driven. Selects one enabled `tempo` instance, uses read-only Tempo metadata endpoints, and commits only `observability.source_instance`, `observability.observed_trace_signal`, and `observability.coverage_warning` facts. It reads `/api/echo`, `/api/v2/search/tags`, and operator-allowlisted `/api/v2/search/tag/<tag>/values`; tag values are counts plus fingerprints within cardinality limits. It does not retrieve traces, spans, raw trace IDs, request attributes, TraceQL bodies, tenant IDs, private URLs, or provider response bodies. Live Tempo state is fallback and validation evidence for no-IaC, drift, and freshness; declared GitOps evidence remains preferred when current. |
| Scanner Worker | Claim-driven. Selects one enabled `scanner_worker` instance, applies analyzer resource limits, emits source facts only, and records retry or dead-letter state without producing reducer-owned findings. The fallback analyzer emits `scanner_worker.warning`; `sbom_generation` accepts repository, image, or artifact targets when the runtime source has enough subject evidence; and the concrete `os_package_extraction` analyzer parses configured, already-extracted Alpine or Debian rootfs targets into `vulnerability.os_package` and `vulnerability.warning` facts. |
| Vulnerability Intelligence | Claim-driven. Selects one enabled `vulnerability_intelligence` instance, fetches bounded source targets (explicit CVE IDs, source snapshots, OSV package-version queries, NVD modified windows, GitLab Gemnasium, GHSA) or coordinator-derived exact owned-package targets for supported ecosystems such as npm and Hex, and commits `vulnerability.*` facts. API keys are referenced from the pod environment through the target's `api_key_env` and never persisted into facts, logs, metric labels, or chart values. |

When `prometheusMimirCollector.enabled=true`, Helm also passes that collector
instance JSON, selected instance ID, and referenced secret envs into the API
Deployment. The API uses the same target to back
`GET /api/v0/metrics/timeseries` for console trend panels; if the source is not
configured, the route returns empty points with unavailable freshness.

The Git repository collector also emits declared Grafana, Prometheus/Mimir,
Loki, and Tempo observability source facts from source-controlled IaC/GitOps
files. This is not a separate hosted runtime: it runs inside normal repository
snapshot parsing and commits metadata-only `observability.*` facts for folders,
dashboards, datasources, alert rules, Prometheus/Mimir scrape config, metric
rules, metric routes, Loki log routes, Tempo trace routes, and coverage
warnings. Live Grafana, Prometheus, Mimir, Loki, and Tempo API collection runs
through the hosted provider collectors above for no-IaC coverage, drift,
freshness, and effective target/rule/log-signal/trace-signal validation.

Keep titles, bodies, URLs, package names, feed URLs, credential values, cloud
resource identifiers, and other high-cardinality source data out of metric
labels. Put those values in logs or trace attributes when they are needed for
diagnosis.

For provider security alerts, do not put repository names, package names, alert
URLs, or credential values in metric labels or status errors. Keep public
examples generic and pass live credentials through a private environment file
or Kubernetes Secret.

For PagerDuty, do not put incident titles, service names, integration names,
account IDs, PagerDuty URLs, routing keys, or credential values in metric
labels or status errors. Keep incident, change, and optional live-config
details in source-fact payloads only when the target environment accepts that
evidence retention boundary.

For Vault live, do not put Vault tokens, private Vault URLs, mount paths, KV
paths, policy names, auth role names, entity names, alias names, accessors, raw
ACL policy bodies, warning messages, or provider response bodies in metric
labels or status errors. Vault payloads retain bounded enums, counts, and
deterministic HMAC markers only.

For Jira, do not put site IDs, issue keys, user identities, summaries, metadata
names, custom-field IDs, remote link URLs, or credential values in metric
labels or status errors. Jira
work-item payloads retain provider IDs, presence flags, redaction policy
version, and URL fingerprints rather than raw summaries, user identifiers,
private URLs, remote-link titles, or remote-link summaries.

For observability evidence, do not put dashboard titles, raw dashboard JSON,
queries, scrape targets, label values, tag values, log lines, spans, traces,
status messages, Kubernetes labels, raw UIDs, managed fields, cluster URLs, or
credential values in metric labels or status errors. Declared and applied
observability facts retain safe identities, fingerprints, resource classes,
source revisions, freshness state, and outcomes only.

For Grafana, do not put instance IDs, folder or dashboard titles, datasource
names, URLs, query models, contact points, notification routes, token
environment names, token values, or provider response bodies in metric labels.
Live Grafana payloads retain provider UIDs, type metadata, presence booleans,
redaction policy version, and fingerprints rather than raw provider content.

For Prometheus/Mimir, do not put instance IDs, scrape target URLs, label values,
raw PromQL, annotations, tenant IDs, tenant headers, token environment names,
token values, or provider response bodies in metric labels. Live metric payloads
retain provider UIDs, label-key lists, tenant presence, freshness state,
redaction policy version, and fingerprints rather than raw provider content.

For Loki, do not put instance IDs, label values, private URLs, raw LogQL,
tenant IDs, tenant headers, token values, or provider response bodies in metric
labels. Live Loki payloads retain provider UIDs, label-key lists, value counts,
tenant presence, freshness state, redaction policy version, and fingerprints
rather than raw log or query content.

Use the focused service runbooks for target, permission, redaction, dashboard,
and failure detail:

- [Terraform State Collector](../services/collector-terraform-state.md)
- [AWS Cloud Collector](../services/collector-aws-cloud.md)
- [PagerDuty Collector](../services/collector-pagerduty.md)
- [Jira Evidence Contract](../reference/jira-evidence.md)
- [Observability Evidence Contract](../reference/observability-evidence.md)

## ServiceMonitor Coverage

Helm can render `ServiceMonitor` resources for every charted hosted collector
on this page. Schema bootstrap and
bootstrap-index are excluded because they are not steady-state services.

The collector metrics services live under:

- `deploy/helm/eshu/templates/service-confluence-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-oci-registry-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-terraform-state-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-aws-cloud-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-gcp-cloud-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-azure-cloud-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-package-registry-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-sbom-attestation-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-security-alert-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-pagerduty-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-jira-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-scanner-worker-metrics.yaml`
- `deploy/helm/eshu/templates/service-vulnerability-intelligence-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-component-extension-collector-metrics.yaml`

No-Regression Evidence: `go test ./internal/runtime -run 'TestHelmJiraCollectorDeployment|TestJiraCollectorBinaryIsBuiltInstalledAndDocumented' -count=1`
proves the Jira collector stays opt-in by default, renders a claim-driven
Deployment with `/usr/local/bin/eshu-collector-jira`, mounts Secret-backed
credential env vars through `extraEnv`, and publishes metrics Service,
ServiceMonitor, NetworkPolicy, and PodDisruptionBudget resources when enabled.
Observability Evidence: the rendered Deployment exposes `/healthz`, `/readyz`,
`/metrics`, and `/admin/status` through the shared hosted runtime and keeps
Jira provider request, fact, rate-limit, and fetch-duration telemetry on the
existing bounded Jira collector instruments.

No-Regression Evidence: the Helm runtime contract test
`go test ./internal/runtime -run TestHelmPagerDutyCollectorDeployment -count=1`
renders `pagerDutyCollector` with workflow claims, metrics Service,
ServiceMonitor, NetworkPolicy, PDB, status probes, and Secret-backed
`PAGERDUTY_API_TOKEN` wiring without enabling the Deployment by default.

Observability Evidence: the chart wires the hosted PagerDuty binary to the
shared `/healthz`, `/readyz`, `/metrics`, and `/admin/status` runtime surface
and the ServiceMonitor selects only the bounded `pagerduty-collector`
component labels.
