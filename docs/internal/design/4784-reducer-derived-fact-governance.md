# Reducer-Derived Fact Governance

Status: proposed decision for #4784.
Parent epic: #4783.
Related design issue: #4585.

## Problem

Contract System v1 governs collector payloads, but reducer-derived fact rows
are still a separate surface. The reducer writers build `map[string]any`
payloads, marshal them to JSON, and insert directly into `fact_records`
through the reducer-local SQL path. The canonical example is
`go/internal/reducer/supply_chain_impact_writer.go:17-59,127-174`; the shared
insert is `go/internal/reducer/workload_identity_writer.go:17-49`.

Those rows are not theoretical internals. Many are read directly by HTTP, MCP,
Postgres read models, readiness checks, search, supply-chain explanations, or
incident/runtime context. Others are only reducer breadcrumbs with one in-tree
producer and no durable read contract. Treating both classes the same would be
wrong in opposite directions: full governance is necessary for public read
contracts, but busywork for internal-only rows would dilute the contract system
without improving accuracy.

## Decision

Reducer-derived facts use two governance classes.

**Full governance** means:

- a fact-kind registry entry that reserves the reducer-owned kind and names the
  read surface;
- a typed `sdk/go/factschema` payload struct;
- a generated JSON Schema under `sdk/go/factschema/schema`;
- a typed reducer writer that builds the struct, validates the payload before
  persistence, and stamps the supported schema version for new rows;
- typed decode/read seams for query, MCP, and Postgres consumers.

This is the default for every reducer-derived fact that crosses a query,
MCP/API, public documentation, capability-matrix, or cross-domain read-model
boundary.

**Documented exemption** means a kind stays out of W1 struct/schema work because
it is reducer-internal: it has exactly one in-tree producer, no public read
surface, no external collector boundary, and no known consumer outside reducer
diagnostics. The exemption extends the #4752 `admission_exempt` precedent:
registry membership, schema metadata, and version admission are separate axes.
When reducer-derived kinds are first added to the registry, any exempt reducer
kind must use an explicit reducer-internal exemption class with
`read_surface: none`, no `payload_schema` requirement, and no schema-version
admission. It must not be registered as an ordinary full-governance row. If an
exempt kind later gains an API/MCP/read-model consumer, it must be promoted to
full governance before that consumer ships.

## Relationship To #4585

#4585 should not create a graph `Finding` node and should not collapse domain
truth models. Its stated direction is a thin unified finding envelope on the
query/MCP surface over existing per-domain stores. This ADR adopts that joint
position.

The reducer finding rows that participate in that envelope need full governance:

- `reducer_supply_chain_impact_finding`
- `reducer_aws_cloud_runtime_drift_finding`
- `reducer_multi_cloud_runtime_drift_finding`

The common #4585 envelope should decode those typed payloads and normalize
status, severity, scope, evidence fact IDs, suppression state, and truth labels
without changing the domain-owned payloads. `documentation_finding` is part of
#4585 but not part of this ADR because it is not a `reducer_*` fact kind.

## Disposition Table

