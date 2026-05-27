# Collector Runtime Services

This page covers the collector runtimes that feed Eshu's fact store. Use
[Service Runtimes](service-runtimes.md) for the full runtime matrix and
[Collector Authoring](../guides/collector-authoring.md) for contributor rules.

## Collector Control Plane

Hosted collectors emit facts through the shared ingestion boundary. They do not
write graph truth directly. Graph-visible state appears only after the
resolution engine projects queued work into the configured graph backend.

The public Helm chart supports two collector styles:

- Direct collectors with explicit targets in their own values.
- Claim-driven collectors that receive `ESHU_COLLECTOR_INSTANCES_JSON`, select
  one enabled instance, claim durable work, and commit facts.

Claim-driven Terraform-state, AWS cloud, package-registry, SBOM-attestation,
provider security-alert, scanner-worker, and vulnerability-intelligence
Deployments require:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`
- at least one matching collector instance

The chart fails at render time when those prerequisites are missing.

## Collector Matrix

| Runtime | Collector kind | Work source | Command | Helm template |
| --- | --- | --- | --- | --- |
| Confluence Collector | `documentation` | bounded Confluence space, space allowlist, or page tree | `/usr/local/bin/eshu-collector-confluence` | `deploy/helm/eshu/templates/deployment-confluence-collector.yaml` |
| OCI Registry Collector | `oci_registry` | explicit registry targets; runtime also supports claim-aware mode | `/usr/local/bin/eshu-collector-oci-registry` | `deploy/helm/eshu/templates/deployment-oci-registry-collector.yaml` |
| Terraform State Collector | `terraform_state` | workflow claims for configured state sources | `/usr/local/bin/eshu-collector-terraform-state` | `deploy/helm/eshu/templates/deployment-terraform-state-collector.yaml` |
| AWS Cloud Collector | `aws` | workflow claims for account, region, and service slices | `/usr/local/bin/eshu-collector-aws-cloud` | `deploy/helm/eshu/templates/deployment-aws-cloud-collector.yaml` |
| Package Registry Collector | `package_registry` | workflow claims for configured or derived package metadata targets | `/usr/local/bin/eshu-collector-package-registry` | `deploy/helm/eshu/templates/deployment-package-registry-collector.yaml` |
| SBOM Attestation Collector | `sbom_attestation` | workflow claims for configured SBOM document URLs or OCI referrer documents | `/usr/local/bin/eshu-collector-sbom-attestation` | `deploy/helm/eshu/templates/deployment-sbom-attestation-collector.yaml` |
| Security Alert Collector | `security_alert` | workflow claims for configured GitHub Dependabot repository-alert targets | `/usr/local/bin/eshu-collector-security-alerts` | `deploy/helm/eshu/templates/deployment-security-alert-collector.yaml` |
| Scanner Worker | `scanner_worker` | workflow claims for CPU-heavy or memory-heavy security analyzer targets | `/usr/local/bin/eshu-scanner-worker` | `deploy/helm/eshu/templates/deployment-scanner-worker.yaml` |
| Vulnerability Intelligence Collector | `vulnerability_intelligence` | workflow claims for bounded vulnerability source targets (CISA KEV, FIRST EPSS, NVD windows, OSV queries, GitLab Gemnasium, GHSA) or derived owned-package targets | `/usr/local/bin/eshu-collector-vulnerability-intelligence` | `deploy/helm/eshu/templates/deployment-vulnerability-intelligence-collector.yaml` |

All hosted collector runtimes expose `/healthz`, `/readyz`, `/metrics`, and
`/admin/status` through the shared runtime admin surface.

## Collector Contracts

| Collector | Contract |
| --- | --- |
| Confluence | Read-only Confluence Cloud `GET` collection. Exactly one crawl scope is required: `ESHU_CONFLUENCE_SPACE_ID`, `ESHU_CONFLUENCE_SPACE_IDS`, or `ESHU_CONFLUENCE_ROOT_PAGE_ID`. |
| OCI Registry | Observes tags, manifests, and referrers from explicit `ociRegistryCollector.targets`; runtime also supports claim-aware `oci_registry` instances when `ESHU_COLLECTOR_INSTANCES_JSON` is present. |
| Terraform State | Claim-driven. Selects one enabled `terraform_state` instance, opens exact local or S3 state sources, redacts sensitive values, and refuses to start without `ESHU_TFSTATE_REDACTION_KEY` and `ESHU_TFSTATE_REDACTION_RULESET_VERSION`. |
| AWS Cloud | Claim-driven. Selects one enabled `aws` instance, claims account/region/service work, obtains claim-scoped credentials, and commits reported AWS facts for IAM, ECR, ECS, ELBv2, Route 53, EC2 networking, Lambda, EKS, SQS, SNS, EventBridge, S3, RDS, Redshift (provisioned and Serverless), DynamoDB, CloudWatch Logs, CloudFront, Secrets Manager, SSM Parameter Store, Security Hub, and Glue Data Catalog. |
| Package Registry | Claim-driven. Selects one enabled `package_registry` instance, fetches the explicit `metadata_url` or a coordinator-derived npm packument target from owned dependency evidence, and commits package, version, dependency, artifact, and source-hint facts for npm, PyPI, Go module, Maven, NuGet, and generic metadata shapes. |
| SBOM Attestation | Claim-driven. Selects one enabled `sbom_attestation` instance, fetches configured CycloneDX/SPDX SBOMs or in-toto attestations from HTTP(S) document URLs or OCI referrer blobs, and commits typed `sbom.*` and `attestation.*` facts. It redacts source URIs, preserves parse warnings as source facts, and keeps signature verification status separate from subject attachment truth. |
| Security Alert | Claim-driven. Selects one enabled `security_alert` instance, resolves the target `token_env` from the pod environment, refuses non-allowlisted repositories, requires HTTPS for any `api_base_url` override, fetches bounded GitHub Dependabot alert pages, and commits only `security_alert.repository_alert` facts. Provider state remains source evidence; reducers own reconciliation and impact truth. |
| Scanner Worker | Claim-driven. Selects one enabled `scanner_worker` instance, applies analyzer resource limits, emits source facts only, and records retry or dead-letter state without producing reducer-owned findings. The fallback analyzer emits `scanner_worker.warning`; the concrete `os_package_extraction` analyzer parses configured, already-extracted Alpine or Debian rootfs targets into `vulnerability.os_package` and `vulnerability.warning` facts. |
| Vulnerability Intelligence | Claim-driven. Selects one enabled `vulnerability_intelligence` instance, fetches bounded source targets (explicit CVE IDs, source snapshots, OSV package-version queries, NVD modified windows, GitLab Gemnasium, GHSA) or coordinator-derived owned-package targets, and commits `vulnerability.*` facts. API keys are referenced from the pod environment through the target's `api_key_env` and never persisted into facts, logs, metric labels, or chart values. |

Keep titles, bodies, URLs, package names, feed URLs, credential values, cloud
resource identifiers, and other high-cardinality source data out of metric
labels. Put those values in logs or trace attributes when they are needed for
diagnosis.

For provider security alerts, do not put repository names, package names, alert
URLs, or credential values in metric labels or status errors. Keep public
examples generic and pass live credentials through a private environment file
or Kubernetes Secret.

Use the focused service runbooks for target, permission, redaction, dashboard,
and failure detail:

- [Terraform State Collector](../services/collector-terraform-state.md)
- [AWS Cloud Collector](../services/collector-aws-cloud.md)

## ServiceMonitor Coverage

Helm can render `ServiceMonitor` resources for every hosted collector on this
page. Schema bootstrap and bootstrap-index are excluded because they are not
steady-state services.

The collector metrics services live under:

- `deploy/helm/eshu/templates/service-confluence-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-oci-registry-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-terraform-state-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-aws-cloud-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-package-registry-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-sbom-attestation-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-security-alert-collector-metrics.yaml`
- `deploy/helm/eshu/templates/service-scanner-worker-metrics.yaml`
- `deploy/helm/eshu/templates/service-vulnerability-intelligence-collector-metrics.yaml`
