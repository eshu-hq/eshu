# #5584 Route-Serves-Data Registry — Per-Route Derivation Rationale

Issue #5584 (epic #5470, follow-up to PR #5583 round-3 P1b, codex): the D1
route-serves-data gate compared `specs/fact-kind-registry.v1.yaml` against a
hand-maintained `routeServesDataBackingMap` that nothing cross-checked
against the real Go handlers — pure self-certifying data. The naive
`<PascalCase(domain)>Store` AST convention proposed on the issue holds for
only 6 of 17 handler structs, so the shipped fix is the registry-first
"real Option B" recorded on the issue: a committed, reviewable
domain→store/label registry built by reading each store's actual SQL/Cypher,
with the backing map validated against it.

Artifacts (all in `go/internal/mcp/`):

- `route_serves_data_registry.go` — `domainDataSignatures`: one
  discriminative signature per reducer domain (SQL fact-kind literals,
  Cypher label anchors, or `facts.<Kind>FactKind` identifiers when the SQL
  is built from the Go constant), plus the registry types.
- `route_serves_data_registry_routes.go` — `routeServesDataRegistry`: one
  entry per route naming the real registration line, handler struct,
  method, read-path files, and per-domain evidence citations.
- `route_serves_data_registry_check.go` — the verification engine: map
  cross-check (gate A), structural/citation verification against real
  source (gate B), the map-independent anti-poison scan (gate C), and
  signature-set closure.
- `route_serves_data_registry_test.go` — the gate
  (`TestRouteServesDataRegistryHonestStateGreen`) and the BITES proofs
  (`TestRouteServesDataRegistryBITES_PoisonedMapGoesRed`,
  `TestRouteServesDataRegistryBITES_PoisonedRegistryGoesRed`).

## Serving semantics

A route **serves** domain D when its registered handler's real read path
reads rows or graph nodes that D's family/reducer produces:

1. **fact-kind read** — the store's SQL filters `fact_records.fact_kind` to
   D's source-family kinds or D's reducer-output kind.
2. **derived-kind read (transitive)** — the store reads a reducer-output
   kind whose producing reducer consumes D's family kinds (the
   `/cloud/inventory` case: three provider families converge into
   `reducer_cloud_resource_identity`).
3. **graph-label read** — the route enumerates a node label that D's
   projection creates (`MERGE (x:L)`) or decorates
   (`MATCH (x:L) ... SET x.prop`). Edge-endpoint `MATCH` anchoring alone
   does **not** count (otherwise every edge writer would "serve" every node
   list).

A **disclosure** is a verified read-path touch that is deliberately not a
served-domain claim (enrichment, anchors, evidence side-channels). A
**MapOnly** claim is a backing-map row with no read-path evidence at all,
kept green but explicit and contradiction-checked.

## Gate strength and residual trust

The gate's independence from the backing map is tiered, not absolute:

- **Store-backed Served claims** (10 of 19 routes) are structurally
  map-independent: the handler struct field and the registered method
  body's `h.<Field>` reference are AST-verified against real source.
