# Collector And Reducer Readiness

Last updated: 2026-05-22.

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
| Vulnerability intelligence | `eshu-collector-vulnerability-intelligence` has source clients for CISA KEV, FIRST EPSS, OSV, and NVD. | Source-truth `vulnerability.*` facts exist. Impact reducers remain separate and must not infer reachability from CVSS, EPSS, or KEV alone. | Prove live source collection, API/MCP fact visibility, then package/image/deployment impact joins after upstream collectors are proven together. |

## Reducer Truth Boundaries

Current reducer domains that matter to collector readiness:

| Domain | Current truth surface | Boundary |
| --- | --- | --- |
| `cloud_asset_resolution` | Canonical cloud asset identity. | Consumes source, applied, and observed resource layers; does not belong in collector code. |
| `config_state_drift` | Bounded counters and structured logs for Terraform config-vs-state drift. | V1 is intentionally not a graph write. |
| `package_source_correlation` | Package ownership candidates, publication evidence, and manifest-backed consumption truth. | Source hints do not promote ownership by themselves. |
| `container_image_identity` | Digest-keyed image identity facts. | Mutable tags require a single active digest observation; ambiguous or stale tags stay diagnostic. |
| `aws_cloud_runtime_drift` | Durable AWS runtime drift finding facts and bounded active reads. | Collector facts are evidence; reducer admission owns drift truth. |
| `ci_cd_run_correlation` | Durable CI/CD run, artifact, and environment correlation facts. | Exact canonical writes require artifact identity evidence, not CI success alone. |
| `service_catalog_correlation` | Durable service-catalog correlation facts from explicit repository evidence. | Catalog names, owners, and labels stay provenance unless repository evidence admits exact or derived correlation. |
| `sbom_attestation_attachment` | Durable SBOM and attestation attachment facts. | Attachment requires explicit subject digest evidence; parse validity and verification trust stay separate. |
| `supply_chain_impact` | Reducer-owned vulnerability impact findings from explicit evidence paths. | CVSS, EPSS, KEV, package, image, SBOM, and repository evidence stay separate. |

Workflow completeness depends on reducer-owned phase publications only for
collector families that declare required phases. Git and Terraform-state have
required graph projection phases. AWS, OCI registry, package registry, and
documentation currently publish fact-backed or read-model truth without
required workflow phase gates.

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

## Proof Gate

Before treating a collector lane as production-ready, capture evidence for the
same deployment shape operators will use:

   No-Regression Evidence: after fixing OCI identity scheduling, focused tests
   cover one `container_image_identity` intent per OCI registry generation,
   active Git/AWS/OCI evidence loading through
   `fact_records_active_container_image_refs_idx`, and real
   `entity_metadata.container_images` parsing:
   `go test ./internal/projector -run 'TestBuildProjectionQueuesSingleContainerImageIdentityIntentForOCIRegistryFacts' -count=1`,
   `go test ./internal/storage/postgres -run 'TestFactStoreListActiveContainerImageIdentityFactsUsesActiveIdentityGenerations|TestBootstrapDefinitionsIncludeCICDRunCorrelationFactIndexes|TestBootstrapSQLFilesMirrorDefinitions' -count=1`, and
   `go test ./internal/reducer -run 'TestBuildContainerImageIdentityDecisionsReadsEntityMetadataContainerImages' -count=1`.
   Remote Compose proof with the branch-built all-collector stack then produced
   OCI facts `oci_registry.repository=11`, `oci_registry.image_manifest=154`,
   `oci_registry.image_tag_observation=154`,
   `oci_registry.image_descriptor=1353`, and
   `oci_registry.warning=154`; reducer domain
   `container_image_identity|succeeded=16`; durable
   `reducer_container_image_identity=125` facts with outcomes
   `exact_digest=70` and `tag_resolved=55`; API and MCP health probes returned
   `status=ok`; failed/retrying/dead-letter fact and workflow items were `0`.

   Observability Evidence: the projector already records
   `eshu_dp_reducer_intents_enqueued_total` and `stage=intent_enqueue`
   duration for the new scheduling path, while the reducer continues to emit
   `eshu_dp_container_image_identity_decisions_total` by bounded outcome after
   durable write success. The active fact loader runs through the existing
   Postgres instrumentation and the reducer result summary reports evaluated
   rows, outcome counts, and canonical writes.

   Read surface: standalone API/MCP reads for
   `reducer_container_image_identity` facts are exposed through
   `GET /api/v0/supply-chain/container-images/identities` and
   `list_container_image_identities`. The read model is bounded by digest,
   image reference, repository ID, or outcome plus `limit` and
   `after_identity_id` cursor pagination. It returns `identity_strength`,
   source layers, and evidence fact IDs directly while keeping weak,
   ambiguous, unresolved, and stale tag outcomes diagnostic rather than
   deployment or vulnerability impact truth.

   No-Regression Evidence: container image identity API/MCP coverage is
   focused on the bounded read contract and schema support:
   `go test ./internal/query -run 'TestSupplyChainListContainerImageIdentities|TestPostgresContainerImageIdentityStoreReportsPaginationLimit|TestContainerImageIdentityQueryUsesActiveFactReadModel|TestOpenAPISpecIncludesContainerImageIdentities' -count=1`,
   `go test ./internal/mcp -run 'TestResolveRouteMapsContainerImageIdentitiesToBoundedQuery|TestReadOnlyTools|TestMCPToolContractMatrixCoversReadOnlyTools' -count=1`,
   `go test ./cmd/api ./cmd/mcp-server -run 'TestNewRouterMountsPostgresBackedHandlers|TestNewMCPQueryRouterMountsMCPBackedHandlers' -count=1`,
   `go test ./internal/telemetry -run TestSpanNames -count=1`, and
   `go test ./internal/storage/postgres -run 'TestBootstrapDefinitionsIncludeCICDRunCorrelationFactIndexes|TestBootstrapSQLFilesMirrorDefinitions' -count=1`.

   Observability Evidence: the API and MCP route is wrapped by
   `query.container_image_identities` with stable `http.route` and
   `eshu.capability` span attributes. The storage path uses existing Postgres
   query-duration instrumentation, and responses expose `count`, `limit`,
   `truncated`, and `next_cursor` so operators can distinguish empty evidence,
   page truncation, and slow Postgres reads.

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
   The reducer now also writes provenance-only package-version publication rows
   from registry versions plus source hints, keeping publication evidence
   visible without promoting package ownership.

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