| Reducer fact kind | Decision | Rationale and W1/W2 guidance |
| --- | --- | --- |
| `reducer_supply_chain_impact_finding` | Full governance | Producer writes the supply-chain finding payload in `go/internal/reducer/supply_chain_impact_writer.go:17-59,127-174`. It is a public HTTP/MCP finding source (`docs/public/reference/mcp-prompt-surface-audit.md:131`, `go/internal/query/supply_chain_impact_findings_queries.go:6`) and part of #4585. W1 adds struct/schema/typed writer; W2 moves query, aggregate, explanation, and Postgres loader reads to typed decode. |
| `reducer_aws_cloud_runtime_drift_finding` | Full governance | Producer writes AWS runtime drift findings in `go/internal/reducer/aws_cloud_runtime_drift_writer.go:19-59,102-143`; #4585 names this as one of the existing domain finding models. The AWS-specific store already treats it as an exported Postgres read surface (`go/internal/storage/postgres/aws_cloud_runtime_drift_findings.go:16`). W1 governs it with the drift finding family; W2 aligns it with the #4585 envelope. |
| `reducer_multi_cloud_runtime_drift_finding` | Full governance | Producer writes provider-neutral drift findings in `go/internal/reducer/multi_cloud_runtime_drift_writer.go:20-61,104-150`. It is explicitly supported in the capability matrix and MCP/query readback (`specs/capability-matrix/cloud-runtime-drift-readback.v1.yaml:6,24-27`, `go/internal/query/cloud_runtime_drift.go:20,99,118`, `go/internal/mcp/tools_cloud_runtime_drift.go:9-17`). W1 governs; W2 typed-decodes the cloud runtime drift store and #4585 envelope. |
| `reducer_cloud_resource_identity` | Full governance | Producer writes canonical cloud inventory rows in `go/internal/reducer/cloud_inventory_admission_writer.go:18-75,170-216`. It is a public cloud inventory readback (`specs/capability-matrix/cloud-inventory-readback.v1.yaml:6,21-23`, `go/internal/query/cloud_inventory_readback.go:18-58`, `go/internal/mcp/tools_cloud_inventory.go:8-16`). W1 governs; W2 typed-decodes cloud inventory query/MCP/Postgres reads. |
| `reducer_container_image_identity` | Full governance | Producer writes digest identity decisions in `go/internal/reducer/container_image_identity_writer.go:17-57,108-148`. It feeds public supply-chain image identity reads and SBOM/impact explanations (`docs/public/reference/mcp-prompt-surface-audit.md:132`, `go/internal/query/container_image_identities.go:15`, `go/internal/query/sbom_attestation_attachments.go:220`). W1 governs; W2 typed-decodes image identity and supply-chain evidence consumers. |
| `reducer_sbom_attestation_attachment` | Full governance | Producer writes SBOM/attestation attachment decisions in `go/internal/reducer/sbom_attestation_attachment_writer.go:17-58,98-138`. It has public API/MCP read surfaces and aggregate indexes (`docs/public/reference/mcp-prompt-surface-audit.md:136`, `go/internal/query/sbom_attestation_attachments.go:16,243`, `go/internal/storage/postgres/schema_fact_records_sbom_test.go:37-69`). W1 governs; W2 typed-decodes attachment reads and aggregate stores. |
| `reducer_security_alert_reconciliation` | Full governance | Producer writes provider alert reconciliation payloads in `go/internal/reducer/security_alert_reconciliation_writer.go:17-80,123-184`. It is a public supply-chain security-alert read surface (`docs/public/reference/mcp-prompt-surface-audit.md:135`, `go/internal/query/security_alert_reconciliation.go:18`, `go/internal/storage/postgres/facts_active_security_alert_reconciliation.go:21,41-42`). W1 governs; W2 typed-decodes reconciliation query and Postgres consumers. |
| `reducer_ci_cd_run_correlation` | Full governance | Producer writes CI/CD run correlation rows in `go/internal/reducer/ci_cd_run_correlation_writer.go:17-64,96-127`. It is a public list/aggregate surface and incident runtime evidence source (`docs/public/reference/mcp-prompt-surface-audit.md:133`, `go/internal/query/ci_cd_run_correlations.go:15`, `go/internal/query/incident_context_runtime_sql.go:50`). W1 governs; W2 typed-decodes CI/CD query and incident context consumers. |
| `reducer_incident_repository_correlation` | Full governance | Producer writes incident-to-repository decisions in `go/internal/reducer/incident_repository_correlation_writer.go:17-81,134-155`. It gates scoped incident auth and service incident evidence (`go/internal/query/incident_context_authz_store.go:16,61`, `go/internal/query/auth_scoped_routes.go:364`, `go/internal/storage/postgres/service_incident_evidence_loader.go:17,97`). W1 governs; W2 typed-decodes incident context and authz consumers. |
| `reducer_kubernetes_correlation` | Full governance | Producer writes Kubernetes runtime correlation decisions in `go/internal/reducer/kubernetes_correlation_writer.go:17-58,107-140`. It has a dedicated query surface and incident runtime consumers (`docs/internal/design/388-kubernetes-correlation-readmodel.md:20,41`, `go/internal/query/kubernetes_correlations.go:13-17`, `go/internal/query/incident_context_runtime_sql.go:30`). W1 governs; W2 typed-decodes Kubernetes query and incident runtime reads. |
| `reducer_observability_coverage_correlation` | Full governance | Producer writes coverage/gap decisions in `go/internal/reducer/observability_coverage_correlation_writer.go:17-57,105-139`. It is a documented read model with a query surface (`docs/internal/design/391-observability-coverage-correlation.md:172,486`, `docs/public/reference/observability-evidence.md:196`, `go/internal/query/observability_coverage_correlations.go:13`). W1 governs; W2 typed-decodes observability coverage reads. |
| `reducer_package_consumption_correlation` | Full governance | Producer writes consumption decisions in `go/internal/reducer/package_correlation_writer.go:18-20,207-301`. It is a supply-chain evidence and package registry read surface (`go/internal/query/package_registry_correlations.go:16-18`, `go/internal/storage/postgres/facts_active_supply_chain_impact.go:47,84`, `go/internal/query/read-models.md:418`). W1 governs all three package correlation kinds together; W2 typed-decodes package, supply-chain, and security-alert consumers. |
| `reducer_package_ownership_correlation` | Full governance | Producer writes ownership decisions in `go/internal/reducer/package_correlation_writer.go:18,146-196`. It is consumed by package registry correlations and code-import owner resolution (`go/internal/query/package_registry_correlations.go:16-18`, `go/internal/storage/postgres/facts_active_package_ownership.go:41-42`, `go/internal/reducer/code_import_owner_facts.go:13-19`). W1 governs with the package correlation family; W2 typed-decodes owner consumers. |
| `reducer_package_publication_correlation` | Full governance | Producer writes publication decisions in `go/internal/reducer/package_correlation_writer.go:20,303-387`. It shares the package registry and code-import owner read path (`go/internal/query/package_registry_correlations.go:16-18`, `go/internal/storage/postgres/facts_active_package_ownership.go:41-42`, `go/internal/reducer/code_import_owner_facts.go:46`). W1 governs with the package correlation family; W2 typed-decodes publication consumers. |
| `reducer_service_catalog_correlation` | Full governance | Producer writes service catalog correlation rows in `go/internal/reducer/service_catalog_correlation_writer.go:17-56,97-130`. It is an API/MCP read model and supply-chain/incident evidence source (`docs/internal/design/563-service-catalog-manifest-fact-emitter.md:64,73,235`, `go/internal/query/service_catalog_correlations.go:16`, `go/internal/storage/postgres/service_catalog_id_resolver.go:14,32`). W1 governs; W2 typed-decodes service catalog, incident, and supply-chain consumers. |
| `reducer_workload_identity` | Full governance | Producer writes workload identity canonical facts in `go/internal/reducer/workload_identity_writer.go:52-90,138-164`. It is used by repository summaries and supply-chain runtime evidence (`go/internal/query/repository_read_model_summary.go:99`, `go/internal/query/content_reader_repository_catalog.go:80`, `docs/public/reference/security-intelligence-release-gate.md:69`). W1 governs; W2 typed-decodes repository summary and supply-chain evidence consumers. |
| `reducer_platform_materialization` | Full governance | Producer writes deployment/platform materialization facts in `go/internal/reducer/platform_materialization_writer.go:16-58,91-111`. It is used in repository summaries and supply-chain deployment evidence (`go/internal/query/repository_read_model_summary.go:132`, `go/internal/query/supply_chain_impact_explain_runtime_test.go:298-316`, `go/internal/query/openapi_paths_supply_chain_impact_findings.go:78`). W1 governs; W2 typed-decodes repository and supply-chain consumers. |
| `reducer_eshu_search_document` | Full governance | Producer writes curated search documents in `go/internal/reducer/eshu_search_document_domain.go:16` and `go/internal/reducer/eshu_search_document_writer.go:87,178,209,422`. It is the design-430 search lane read model (`docs/internal/design/430-nornicdb-graph-search-split.md:246`, `docs/public/reference/search-document-projection.md:99`, `go/internal/storage/postgres/eshu_search_document.go:19`). W1 governs; W2 typed-decodes search document reads. |
| `reducer_secrets_iam_identity_trust_chain` | Full governance | Producer writes secrets/IAM read models in `go/internal/reducer/secrets_iam_trust_chain_writer.go:18,35-61,123-170`. It is a public posture read model (`docs/public/reference/secrets-iam-posture-collector-contract.md:162`, `go/internal/query/secrets_iam_trust_chain.go:13-17`). W1 governs all four secrets/IAM reducer read models together; W2 typed-decodes posture query stores. |
| `reducer_secrets_iam_privilege_posture_observation` | Full governance | Producer writes the privilege posture model in `go/internal/reducer/secrets_iam_trust_chain_writer.go:19,40-48,123-170`; public docs list it as a posture output (`docs/public/reference/secrets-iam-posture-collector-contract.md:163`, `docs/internal/design/1314-secrets-iam-graph-promotion-gate.md:46-49`). W1 governs with secrets/IAM; W2 typed-decodes posture query stores. |
| `reducer_secrets_iam_secret_access_path` | Full governance | Producer writes secret access path read models in `go/internal/reducer/secrets_iam_trust_chain_writer.go:20,49-54,123-170`; public docs list it as a posture output (`docs/public/reference/secrets-iam-posture-collector-contract.md:164`, `go/internal/query/secrets_iam_posture_stores.go:14-16`). W1 governs with secrets/IAM; W2 typed-decodes posture query stores. |
| `reducer_secrets_iam_posture_gap` | Full governance | Producer writes posture gap read models in `go/internal/reducer/secrets_iam_trust_chain_writer.go:21,55-60,123-170`; query stores consume the kind (`go/internal/query/secrets_iam_posture_stores.go:14-16`) and tests already treat the payload as graph-projection evidence (`go/internal/reducer/secrets_iam_graph_projection_extract_test.go:264`). W1 governs with secrets/IAM; W2 typed-decodes posture query stores. |
| `reducer_cloud_asset_resolution` | Documented exemption | Producer writes one canonical cloud-asset reconciliation row in `go/internal/reducer/cloud_asset_resolution_writer.go:22-52,84-108`, but repository search on 2026-07-06 found no non-test query, MCP, storage, docs, or capability-matrix consumer for this kind. The public cloud inventory read contract has moved to `reducer_cloud_resource_identity` (`go/internal/query/cloud_inventory_readback.go:18-58`). W1 does not author a struct/schema for this kind; if a reader is added, promote it to full governance first. |

