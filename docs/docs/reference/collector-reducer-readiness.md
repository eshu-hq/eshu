# Collector And Reducer Readiness

Last updated: 2026-05-15.

Use this page when deciding whether the next Eshu slice should add a collector,
add a reducer, update Helm, or run deployment proof. The platform rule is
facts first:

```mermaid
flowchart LR
  A["Collector observes source truth"] --> B["Scope + generation"]
  B --> C["Typed facts in Postgres"]
  C --> D["Projector / reducer intents"]
  D --> E["Reducer-owned truth"]
  E --> F["Graph, facts, status, API, MCP"]
```

Collectors stop at source observation and typed facts. Reducers decide
cross-source truth. API and MCP surfaces read the reducer-owned result.

## Current Answer

The next blocker is not another design-only collector. The current blocker is
proving the implemented collector lane in deployment and closing reducer/read
model gaps for facts already emitted.

Helm already renders optional workloads for these implemented collectors:

- Confluence collector
- OCI registry collector
- Terraform-state collector
- AWS cloud collector
- package-registry collector
- webhook listener with AWS freshness intake

Do not add Helm values for design-only collectors until a binary exists. Empty
chart knobs create an operator promise the runtime cannot keep.

The public chart still keeps workflow-coordinator claim ownership dark:
`deploy/helm/eshu/templates/validate.yaml` rejects
`workflowCoordinator.claimsEnabled=true`. That is correct until the active
claim path has deployment evidence. Claim-driven collectors can be rendered, but
a full EKS collector proof needs a follow-up deployment slice that proves
coordinator-owned claims, collector work creation, reducer drain, and status
visibility together before relaxing that guard.

## Implemented Runtime Inventory

| Source family | Runtime state | Helm state | Reducer/read state | Remaining deployment or truth gap |
| --- | --- | --- | --- | --- |
| Git/repository | Implemented through ingester and collector-git paths. | Ingester `StatefulSet` is charted. | Workload, deployment, code-call, semantic entity, SQL relationship, inheritance, and package-source follow-up domains exist. | EKS proof must show repo sync, fact commit, queue drain, graph projection, and query completeness on the target cluster. |
| Terraform state | `go/cmd/collector-terraform-state` and `go/internal/collector/terraformstate` exist. | Optional `terraformStateCollector` deployment exists and requires collector instances plus redaction config. | `DomainConfigStateDrift` emits bounded counter/log truth; management-status read models remain in planner issues. | Live S3/local state proof in the target environment, plus #124, #130, and #131 for useful management status. |
| AWS cloud | `go/cmd/collector-aws-cloud` and service scanners exist for the current AWS slice. | Optional `awsCloudCollector` deployment and isolated service account/IRSA values exist. | `DomainCloudAssetResolution` and `DomainAWSCloudRuntimeDrift` exist; AWS runtime drift writes durable facts and exposes bounded API/MCP read-model rows. | Live read-only AWS proof, #37 operator closeout, graph projection shape for drift findings, and active coordinator claim proof. |
| AWS freshness | Implemented through `go/cmd/webhook-listener` and `go/internal/collector/awscloud/freshness`. | Webhook listener and `awsFreshness` ingress path are charted. | Freshness creates targeted AWS collector work; scheduled scans remain authoritative. | #37 remains open for live AWS EventBridge/AWS Config sample, dashboard visibility, and security sign-off. |
| OCI registry | `go/cmd/collector-oci-registry` exists. | Optional `ociRegistryCollector` deployment exists. | No first-class container-image identity reducer is complete. | Add digest identity/read model joining Git image refs, AWS runtime refs, OCI manifests, and later SBOM/attestation. |
| Package registry | `go/cmd/collector-package-registry` exists. | Optional `packageRegistryCollector` deployment exists. | `DomainPackageSourceCorrelation` classifies source hints with counters only; it does not promote package ownership. Package-native dependency facts now project to bounded package dependency graph reads. | Expand package ownership/usage correlation after EKS collector proof and image identity. |
| Confluence documentation | `go/cmd/collector-confluence` exists. | Optional `confluenceCollector` deployment exists. | Documentation facts remain evidence, not operational truth. | Useful for documentation drift later; not required for the AWS/IaC EKS proof path. |

## Design-Only Or Incomplete Collector Families

These collectors should not receive Helm workloads until their fact contracts,
fixtures, reducer contracts, telemetry, and binary wiring exist.