- **Marker-only Served claims** must be evidenced ON the route's own read
  path: at least one evidence marker must appear in the route's ScanFiles.
  A citation whose marker exists only off the read path (e.g. in the
  domain's own writer file) is rejected — declaring a claim is not enough.
- **MapOnly claims** are positive "declared but not served" assertions:
  the domain's signature markers must be ABSENT from the route's read
  path, so evidence appearing later forces the claim to move to Served,
  and a laundered misroute (MapOnly-ing a domain the route actually
  serves) is a contradiction.
- **Comment stripping**: marker matching runs on comment-stripped Go
  source (`stripGoComments`, length-preserving, via `go/scanner`), so a
  `// formerly used <marker>` remark can neither keep a stale citation
  green nor trip the anti-poison scan, and a commented-out registration
  or `h.<Field>` reference cannot satisfy the wiring checks. Residual:
  matching inside string literals is still textual (a marker embedded in
  an unrelated string constant would match), and non-Go evidence files —
  none today — would be matched raw.
- **Residual trust**: the anti-poison scan is only as complete as each
  route's ScanFiles list. MethodFile membership is machine-enforced; the
  query/store helper files are reviewer-maintained registry data — the
  fail direction is asymmetric (omitting a file that carries an honest
  claim's marker fails CLOSED via the read-path anchor; omitting a file
  that carries a foreign domain's marker under-scans gate C), so registry
  diffs touching ScanFiles or signatures deserve focused review.

## Route → domain derivation table

| Route | Handler.method | Served domain(s) | Load-bearing query evidence |
| --- | --- | --- | --- |
| GET /api/v0/documentation/facts | DocumentationHandler.listFacts | documentation_materialization | `(*ContentReader).documentationFacts` IN-list from `facts.Documentation*FactKind` (query/documentation_read_model.go:359-367) over `fact_records` |
| GET /api/v0/cloud/inventory | CloudInventoryHandler.listInventory | aws_cloud_runtime_drift, azure_resource_materialization, gcp_resource_materialization | `fact_kind = 'reducer_cloud_resource_identity'` (query/cloud_inventory_read_model.go:22,88); producer: reducer/cloud_inventory_admission_writer.go:18 fed by the closed set {aws_resource, gcp_cloud_resource, azure_cloud_resource} (projector/cloud_inventory_admission_intents.go:18-21) |
| GET /api/v0/ci-cd/run-correlations | CICDHandler.listRunCorrelations | ci_cd_run_correlation | `fact.fact_kind = $1` = `reducer_ci_cd_run_correlation` (query/ci_cd_run_correlations.go:15,144); writer reducer/ci_cd_run_correlation_writer.go:17 |
| GET /api/v0/repositories | RepositoryHandler.listRepositories | code_graph_projection | `MATCH (r:Repository)` page + count Cypher (query/repository.go:66,164-171) |
| GET /api/v0/supply-chain/impact/findings | SupplyChainHandler.listImpactFindings | reducer_derived_findings, supply_chain_impact | `fact.fact_kind = $1` = `reducer_supply_chain_impact_finding` (query/supply_chain_impact_findings_queries.go:6,56); kind owned by reducer_derived (specs:131-142), produced by the supply_chain_impact projection (reducer/supply_chain_impact_writer.go:19, scanner_worker family specs:346-356) |
| GET /api/v0/cloud/resources | InfraHandler.listCloudResources | ec2_instance_node_materialization, rds_posture_materialization, s3_internet_exposure_materialization | `graph_node_owner` ledger page (query/cloud_resource_list_store.go:137-142) + `MATCH (n:CloudResource)` hydration (query/cloud_resources.go:203-223); ec2 MERGEs the nodes (storage/cypher/ec2_instance_node_writer.go:33), rds/s3 decorate them (rds_posture_node_writer.go:17, s3_internet_exposure_node_writer.go:16) |
| GET /api/v0/incidents/{incident_id}/context | IncidentHandler.getIncidentContext | incident_repository_correlation, incident_routing_materialization | `fact_kind = 'incident.record' / 'incident.lifecycle_event' / 'change.record'` (query/incident_context_sql.go:36,48,61); `incident_routing.*` kinds (query/incident_context_routing_sql.go:31,51,71) |
| GET /api/v0/kubernetes/correlations | KubernetesHandler.listCorrelations | kubernetes_correlation | `fact.fact_kind = $1` = `reducer_kubernetes_correlation` (query/kubernetes_correlations.go:15,168); writer reducer/kubernetes_correlation_writer.go:17 |
| GET /api/v0/observability/coverage/correlations | ObservabilityCoverageHandler.listCorrelations | observability_coverage_correlation | `fact.fact_kind = $1` = `reducer_observability_coverage_correlation` (query/observability_coverage_correlations.go:15,172) |
| GET /api/v0/images | ImageHandler.listImages | container_image_identity | `MATCH (img:ContainerImage)` (query/images.go:30-49); label projected by container_image_identity (projector/canonical.go:251) |
| GET /api/v0/package-registry/packages | PackageRegistryHandler.listPackages | package_source_correlation | `MATCH (p:Package ...)` anchors (query/package_registry_cypher.go:6-18); label projected by package_source_correlation (projector/canonical.go:263, projector/package_registry_canonical.go) |
| GET /api/v0/secrets-iam/posture-summary | SecretsIAMHandler.summary | secrets_iam_trust_chain (+ MapOnly: s3_external_principal_grant_materialization) | four `reducer_secrets_iam_*` kinds bucketed (query/secrets_iam_summary.go:69-81,134); writer reducer/secrets_iam_trust_chain_writer.go:18-21 |
| GET /api/v0/supply-chain/sbom-attestations/attachments | SupplyChainHandler.listSBOMAttachments | sbom_attestation_attachment | `fact.fact_kind = $1` = `reducer_sbom_attestation_attachment` (query/sbom_attestation_attachments.go:28,223) |
| GET /api/v0/supply-chain/security-alerts/reconciliations | SupplyChainHandler.listSecurityAlertReconciliations | security_alert_reconciliation | `fact.fact_kind = $1` = `reducer_security_alert_reconciliation` (query/security_alert_reconciliation.go:18, _queries.go:47) |
| GET /api/v0/semantic/documentation-observations | SemanticEvidenceHandler.listDocumentationObservations | semantic_entity_materialization | SQL built from `facts.SemanticDocumentationObservationFactKind` (query/semantic_evidence.go:91, semantic_evidence_read_model.go:16-18) — a SOURCE-fact read (semanticdocs emitter), no reducer indirection |
| GET /api/v0/service-catalog/correlations | ServiceCatalogHandler.listCorrelations | service_catalog_correlation | `fact.fact_kind = $1` = `reducer_service_catalog_correlation` (query/service_catalog_correlations.go:16,199) |
| GET /api/v0/codeowners/ownership | CodeownersOwnershipHandler.listOwnership | codeowners_ownership | `(repo:Repository)-[rel:DECLARES_CODEOWNER]->(team:CodeownerTeam)` (query/codeowners_ownership_cypher.go:11-21); writer storage/cypher/canonical_codeowners_edges.go:34-35 |
| GET /api/v0/iac/resources | IaCHandler.listResources | (none served; MapOnly: config_state_drift) | candidates from `fact_kind = 'content_entity'` CONFIG entities only (query/iac_inventory_postgres.go:64-70), hydrated by `uid IN` those candidates (query/iac_resources.go:167-170,306-320) — the state projection's own uid keyspace (TerraformStateResource, MATCHES_STATE, tf_attr_*; tfstate_canonical_writer.go) is never reached (PR #5641 codex P1) |
| GET /api/v0/work-items/evidence | WorkItemHandler.listWorkItemEvidence | incident_repository_correlation | `fact.fact_kind = ANY($1)` = `facts.WorkItemFactKinds()` (query/work_item_evidence_read_kinds.go:22, work_item_evidence_sql.go); the work_item family declares reducer_domain incident_repository_correlation (specs:540-543) |

## Disclosures (verified touches that are not served-domain claims)

| Route | Domain | Why not served |
| --- | --- | --- |
| documentation/facts | semantic_entity_materialization | the collected IN-list includes `semantic.documentation_observation` (documentation_read_model.go:366), so semantic rows return here too; the family's declared surface is the semantic route |
| incidents/{id}/context | kubernetes_correlation, ci_cd_run_correlation | runtime-evidence enrichment reads (incident_context_runtime_sql.go:30,50) decorating the incident response |
| sbom-attestations/attachments | container_image_identity | `reducer_container_image_identity` appears only in the missing-evidence CTE (sbom_attestation_attachments.go:253) |
| codeowners/ownership | service_catalog_correlation | effective-owner precedence enrichment via `h.Correlations` (codeowners_ownership.go:162) |
| codeowners/ownership | code_graph_projection | `repo:Repository` is the Cypher anchor, not served rows |

## Flagged for architect review (#5584 follow-ups)

1. **MapOnly: secrets-iam/posture-summary → s3_external_principal_grant_materialization.**
   The family declares this read_surface (specs:307-317) but the summary
   handler reads none of its data; the domain materializes
   `(:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal)` graph truth
   (storage/cypher/s3_external_principal_grant_writer.go) that no
   read-surface route queries today. Either surface the grants through this
   route or re-point the family's read_surface.
2. **Shared CloudResource creators.** The base
   `MERGE (r:CloudResource {uid: row.uid})` writer
   (storage/cypher/cloud_resource_node_writer.go:22, aws_resource facts →
   aws_cloud_runtime_drift) and the azure/gcp materializations
   (reducer/azure_resource_materialization.go:93, reducer README §GCP) also
   create the nodes `/cloud/resources` enumerates, while their declared
   read_surface is `/cloud/inventory`. The backing map does not list them on
   `/cloud/resources`; their signatures were deliberately scoped to the
   inventory derived-kind so the scan stays discriminative. If the
   architect wants the map to state the full creator set, add them to
   ServedDomains with label evidence.
3. **MapOnly: iac/resources → config_state_drift** (upgraded from a
   served-domain claim after PR #5641 codex P1). The endpoint hydrates only
   `content_entity`-derived CONFIG candidates (iac_inventory_postgres.go:64-70)
   and never reaches the state projection's uid keyspace; the earlier claim
   rested on the shared, non-discriminative TerraformModule label. The
   domain's drift-finding readback
   (`POST /api/v0/terraform/config-state-drift/findings`,
   storage/postgres/terraform_config_state_drift_findings.go:18) reads
   `reducer_terraform_config_state_drift_finding`, which the registry
   assigns to reducer_derived_findings via `read_surface_overrides` — so no
   registry read_surface serves config_state_drift's state projection
   today; its nodes are only browsable through generic infra/entity-map/
   impact surfaces. Owner decision: re-point the terraform_state family's
   read_surface (likely the drift-findings route) or build a
   state-projection reader on /iac/resources.
4. **work-items/evidence domain label.** The route serves `work_item.*`
   source facts; `incident_repository_correlation` is registry-declared for
   the family (specs:543) but semantically shared with the incident_context
   family — a rename would remove the ambiguity.

## Evidence

No-Regression Evidence: no runtime query, Cypher, SQL, index, or handler
behavior changes in this PR — the new files are compile-time registry data
plus test-time verification that reads committed source files; the touched
production surface (`read_surface_route_serves_data.go`) changed only a doc
comment. Gate runtime:
`go test ./internal/mcp -run TestRouteServesDataRegistry -count=1` completes
in under one second on the full 19-route × 25-domain matrix.

No-Observability-Change: no runtime code path is added or altered; the gate
is a test-only surface, so no metrics, spans, or logs change.
