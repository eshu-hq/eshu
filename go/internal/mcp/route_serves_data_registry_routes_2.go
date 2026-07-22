// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// routeServesDataRegistryPart2 holds the second half of the #5584 route
// registry (split for the 500-line file cap). See
// route_serves_data_registry_routes.go for the derivation contract and
// route_serves_data_registry.go for the merge.
var routeServesDataRegistryPart2 = map[string]routeServesDataSource{
	"GET /api/v0/images": {
		RegistrationFile: "go/internal/query/images.go",
		HandlerStruct:    "ImageHandler",
		StructFile:       "go/internal/query/images.go",
		Method:           "listImages",
		MethodFile:       "go/internal/query/images.go",
		ScanFiles: []string{
			"go/internal/query/images.go",
		},
		Served: []routeServedDomain{{
			Domain: "container_image_identity",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/images.go", Marker: "MATCH (img:ContainerImage)"},
			},
		}},
	},

	// PackageRegistryHandler.listPackages is a graph read anchored on the
	// Package label (go/internal/query/package_registry_cypher.go:6-18),
	// projected by the package_source_correlation domain
	// (projector/package_registry_canonical.go, projector/canonical.go:263).
	"GET /api/v0/package-registry/packages": {
		RegistrationFile: "go/internal/query/package_registry.go",
		HandlerStruct:    "PackageRegistryHandler",
		StructFile:       "go/internal/query/package_registry.go",
		Method:           "listPackages",
		MethodFile:       "go/internal/query/package_registry.go",
		ScanFiles: []string{
			"go/internal/query/package_registry.go",
			"go/internal/query/package_registry_cypher.go",
		},
		Served: []routeServedDomain{{
			Domain: "package_source_correlation",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/package_registry_cypher.go", Marker: "MATCH (p:Package"},
			},
		}},
	},

	// SecretsIAMHandler.summary -> h.Summary
	// (PostgresSecretsIAMPostureSummaryStore) buckets exactly four
	// reducer-owned kinds (secrets_iam_summary.go:69-81):
	// reducer_secrets_iam_{identity_trust_chain,privilege_posture_observation,
	// secret_access_path,posture_gap} — all written under
	// secrets_iam_trust_chain (reducer/secrets_iam_trust_chain_writer.go:18-21).
	// The s3_external_principal_grant_materialization claim has NO read-path
	// evidence — see MapOnly.
	"GET /api/v0/secrets-iam/posture-summary": {
		RegistrationFile: "go/internal/query/secrets_iam.go",
		HandlerStruct:    "SecretsIAMHandler",
		StructFile:       "go/internal/query/secrets_iam.go",
		Method:           "summary",
		MethodFile:       "go/internal/query/secrets_iam_summary.go",
		ScanFiles: []string{
			"go/internal/query/secrets_iam_summary.go",
			"go/internal/query/secrets_iam_trust_chain.go",
			"go/internal/query/secrets_iam_posture_stores.go",
		},
		Served: []routeServedDomain{{
			Domain:     "secrets_iam_trust_chain",
			StoreField: "Summary",
			StoreType:  "SecretsIAMPostureSummaryStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/secrets_iam_trust_chain.go", Marker: "reducer_secrets_iam_identity_trust_chain"},
				{File: "go/internal/query/secrets_iam_posture_stores.go", Marker: "reducer_secrets_iam_posture_gap"},
			},
		}},
		MapOnly: []routeMapOnlyClaim{{
			Domain: "s3_external_principal_grant_materialization",
			Reason: "specs/fact-kind-registry.v1.yaml:307-317 declares this read_surface for the s3_external_principal_grant family, but the summary handler reads only the four reducer_secrets_iam_* kinds; the domain materializes (:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal) graph truth (storage/cypher/s3_external_principal_grant_writer.go) that no read-surface route queries today. Flagged in #5584 for architect review: either surface the grants through this route (moving this to Served with real evidence) or re-point the family's read_surface.",
		}},
	},

	// SupplyChainHandler.listSBOMAttachments -> h.SBOMAttachments
	// (PostgresSBOMAttestationAttachmentStore): fact_kind = $1 bound to
	// "reducer_sbom_attestation_attachment"
	// (go/internal/query/sbom_attestation_attachments.go:28,223). The
	// missing-evidence CTE also touches reducer_container_image_identity —
	// disclosed, not served.
	"GET /api/v0/supply-chain/sbom-attestations/attachments": {
		RegistrationFile: "go/internal/query/supply_chain.go",
		HandlerStruct:    "SupplyChainHandler",
		StructFile:       "go/internal/query/supply_chain.go",
		Method:           "listSBOMAttachments",
		MethodFile:       "go/internal/query/supply_chain_sbom_attachments.go",
		ScanFiles: []string{
			"go/internal/query/supply_chain_sbom_attachments.go",
			"go/internal/query/sbom_attestation_attachments.go",
		},
		Served: []routeServedDomain{{
			Domain:     "sbom_attestation_attachment",
			StoreField: "SBOMAttachments",
			StoreType:  "SBOMAttestationAttachmentStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/sbom_attestation_attachments.go", Marker: "reducer_sbom_attestation_attachment"},
			},
		}},
		Disclosed: []routeDisclosure{{
			Domain: "container_image_identity",
			Reason: "missing-evidence signal only: the store's CTE counts reducer_container_image_identity rows to compute the missing_evidence flag; no image identity rows are returned. The kind's own read surface is GET /api/v0/images",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/sbom_attestation_attachments.go", Marker: "reducer_container_image_identity"},
			},
		}},
	},

	// SupplyChainHandler.listSecurityAlertReconciliations ->
	// h.SecurityAlerts (PostgresSecurityAlertReconciliationStore):
	// fact_kind = $1 bound to "reducer_security_alert_reconciliation"
	// (go/internal/query/security_alert_reconciliation.go:18,
	// security_alert_reconciliation_queries.go:47).
	"GET /api/v0/supply-chain/security-alerts/reconciliations": {
		RegistrationFile: "go/internal/query/supply_chain.go",
		HandlerStruct:    "SupplyChainHandler",
		StructFile:       "go/internal/query/supply_chain.go",
		Method:           "listSecurityAlertReconciliations",
		MethodFile:       "go/internal/query/supply_chain_security_alerts.go",
		ScanFiles: []string{
			"go/internal/query/supply_chain_security_alerts.go",
			"go/internal/query/security_alert_reconciliation.go",
			"go/internal/query/security_alert_reconciliation_queries.go",
		},
		Served: []routeServedDomain{{
			Domain:     "security_alert_reconciliation",
			StoreField: "SecurityAlerts",
			StoreType:  "SecurityAlertReconciliationStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/security_alert_reconciliation.go", Marker: "reducer_security_alert_reconciliation"},
			},
		}},
	},

	// SemanticEvidenceHandler.listDocumentationObservations reads the
	// SOURCE kind semantic.documentation_observation directly: the SQL is
	// built from facts.SemanticDocumentationObservationFactKind
	// (semantic_evidence.go:91, semantic_evidence_read_model.go:16-18,265).
	// The handler's Content field is deliberately `any` (wired to
	// *query.ContentReader in cmd/api), so the claim is marker-based.
	"GET /api/v0/semantic/documentation-observations": {
		RegistrationFile: "go/internal/query/semantic_evidence.go",
		HandlerStruct:    "SemanticEvidenceHandler",
		StructFile:       "go/internal/query/semantic_evidence.go",
		Method:           "listDocumentationObservations",
		MethodFile:       "go/internal/query/semantic_evidence.go",
		ScanFiles: []string{
			"go/internal/query/semantic_evidence.go",
			"go/internal/query/semantic_evidence_read_model.go",
		},
		Served: []routeServedDomain{{
			Domain: "semantic_entity_materialization",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/semantic_evidence.go", Marker: "facts.SemanticDocumentationObservationFactKind"},
				{File: "go/internal/query/semantic_evidence_read_model.go", Marker: "fact_records.fact_kind"},
			},
		}},
	},

	// ServiceCatalogHandler.listCorrelations -> h.Correlations
	// (PostgresServiceCatalogCorrelationStore): fact_kind = $1 bound to
	// "reducer_service_catalog_correlation"
	// (go/internal/query/service_catalog_correlations.go:16,199).
	"GET /api/v0/service-catalog/correlations": {
		RegistrationFile: "go/internal/query/service_catalog.go",
		HandlerStruct:    "ServiceCatalogHandler",
		StructFile:       "go/internal/query/service_catalog.go",
		Method:           "listCorrelations",
		MethodFile:       "go/internal/query/service_catalog.go",
		ScanFiles: []string{
			"go/internal/query/service_catalog.go",
			"go/internal/query/service_catalog_correlations.go",
		},
		Served: []routeServedDomain{{
			Domain:     "service_catalog_correlation",
			StoreField: "Correlations",
			StoreType:  "ServiceCatalogCorrelationStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/service_catalog_correlations.go", Marker: "reducer_service_catalog_correlation"},
			},
		}},
	},

	// CodeownersOwnershipHandler.listOwnership reads
	// (repo:Repository)-[rel:DECLARES_CODEOWNER]->(team:CodeownerTeam)
	// (go/internal/query/codeowners_ownership_cypher.go:11-21), written by
	// storage/cypher/canonical_codeowners_edges.go:34-35. The handler's
	// Correlations field (ServiceCatalogCorrelationStore) and the Repository
	// anchor are enrichment/anchor touches — disclosed, not served.
	"GET /api/v0/codeowners/ownership": {
		RegistrationFile: "go/internal/query/codeowners_ownership.go",
		HandlerStruct:    "CodeownersOwnershipHandler",
		StructFile:       "go/internal/query/codeowners_ownership.go",
		Method:           "listOwnership",
		MethodFile:       "go/internal/query/codeowners_ownership.go",
		ScanFiles: []string{
			"go/internal/query/codeowners_ownership.go",
			"go/internal/query/codeowners_ownership_cypher.go",
			"go/internal/query/codeowners_ownership_rows.go",
			"go/internal/query/codeowners_ownership_precedence.go",
		},
		Served: []routeServedDomain{{
			Domain: "codeowners_ownership",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/codeowners_ownership_cypher.go", Marker: "DECLARES_CODEOWNER"},
				{File: "go/internal/storage/cypher/canonical_codeowners_edges.go", Marker: "DECLARES_CODEOWNER"},
			},
		}},
		Disclosed: []routeDisclosure{
			{
				Domain: "service_catalog_correlation",
				Reason: "effective-owner enrichment only: resolveEffectiveRepositoryOwner consults reducer_service_catalog_correlation rows (via h.Correlations) to arbitrate manifest-vs-codeowners owner precedence; no correlation rows are returned. The correlations' own read surface is GET /api/v0/service-catalog/correlations",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/codeowners_ownership.go", Marker: "h.Correlations"},
				},
			},
			{
				Domain: "code_graph_projection",
				Reason: "anchor only: the ownership Cypher anchors on the Repository node (repo:Repository {id: $repo_id}) to reach DECLARES_CODEOWNER edges; Repository rows themselves are served by GET /api/v0/repositories",
				Evidence: []routeReadEvidence{
					{File: "go/internal/query/codeowners_ownership_cypher.go", Marker: ":Repository"},
				},
			},
		},
	},

	// IaCHandler.listResources selects candidate uids from active
	// content_entity facts — CONFIG-side parser entities only
	// (iac_inventory_postgres.go:64-70) — and hydrates only nodes whose uid
	// is IN those candidates (iac_resources.go:167-170,306-320). The
	// config_state_drift STATE projection writes its own state-uid keyspace
	// (TerraformStateResource nodes, MATCHES_STATE edges, tf_attr_*
	// properties; state-created TerraformModule rows carry state uids), so
	// this endpoint never reads or returns state-projection data — the
	// earlier served-domain claim rested on the shared, non-discriminative
	// TerraformModule label and was withdrawn (PR #5641 codex P1). The map
	// row is therefore a MapOnly claim: the state-specific signature is
	// genuinely absent from this read path.
	"GET /api/v0/iac/resources": {
		RegistrationFile: "go/internal/query/iac.go",
		HandlerStruct:    "IaCHandler",
		StructFile:       "go/internal/query/iac.go",
		Method:           "listResources",
		MethodFile:       "go/internal/query/iac_resources.go",
		ScanFiles: []string{
			"go/internal/query/iac_resources.go",
			"go/internal/query/iac_inventory_postgres.go",
		},
		MapOnly: []routeMapOnlyClaim{{
			Domain: "config_state_drift",
			Reason: "specs/fact-kind-registry.v1.yaml:475-500 declares this read_surface for the terraform_state family, but the endpoint hydrates only content_entity-derived CONFIG candidates (iac_inventory_postgres.go:64-70) and never reaches the state projection's own uid keyspace. The domain's dedicated finding readback (POST /api/v0/terraform/config-state-drift/findings, storage/postgres/terraform_config_state_drift_findings.go:18) reads reducer_terraform_config_state_drift_finding, which the registry assigns to reducer_derived_findings via read_surface_overrides — so no registry read_surface serves config_state_drift's state projection today; its nodes are only browsable via generic infra/entity-map/impact surfaces. Flagged in #5584/#5641 for architect review: re-point the family's read_surface (e.g. to the drift-findings route) or build a state-projection reader on this route.",
		}},
	},

	// WorkItemHandler.listWorkItemEvidence -> h.Evidence
	// (PostgresWorkItemEvidenceStore): fact_kind = ANY($1) bound to
	// facts.WorkItemFactKinds() — the work_item.* source family
	// (work_item_evidence_read_kinds.go:22, work_item_evidence_sql.go:32),
	// whose registry family declares reducer_domain
	// incident_repository_correlation (specs/fact-kind-registry.v1.yaml:543).
	"GET /api/v0/work-items/evidence": {
		RegistrationFile: "go/internal/query/work_item_evidence_handler.go",
		HandlerStruct:    "WorkItemHandler",
		StructFile:       "go/internal/query/work_item_evidence_handler.go",
		Method:           "listWorkItemEvidence",
		MethodFile:       "go/internal/query/work_item_evidence_handler.go",
		ScanFiles: []string{
			"go/internal/query/work_item_evidence_handler.go",
			"go/internal/query/work_item_evidence_sql.go",
			"go/internal/query/work_item_evidence_read_kinds.go",
			"go/internal/query/work_item_evidence_store.go",
		},
		Served: []routeServedDomain{{
			Domain:     "incident_repository_correlation",
			StoreField: "Evidence",
			StoreType:  "WorkItemEvidenceStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/work_item_evidence_read_kinds.go", Marker: "facts.WorkItemFactKinds()"},
				{File: "go/internal/facts/work_item.go", Marker: `"work_item.record"`},
			},
		}},
	},
}