| Source family | Current state | Needed before runtime |
| --- | --- | --- |
| Kubernetes live | ADR exists; no runtime package or charted workload. | Collector kind/workflow contract, API discovery, fixtures, reducer joins, and a service runbook. |
| SBOM and attestation | ADR exists; no runtime package or reducer. | Fact contracts, fixture parsers, OCI/referrer integration, provenance reducer, and read model. |
| CI/CD runs | ADR exists; hosted runtime is not implemented. The first reducer slice writes `reducer_ci_cd_run_correlation` facts from fixture-backed run/artifact/environment evidence and exposes bounded API/MCP reads. | Provider fixture collectors, run/job/artifact fact contracts beyond reducer fixtures, provider-specific extraction, credentials/redaction proof, and hosted runtime. |
| Service catalog | ADR exists; no runtime package or reducer. | Catalog fact contracts, ownership/admission reducer, and service-story read integration. |
| Observability | ADR exists; no runtime package or reducer. | OTel/Prometheus/Grafana/Datadog fact fixtures, reducer coverage outcomes, then hosted runtime. |
| Vulnerability intelligence | ADR exists and is intentionally gated. | Package, OCI, SBOM, AWS, Terraform state, and deployment evidence must be proven before impact reducers. |
| Incident/change | Research/design issue remains open. | ADR before implementation. |
| Secrets/IAM posture | Research/design issue remains open and needs security review. | ADR before implementation; no source credentials or secret values in facts. |
| GCP/Azure/multi-cloud | Research/design issues remain open. | Shared multi-cloud runtime contract first, then provider-specific collector ADRs. |

## Reducer And Read-Model Gaps

The implemented collectors already emit facts that need stronger reducer or read
surfaces.

1. Container image identity.
   Join Git image references, OCI registry digests, AWS ECR/ECS/EKS/Lambda
   runtime references, and later SBOM/attestation by digest. This should land
   before vulnerability impact work. Current reducer scope is digest-first:
   explicit digest references and single OCI tag observations become durable
   `reducer_container_image_identity` facts; ambiguous, unresolved, and stale
   runtime tag outcomes stay diagnostic counters.

   No-Regression Evidence: focused reducer coverage with
   `go test ./internal/reducer -run 'TestBuildContainerImageIdentity|TestContainerImageIdentity|TestPostgresContainerImageIdentity|TestImplementedDefaultDomainDefinitions.*ContainerImageIdentity' -count=1`,
   active OCI loader coverage with
   `go test ./internal/storage/postgres -run 'TestFactStoreListActiveContainerImageIdentityFacts' -count=1`,
   and package coverage with
   `go test ./internal/reducer ./internal/storage/postgres ./internal/telemetry ./cmd/reducer -count=1`
   cover exact digest, tag resolution, ambiguous tags, unresolved tags, stale
   runtime tags, active OCI fact loading, durable writer filtering, default
   domain wiring, telemetry registration, and reducer command wiring.

   Observability Evidence: `eshu_dp_container_image_identity_decisions_total`
   emits bounded `domain` and `outcome` dimensions for exact digest, tag
   resolved, ambiguous tag, unresolved, and stale tag decisions; durable facts
   include `identity_strength`, source layers, and evidence fact IDs for
   operator diagnosis.

2. IaC management status.
   Use Terraform config, Terraform state, AWS cloud facts, and reducer drift
   findings to answer whether a resource is managed, unmanaged, orphaned, stale,
   ambiguous, or unknown. This maps to #124, #130, and #131 before import-plan
   generation in #125.

3. AWS runtime drift read surface.
   `DomainAWSCloudRuntimeDrift` writes durable reducer facts. The
   `POST /api/v0/aws/runtime-drift/findings` route and
   `list_aws_runtime_drift_findings` MCP tool expose bounded scope/account,
   region, ARN, finding-kind, limit, and offset filters with
   exact/derived/ambiguous/stale/unknown outcomes and rejected promotion status.
   Service/environment candidates and dependency paths remain evidence fields,
   not ownership truth. Graph nodes still need a frozen Cypher shape before
   projection lands.

   No-Regression Evidence: `go test ./internal/query ./internal/mcp -count=1`
   covers the bounded API route, MCP dispatch, OpenAPI contract, capability
   matrix parity, and existing IaC management behavior without changing the
   reducer or store query shape.

   Observability Evidence: `query.aws_runtime_drift_findings` spans wrap the
   route and the existing instrumented Postgres reader emits
   `eshu_dp_postgres_query_duration_seconds` for active-generation drift fact
   list/count queries.

4. Package ownership and consumption.
   Package source hints are currently classified without ownership promotion.
   Do not infer package ownership from weak registry metadata until exact,
   derived, ambiguous, unresolved, stale, and rejected cases are proven.
   Package-native dependency edges are safe to expose separately because they
   describe package metadata, not repository ownership or runtime consumption.

