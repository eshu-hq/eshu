# Collector And Reducer Readiness

Use this page to decide whether a source family is ready for deployed
collection, reducer materialization, and API or MCP reads. Eshu is facts-first:
collectors and webhooks observe source systems and commit facts; the resolution
engine owns graph truth, read-model truth, retries, dead letters, and completion
state.

A collector is not production-ready just because its binary exists. The
deployment path must also prove bounded collection, durable facts, reducer
drain, and operator-visible status.

## Current Contract

The implemented deployed collector lane is:

- direct Confluence collection
- direct OCI registry collection, with claim-aware runtime support outside the
  public chart path
- claim-driven Terraform-state collection
- claim-driven AWS cloud collection
- claim-driven package-registry collection
- remote-E2E-gated vulnerability intelligence collection
- webhook listener intake for Git provider events and AWS freshness triggers

Do not add chart values for design-only collectors. A Helm knob is an operator
promise; only chart collectors whose binary, fact contract, configuration, and
runtime status path exist.

Claim-driven collectors require an active workflow coordinator. The public Helm
chart rejects Terraform-state, AWS cloud, or package-registry collector
Deployments unless all of these are true:

- `workflowCoordinator.enabled=true`
- `workflowCoordinator.deploymentMode=active`
- `workflowCoordinator.claimsEnabled=true`
- `workflowCoordinator.collectorInstances` contains at least one instance

The runtime has the same guardrail: active coordinator mode requires claims to
be enabled and at least one enabled claim-capable collector instance. Individual
claim-driven collectors also reject missing, disabled, or non-claimable
instances.

## Implemented Collector Lanes

| Source family | Runtime state | Reducer and read state | Readiness gap |
| --- | --- | --- | --- |
| Git and repository | Ingester and Git collector paths emit repository, parser, relationship, and follow-up facts. | Workload identity, deployment mapping, code-call, semantic entity, SQL relationship, inheritance, package-source, and shared projection domains exist. | Prove sync, fact commit, queue drain, graph projection, and API/MCP truth on the target cluster. |
| Confluence documentation | `eshu-collector-confluence` reads one bounded Confluence scope. | Emits documentation source, document, section, link, and optional claim-candidate facts. Documentation facts remain evidence unless a reducer domain admits them. | Prove the configured Confluence scope, credentials, metrics, and status in the target environment. |
| OCI registry | `eshu-collector-oci-registry` reads registry targets. Runtime code supports direct and claim-aware modes. | Container image identity is digest-first. Explicit digests and single tag observations can become reducer image identity facts; ambiguous, unresolved, or stale tags stay diagnostic. | Prove registry collection in the target environment and keep image reads digest-bound before vulnerability impact work. |
| Terraform state | `eshu-collector-terraform-state` is claim-driven. | Terraform-state facts feed graph projection and `config_state_drift`. Drift v1 emits bounded counters and structured logs; graph/read-model promotion remains separate. | Prove live local or S3 state collection, redaction policy version, claim handoff, reducer drain, and management-status reads together. |
| AWS cloud | `eshu-collector-aws-cloud` is claim-driven. | AWS facts feed cloud-asset and AWS runtime-drift domains. AWS runtime drift writes durable reducer facts and bounded Postgres reads; graph shape remains reducer-owned. | Prove read-only AWS collection, claim-scoped credentials, AWS service coverage, reducer drain, drift reads, and status visibility in the target environment. |
| AWS freshness | `eshu-webhook-listener` accepts AWS freshness events and stores durable triggers. | Freshness narrows the next AWS collection target. Scheduled scans remain the baseline completeness path. | Prove one live AWS EventBridge or AWS Config sample through webhook intake, trigger handoff, AWS work creation, and final status. |
| Package registry | `eshu-collector-package-registry` is claim-driven. | Package source correlation classifies source hints without ownership promotion. Package-native dependency and publication facts are safe as provenance/read-model evidence. | Expand ownership and usage correlation only after exact, derived, ambiguous, unresolved, stale, and rejected cases are proven. |
| Vulnerability intelligence | `eshu-collector-vulnerability-intelligence` has source clients for CISA KEV, FIRST EPSS, OSV, and NVD. | Source-truth `vulnerability.*` facts exist. Impact reducers require owned package-manifest, repository, image, or SBOM evidence before publishing user-facing impact findings. They must not infer reachability from CVSS, EPSS, KEV, product-only CPEs, or package-registry facts alone. | Prove live source collection, API/MCP fact visibility, then package/image/deployment impact joins after upstream collectors are proven together. |

## Reducer Truth Boundaries

Collector readiness depends on the reducer admitting explicit evidence, not on
the collector naming something truth. Current reducer-owned surfaces include:

| Domain | Operator contract |
| --- | --- |
| `cloud_asset_resolution` | Cloud asset identity is admitted from source, applied, and observed resource layers. |
| `config_state_drift` | Terraform config-vs-state drift v1 emits bounded counters and logs; it is not a graph write. |
| `package_source_correlation` | Package source hints stay provenance until exact, derived, ambiguous, unresolved, stale, and rejected cases are proven. |
| `container_image_identity` | Image identity is digest-keyed; weak, ambiguous, unresolved, or stale tag observations stay diagnostic. |
| `aws_cloud_runtime_drift` | AWS drift findings are durable reducer facts with bounded Postgres reads; graph shape remains reducer-owned. |
| `ci_cd_run_correlation` | Exact CI/CD correlation requires artifact identity evidence, not CI success alone. |
| `service_catalog_correlation` | Catalog names, owners, and labels remain provenance until explicit repository evidence admits correlation. |
| `sbom_attestation_attachment` | SBOM and attestation attachment requires explicit subject digest evidence; parse validity and verification trust stay separate. |
| `supply_chain_impact` | Vulnerability impact findings come from explicit CVE/advisory to package/component to repository/image evidence paths. Source-only vulnerability intelligence is retained as facts but stays out of user-facing impact findings until it joins to owned package-manifest, repository, image, or SBOM evidence. |

