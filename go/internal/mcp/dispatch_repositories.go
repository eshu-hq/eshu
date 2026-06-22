package mcp

import "net/url"

func repositoryRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "list_indexed_repositories":
		return &route{method: "GET", path: "/api/v0/repositories", query: paginationQuery(args, 100)}, true
	case "count_repositories_by_language":
		return &route{method: "GET", path: "/api/v0/repositories/by-language", query: map[string]string{
			"language": str(args, "language"),
			"limit":    "0",
			"offset":   "0",
		}}, true
	case "list_repositories_by_language":
		return &route{method: "GET", path: "/api/v0/repositories/by-language", query: map[string]string{
			"language": str(args, "language"),
			"limit":    intString(args, "limit", 100),
			"offset":   intString(args, "offset", 0),
		}}, true
	case "get_repository_language_inventory":
		return &route{method: "GET", path: "/api/v0/repositories/language-inventory", query: paginationQuery(args, 100)}, true
	case "get_repository_stats":
		repoID := str(args, "repo_id")
		if repoID == "" {
			return &route{method: "GET", path: "/api/v0/repositories"}, true
		}
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(repoID) + "/stats"}, true
	case "get_repo_context":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/context"}, true
	case "get_relationship_evidence":
		return &route{method: "GET", path: "/api/v0/evidence/relationships/" + url.PathEscape(str(args, "resolved_id"))}, true
	case "list_admission_decisions":
		return admissionDecisionsRoute(args), true
	case "list_package_registry_packages":
		return packageRegistryPackagesRoute(args), true
	case "count_package_registry_packages":
		return packageRegistryAggregateCountRoute(args), true
	case "get_package_registry_package_inventory":
		return packageRegistryAggregateInventoryRoute(args), true
	case "list_package_registry_versions":
		return packageRegistryVersionsRoute(args), true
	case "list_package_registry_dependencies":
		return packageRegistryDependenciesRoute(args), true
	case "list_package_registry_correlations":
		return packageRegistryCorrelationsRoute(args), true
	case "list_ci_cd_run_correlations":
		return cicdRunCorrelationsRoute(args), true
	case "count_ci_cd_run_correlations":
		return cicdRunCorrelationAggregateCountRoute(args), true
	case "get_ci_cd_run_correlation_inventory":
		return cicdRunCorrelationAggregateInventoryRoute(args), true
	case "list_service_catalog_correlations":
		return serviceCatalogCorrelationsRoute(args), true
	case "list_kubernetes_correlations":
		return kubernetesCorrelationsRoute(args), true
	case "list_secrets_iam_identity_trust_chains":
		return secretsIAMIdentityTrustChainsRoute(args), true
	case "list_secrets_iam_privilege_posture_observations":
		return secretsIAMPrivilegePostureObservationsRoute(args), true
	case "list_secrets_iam_secret_access_paths":
		return secretsIAMSecretAccessPathsRoute(args), true
	case "list_secrets_iam_posture_gaps":
		return secretsIAMPostureGapsRoute(args), true
	case "count_secrets_iam_posture":
		return secretsIAMPostureSummaryRoute(args), true
	case "list_observability_coverage_correlations":
		return observabilityCoverageCorrelationsRoute(args), true
	case "list_container_image_identities":
		return containerImageIdentitiesRoute(args), true
	case "count_container_image_identities":
		return containerImageIdentityAggregateCountRoute(args), true
	case "get_container_image_identity_inventory":
		return containerImageIdentityAggregateInventoryRoute(args), true
	case "list_advisory_evidence":
		return advisoryEvidenceRoute(args), true
	case "get_vulnerability_scanner_read_contract":
		return vulnerabilityScannerReadContractRoute(args), true
	case "list_supply_chain_impact_findings":
		return supplyChainImpactFindingsRoute(args), true
	case "count_supply_chain_impact_findings":
		return supplyChainImpactAggregateCountRoute(args), true
	case "get_supply_chain_impact_inventory":
		return supplyChainImpactAggregateInventoryRoute(args), true
	case "explain_supply_chain_impact":
		return supplyChainImpactExplanationRoute(args), true
	case "list_security_alert_reconciliations":
		return securityAlertReconciliationsRoute(args), true
	case "count_security_alert_reconciliations":
		return securityAlertReconciliationAggregateCountRoute(args), true
	case "get_security_alert_reconciliation_inventory":
		return securityAlertReconciliationAggregateInventoryRoute(args), true
	case "list_sbom_attestation_attachments":
		return sbomAttestationAttachmentsRoute(args), true
	case "count_sbom_attestation_attachments":
		return sbomAttestationAttachmentAggregateCountRoute(args), true
	case "get_sbom_attestation_attachment_inventory":
		return sbomAttestationAttachmentAggregateInventoryRoute(args), true
	case "get_repo_story":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/story"}, true
	case "get_repo_summary":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/stats"}, true
	case "get_repository_coverage":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/coverage"}, true
	default:
		return nil, false
	}
}