5. CI/CD run correlation.
   `DomainCICDRunCorrelation` writes durable reducer facts for provider runs,
   artifacts, environments, and rejected shell-only hints. Exact canonical
   writes require an artifact digest that joins to one reducer-owned
   container-image identity row; environment-only and CI-success evidence stays
   provenance. The `GET /api/v0/ci-cd/run-correlations` route and
   `list_ci_cd_run_correlations` MCP tool expose bounded scope, repository,
   commit, provider plus provider-run for run-only reads, artifact-digest,
   environment, outcome, limit, and cursor filters.

   No-Regression Evidence: focused reducer, query, MCP, storage, telemetry,
   API, and reducer command coverage with
   `go test ./internal/reducer -run 'TestBuildCICDRunCorrelationDecisions|TestCICDRunCorrelationHandler|TestPostgresCICDRunCorrelationWriter|TestImplementedDefaultDomainDefinitions|TestNewDefaultRegistry' -count=1`,
   `go test ./internal/query -run 'TestOpenAPISpecIncludesCICDRunCorrelations|TestCICDListRunCorrelations|TestCICDRunCorrelationQuery|TestCapabilityMatrixMatchesYAMLContract' -count=1`,
   `go test ./internal/mcp -run 'TestMCPToolContractMatrixCoversReadOnlyTools|TestResolveRouteMapsCICDRunCorrelationsToBoundedQuery|TestReadOnlyTools|TestHandleHTTPMessage_ToolsList|TestReadOnlyToolsDoNotUseTopLevelComposition' -count=1`,
   `go test ./internal/storage/postgres -run 'TestListActiveCICDRunCorrelationFactsQueryIsDigestBoundedAndPaged|TestBootstrapDefinitionsIncludeCICDRunCorrelationFactIndexes' -count=1`,
   `go test ./internal/telemetry -run 'TestSpanNames|TestMetricDimensionKeys' -count=1`,
   and `go test ./cmd/reducer ./cmd/api -count=1` covers exact,
   derived, ambiguous, unresolved, rejected, index, OpenAPI, MCP, and wiring
   contracts.

   Observability Evidence: `eshu_dp_ci_cd_run_correlations_total` exposes the
   reducer domain and bounded outcome for admission decisions; the
   `query.ci_cd_run_correlations` span plus existing Postgres query duration
   metrics expose the read path.

6. Supply-chain impact.
   SBOM, attestation, vulnerability, package, OCI, cloud, and deployment facts
   need reducer-owned confidence rules. The vulnerability collector should stay
   gated until those upstream facts are proven together.

## EKS Proof Ladder

Use this order before adding more collector families:

1. Render and deploy the existing chart in its current safe mode with API, MCP,
   ingester, reducer, workflow coordinator, and the collector workloads that do
   not require active coordinator claims.
2. Prove `/healthz`, `/readyz`, `/admin/status`, and `/metrics` for each
   enabled runtime.
3. In a follow-up proof branch, enable active workflow-coordinator claims and
   render the claim-driven AWS, Terraform-state, and package-registry paths
   together. The public chart should not loosen this guard until that branch
   records proof.
4. Confirm collector facts in Postgres by `collector_kind` and `fact_kind`.
5. Confirm reducer queues drain to zero without dead letters.
6. Confirm graph truth and API/MCP truth agree for one service with Git,
   Terraform state, AWS, OCI, and package evidence.
7. Run an AWS EventBridge or AWS Config freshness sample through the webhook
   path and verify it creates ordinary AWS collector work without widening
   authorization scope.
8. Record wall time, fact counts, queue counts, retry counts, dead letters,
   backend, chart values shape, image digest, and commit SHA.
9. Only after this proof should Helm loosen the active workflow-coordinator
   claim guard for supported deployments.

## Next Slices

Recommended order:

1. Deployment readiness PR: document and prove the EKS values shape for the
   implemented collectors, including the workflow-coordinator claim guard.
2. #37 closeout: live AWS freshness validation, dashboard/metric visibility,
   and security sign-off.
3. Container image identity reducer and read model.
4. #124, #130, and #131 for IaC management status before import-plan generation.
5. Package ownership/usage reducer expansion.
6. SBOM/attestation runtime and reducer.
7. Vulnerability intelligence runtime and impact reducer.
8. Kubernetes live collector if cluster runtime truth is required.
9. #20, #21, and #22 for shared multi-cloud collector design.

## Issue Map

Open tracking issues that still matter for this path:

- #12 vulnerability intelligence collector: keep gated until package, OCI,
  SBOM, AWS, Terraform state, and deployment facts are proven together.
- #20 multi-cloud runtime collectors beyond AWS.
- #21 GCP cloud collector.
- #22 Azure cloud collector.
- #23 incident and change collector.
- #25 secrets and IAM posture collector.
- #37 AWS freshness layer operator closeout.
- #51 AWS cloud scanner implementation epic.
- #123 IaC re-platforming planner epic.
- #124 management-status read model and evidence schema.
- #125 Terraform import-plan candidate generator.
- #129 safety, redaction, and security-review gates.
- #130 evidence matching across Git, Terraform state, and cloud resources.
- #131 read-only API and MCP surfaces for IaC management status.