## W1/W2 Scope Delta

W1 should author structs, schemas, registry entries, and typed writers for the
22 full-governance kinds above. The work should batch by family to keep
registry merge order serialized:

- finding/drift: supply-chain impact, AWS drift, multi-cloud drift;
- cloud inventory: cloud resource identity;
- supply-chain support: container image identity, SBOM attachment, security
  alert reconciliation, package correlations;
- runtime/correlation: CI/CD, incident repository, Kubernetes, observability,
  service catalog, workload identity, platform materialization;
- search: Eshu search document;
- secrets/IAM: the four secrets/IAM reducer read-model kinds.

W1 should not author a struct or schema for `reducer_cloud_asset_resolution`.
If W1 or a registry-foundation PR adds reducer-derived fact kinds to the
registry, it must also add the reducer-internal exemption class and record this
kind there instead of silently omitting it.

W2 should treat every full-governance kind as a typed consumer target. Query,
MCP, and Postgres loaders that read these payloads should decode through the
factschema seam rather than raw `payload->>` SQL or `map[string]any` helpers,
except for explicitly measured SQL predicates that remain as indexed filters
and hydrate through typed decode after selection.

## Consequences

This decision intentionally makes reducer-derived public read models part of
the contract system even though they do not cross the collector boundary. The
reason is the consumer boundary: API, MCP, documented read models, and #4585's
unified finding envelope need stable field contracts just as much as source
facts do.

The cost is more W1 ceremony for 22 kinds. The benefit is that W2 can migrate
consumers to typed decode without inventing one-off structs per read path, and
future schema changes get the same schema-diff, registry, and review discipline
as source facts.

The one exemption avoids pretending that an internal-only legacy breadcrumb is
an external contract. It also gives a clear promotion rule: no public reader for
an exempt reducer fact without first moving it into full governance.