6. Service catalog correlations.
   The first service-catalog slice defines the fact-kind contract for catalog
   entity, ownership, repository link, dependency, API, operational link,
   scorecard, and warning facts. `DomainServiceCatalogCorrelation` writes
   `reducer_service_catalog_correlation` facts only when catalog entities have
   explicit repository evidence; name-only links are rejected, tombstoned
   matches are stale, and multiple active repository matches are ambiguous.
   This is not a hosted collector yet, and catalog declarations stay provenance
   until reducer evidence corroborates them with repository, service, workload,
   runtime, or deployment truth.

   No-Regression Evidence: focused fact, query, MCP, telemetry, API, and MCP
   server coverage with
   `go test ./internal/facts ./internal/reducer ./internal/query ./internal/mcp ./internal/storage/postgres ./internal/telemetry ./cmd/api ./cmd/mcp-server ./cmd/reducer -count=1`
   covers service-catalog fact-kind registry immutability, bounded
   reducer classification, durable fact writes, Postgres indexes,
   `GET /api/v0/service-catalog/correlations` filtering, active-generation and
   tombstone predicates, limit-plus-one pagination, OpenAPI, capability-matrix
   parity, MCP route mapping, MCP tool registry count, span-name registration,
   and API/MCP/reducer runtime wiring.

   Observability Evidence: `eshu_dp_service_catalog_correlations_total`
   exposes bounded reducer outcomes. `query.service_catalog_correlations`
   wraps API/MCP reads with stable `http.route` and `eshu.capability` span
   attributes. The read path uses existing Postgres query-duration
   instrumentation, and responses expose `count`, `limit`, `truncated`, and
   `next_cursor` so operators can distinguish empty evidence, page truncation,
   and slow Postgres reads.

7. SBOM and attestation attachment.
   `DomainSBOMAttestationAttachment` writes durable reducer facts for SBOM
   documents and attestation statements by explicit subject digest. The
   read model exposes `attached_verified`, `attached_unverified`,
   `attached_parse_only`, `subject_mismatch`, `ambiguous_subject`,
   `unknown_subject`, and `unparseable` without collapsing parse validity and
   verification trust into one boolean or attaching multi-subject attestations
   to an arbitrary digest. Component rows are evidence only; vulnerability
   priority and affected-by findings remain gated.

   No-Regression Evidence: focused reducer, query, MCP, storage, telemetry,
   API, and reducer command coverage with
   `go test ./internal/reducer -run 'TestBuildSBOMAttestationAttachmentDecisions|TestSBOMAttestationAttachmentHandler|TestPostgresSBOMAttestationAttachmentWriter' -count=1`,
   `go test ./internal/query -run 'TestSupplyChainListSBOMAttestationAttachments|TestSBOMAttestationAttachmentQuery|TestOpenAPISpecIncludesSBOMAttestationAttachments|TestCapabilityMatrixMatchesYAMLContract' -count=1`,
   `go test ./internal/mcp -run 'TestResolveRouteMapsSBOMAttestationAttachments|TestMCPToolContractMatrixCoversReadOnlyTools|TestReadOnlyTools|TestHandleHTTPMessage_ToolsList' -count=1`,
   `go test ./internal/storage/postgres -run 'TestListActiveSBOMAttestationAttachmentFactsQueryIsDigestBoundedAndPaged|TestBootstrapDefinitionsIncludeSBOMAttestationAttachmentFactIndexes|TestBootstrapSQLFilesMirrorDefinitions' -count=1`,
   `go test ./internal/telemetry -run 'TestSpanNames|TestInstruments' -count=1`,
   and `go test ./cmd/reducer ./cmd/api ./cmd/mcp-server -count=1` covers
   verified, failed-verification, parse-only, subject mismatch, ambiguous
   subject, unknown subject, unparseable, bounded active fact loading, Postgres
   indexes, OpenAPI, MCP, and runtime wiring contracts.

   Observability Evidence: `eshu_dp_sbom_attestation_attachments_total`
   exposes the reducer domain and bounded attachment outcome for admitted and
   suppressed decisions; the `query.sbom_attestation_attachments` span plus
   existing Postgres query duration metrics expose the read path. Attachment
   facts carry parse status, verification status, warning summaries, component
   count, source confidence, and evidence fact IDs for operator diagnosis.

