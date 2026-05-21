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

Claim-driven Terraform-state, AWS cloud, and package-registry collector
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
| Package Registry Collector | `package_registry` | workflow claims for configured package metadata targets | `/usr/local/bin/eshu-collector-package-registry` | `deploy/helm/eshu/templates/deployment-package-registry-collector.yaml` |

All hosted collector runtimes expose `/healthz`, `/readyz`, `/metrics`, and
`/admin/status` through the shared runtime admin surface.

## Confluence Collector

The Confluence collector reads a bounded Confluence Cloud scope and emits
source-neutral documentation facts. It is read-only: it issues Confluence HTTP
`GET` requests and never calls mutation APIs.

Required scope input is exactly one of:

- `ESHU_CONFLUENCE_SPACE_ID`
- `ESHU_CONFLUENCE_SPACE_IDS`
- `ESHU_CONFLUENCE_ROOT_PAGE_ID`

Use this collector when documentation truth should come from Confluence. Keep
page titles, bodies, excerpts, URLs, and stored content out of metric labels and
request logs.

## OCI Registry Collector

The OCI registry collector observes image tags, manifests, and referrers and
emits digest-addressed registry facts. The chart uses
`ociRegistryCollector.targets` for explicit target configuration. Runtime
support also exists for claim-aware `oci_registry` instances when
`ESHU_COLLECTOR_INSTANCES_JSON` is present.

Supported client wiring includes JFrog Docker/OCI, ECR, Docker Hub, GHCR,
Harbor, Google Artifact Registry, and Azure Container Registry.

Use the registry collector when image provenance, tag movement, or container
supply-chain context needs to join the service graph.

## Terraform State Collector

The Terraform-state collector is claim-driven. It selects one enabled
`terraform_state` instance from `ESHU_COLLECTOR_INSTANCES_JSON`, claims work,
opens exact local or S3 state sources, parses evidence, redacts sensitive
values, and commits Terraform-state facts.

Required runtime inputs include:

- `ESHU_COLLECTOR_INSTANCES_JSON` with one claim-enabled `terraform_state`
  instance
- `ESHU_TFSTATE_REDACTION_KEY`
- `ESHU_TFSTATE_REDACTION_RULESET_VERSION`

The redaction ruleset version is mandatory. The binary refuses to start when it
is blank because audit evidence needs to prove which policy version produced a
redaction decision.

Use the [Terraform State Collector](../services/collector-terraform-state.md)
runbook for target-scope, S3, graph-discovery, redaction, and telemetry detail.

## AWS Cloud Collector

The AWS cloud collector is claim-driven. It selects one enabled `aws` instance
from `ESHU_COLLECTOR_INSTANCES_JSON`, claims account/region/service work,
obtains claim-scoped credentials, observes AWS resources, and commits reported
AWS facts.

It currently covers IAM, ECR, ECS, ELBv2, Route 53, EC2 networking, Lambda, EKS,
SQS, SNS, EventBridge, S3, RDS, DynamoDB, CloudWatch Logs, CloudFront, Secrets
Manager, and SSM Parameter Store surfaces.

Use the [AWS Cloud Collector](../services/collector-aws-cloud.md) runbook for
configuration, status fields, dashboards, common failures, and escalation.

## Package Registry Collector

The package-registry collector is claim-driven. It selects one enabled
`package_registry` instance from `ESHU_COLLECTOR_INSTANCES_JSON`, claims work,
fetches the target's explicit `metadata_url`, parses package metadata, and
commits package, version, dependency, artifact, and source-hint facts.

Supported parser families include npm, PyPI, Go module, Maven, NuGet, and a
generic metadata shape.

Use this collector when package metadata needs to become graph evidence without
placing package names, feed URLs, metadata paths, or credential values in
metrics or logs.

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