Workflow completeness depends on reducer-owned phase publications only for
collector families that declare required phases. Git and Terraform-state have
required graph projection phases. AWS, OCI registry, package registry, and
documentation currently publish fact-backed or read-model truth without
required workflow phase gates.

No-Regression Evidence: the anchored-impact correction ran
`go test ./internal/reducer -run
'TestBuildSupplyChainImpactFindings(SkipsProductOnlyEvidenceWithoutOwnedSBOM|ClassifiesEvidencePaths|SkipsNonVulnerableNVDProductCriteria|DerivesProductImpactFromSBOMCPE|RequiresAffectedVersionForExactImpact)'
-count=1`, `go test ./internal/reducer ./internal/storage/postgres
./internal/query ./internal/mcp -count=1`, and
`go test ./internal/reducer ./internal/query ./internal/mcp
./internal/storage/postgres ./internal/telemetry ./cmd/reducer ./cmd/api
./cmd/mcp-server -count=1`. The input shape covers product-only CPE source
intelligence, non-vulnerable CPE rows, package-only source intelligence,
known-fixed package-manifest evidence, package-manifest repository anchors, and
CPE/SBOM/image anchors. Product-only and source-only rows produce zero impact
facts; anchored rows keep their exact, derived, possible, or known-fixed status.
Remote Compose run `pr573-anchored-impact-20260523T162055Z` completed the
45-repository smoke corpus with `435/435` queue rows succeeded, zero pending,
retrying, failed, or dead-letter rows, and workflow counts `aws=19`,
`oci_registry=1`, `package_registry=2`, `terraform_state=1`, and
`vulnerability_intelligence=4` completed. Source intelligence was still stored
(`vulnerability.cve=8`, `vulnerability.affected_package=14`,
`vulnerability.affected_product=19`), while unanchored
`reducer_supply_chain_impact_finding` facts were absent. API
`GET /api/v0/supply-chain/impact/findings?impact_status=possibly_affected&limit=200`
returned `count=0`, `truncated=false`; MCP
`list_supply_chain_impact_findings` returned the same zero-result envelope with
truth `exact`, profile `production`, and freshness `fresh`.

Observability Evidence: no telemetry contract changed for the anchored-impact
correction. Existing reducer outcome counts, `query.supply_chain_impact_findings`
spans, Postgres query duration metrics, API/MCP truth envelopes, and the
missing-evidence payload explain whether a finding is anchored to a repository,
subject digest, package manifest, or image/SBOM path.

## Gated Source Families

Do not present these as deployed collector lanes until their hosted runtime,
fact contract, reducer contract, fixtures, telemetry, and chart path are all
implemented:

| Source family | Current state |
| --- | --- |
| Kubernetes live | No hosted collector runtime or charted workload. |
| SBOM and attestation | Fact contracts and reducer attachment exist; hosted collector wiring is not a deployed lane. |
| CI/CD runs | Fixture normalizer and reducer correlation exist; hosted provider polling is not a deployed lane. |
| Service catalog, observability, incident/change, secrets/IAM posture, GCP, Azure, multi-cloud | Design or research only for deployed collector readiness. |

## Promotion Proof

Before treating a collector lane as production-ready, capture evidence for the
same deployment shape operators will use:

1. Render or deploy only implemented collectors plus the API, MCP, ingester,
   reducer, and coordinator runtimes needed for the proof.
2. Prove `/healthz`, `/readyz`, `/admin/status`, and `/metrics` for each
   enabled runtime that exposes the shared admin surface.
3. For claim-driven collectors, prove active coordinator claims, claim leases,
   heartbeat behavior, expired-claim reaping, and no duplicate open target work.
4. Confirm facts in Postgres by `collector_kind`, `fact_kind`, scope, and active
   generation.
5. Confirm reducer queues drain to zero without dead letters.
6. Confirm graph truth, read-model truth, API truth, and MCP truth agree for
   the source family being promoted.
7. Record wall time, fact count, queue count, retry count, dead-letter count,
   backend, chart values shape, image digest, and commit SHA.

Keep the detailed test matrix with the package that owns the behavior. Start
with the collector, workflow, reducer, query, MCP, telemetry, and storage
package READMEs instead of duplicating their local contracts here.

If any step fails, fix the owning layer instead of adding a broader fallback.
Collection bugs belong in collectors or workflow planning. Truth bugs belong in
reducers, graph projection, or read-model stores. Operator visibility bugs
belong in status, telemetry, or runtime wiring.

## Maintainer Details

Implementation details live with the owning packages:

- `go/internal/collector/README.md`
- `go/internal/workflow/README.md`
- `go/internal/reducer/README.md`
- `go/internal/query/README.md`
- `go/internal/storage/postgres/README.md`