8. Supply-chain impact.
   SBOM, attestation, vulnerability, package, OCI, cloud, and deployment facts
   now have the first reducer-owned confidence/read-model slice for fixture or
   preloaded facts. `DomainSupplyChainImpact` writes
   `reducer_supply_chain_impact_finding` facts with `affected_exact`,
   `affected_derived`, `possibly_affected`, `not_affected_known_fixed`, and
   `unknown_impact` statuses. The live vulnerability collector remains gated
   until the existing upstream collectors are proven together, but the impact
   reducer can explain explicit CVE/advisory -> package/component ->
   repository/image evidence paths without collapsing CVSS, EPSS, or KEV into
   reachability.

   Performance Impact Declaration: supply-chain impact affects the reducer fact
   load, durable fact write, and HTTP/MCP Postgres read path. Cardinality is
   bounded by the triggering vulnerability package/CVE set plus active package,
   SBOM, image, and package-consumption rows selected by package ID, PURL, CVE,
   or subject digest. Known-normal baseline is the existing SBOM attachment and
   package correlation read-model pattern. Proof ladder is focused reducer,
   Postgres query-shape, HTTP, MCP, telemetry, and command wiring tests, then
   package gates. Stop threshold: any unanchored fact scan, missing `limit+1`
   pagination, missing index for an advertised anchor, or package gate runtime
   materially above the adjacent SBOM/package read-model tests blocks merge.

   No-Regression Evidence: focused reducer, query, MCP, storage, telemetry, API,
   and command coverage with
   `go test ./internal/reducer -run 'TestBuildSupplyChainImpact|TestSupplyChainImpact|TestPostgresSupplyChainImpact' -count=1`,
   `go test ./internal/query -run 'TestSupplyChainListImpact|TestSupplyChainImpactFindingQuery|TestOpenAPISpecIncludesSupplyChainImpact' -count=1`,
   `go test ./internal/mcp -run 'TestResolveRouteMapsSupplyChainImpactFindingsToBoundedQuery|TestReadOnlyTools' -count=1`,
   `go test ./internal/storage/postgres -run 'TestListActiveSupplyChainImpact|TestBootstrapDefinitionsIncludeSupplyChainImpact' -count=1`,
   and `go test ./internal/reducer ./internal/query ./internal/mcp ./internal/storage/postgres ./internal/telemetry ./cmd/reducer ./cmd/api ./cmd/mcp-server -count=1`
   covers exact, derived, possible, known-fixed, and unknown statuses, bounded
   active evidence loading, active read-model predicates, OpenAPI, MCP route
   mapping, capability matrix wiring, telemetry registration, and runtime
   wiring.

   Observability Evidence: `eshu_dp_supply_chain_impact_findings_total` reports
   bounded `domain` and `outcome` dimensions for reducer decisions; the
   `query.supply_chain_impact_findings` span wraps API/MCP reads; existing
   instrumented Postgres query duration metrics expose the active fact read
   path. Finding facts carry CVE, package, status, reachability, missing
   evidence, evidence path, and evidence fact IDs for operator diagnosis.

## EKS Proof Ladder

Use this order before adding more collector families:

1. Render or deploy chart values for only implemented collectors and the API,
   MCP, ingester, reducer, and coordinator runtimes needed for the proof.
2. Prove `/healthz`, `/readyz`, `/admin/status`, and `/metrics` for each
   enabled runtime.
3. For claim-driven collectors, prove active coordinator claims, claim leases,
   heartbeat behavior, expired-claim reaping, and no duplicate open target work.
4. Confirm facts in Postgres by `collector_kind`, `fact_kind`, scope, and active
   generation.
5. Confirm reducer queues drain to zero without dead letters.
6. Confirm graph truth, read-model truth, API truth, and MCP truth agree for
   the source family being promoted.
7. Record wall time, fact count, queue count, retry count, dead-letter count,
   backend, chart values shape, image digest, and commit SHA.

If any step fails, fix the owning layer instead of adding a broader fallback.
Collection bugs belong in collectors or workflow planning. Truth bugs belong in
reducers, graph projection, or read-model stores. Operator visibility bugs
belong in status, telemetry, or runtime wiring.
