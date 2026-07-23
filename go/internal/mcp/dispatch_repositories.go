// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"net/url"
)

// repositorySummarySelector resolves the repository selector for
// get_repo_summary. repo_id is the canonical field; repo_name is a documented
// backward-compatibility alias for clients that predate repo_id and takes
// effect only when repo_id is absent.
//
// The advertised input schema lists neither field as required so that a
// legacy repo_name-only call passes schema validation under MCP clients that
// honor inputSchema (the OpenAI MCP contract test forbids a top-level
// anyOf/oneOf that would otherwise express "exactly one of"). Because schema
// validation no longer forces a selector, this helper enforces the
// "at least one present" invariant and returns a clear error when both are
// absent instead of building a malformed empty-selector path.
func repositorySummarySelector(args map[string]any) (string, error) {
	selector := str(args, "repo_id")
	if selector == "" {
		selector = str(args, "repo_name")
	}
	if selector == "" {
		return "", fmt.Errorf("repo_id or repo_name is required")
	}
	return selector, nil
}

func repositoryRoute(toolName string, args map[string]any) (*route, bool, error) {
	switch toolName {
	case "list_indexed_repositories":
		return &route{method: "GET", path: "/api/v0/repositories", query: paginationQuery(args, 100)}, true, nil
	case "count_repositories_by_language":
		return &route{method: "GET", path: "/api/v0/repositories/by-language", query: map[string]string{
			"language": str(args, "language"),
			"limit":    "0",
			"offset":   "0",
		}}, true, nil
	case "list_repositories_by_language":
		return &route{method: "GET", path: "/api/v0/repositories/by-language", query: map[string]string{
			"language": str(args, "language"),
			"limit":    intString(args, "limit", 100),
			"offset":   intString(args, "offset", 0),
		}}, true, nil
	case "get_repository_language_inventory":
		return &route{method: "GET", path: "/api/v0/repositories/language-inventory", query: paginationQuery(args, 100)}, true, nil
	case "get_repository_stats":
		repoID := str(args, "repo_id")
		if repoID == "" {
			return &route{method: "GET", path: "/api/v0/repositories"}, true, nil
		}
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(repoID) + "/stats"}, true, nil
	case "get_repo_context":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/context"}, true, nil
	case "get_relationship_evidence":
		return &route{method: "GET", path: "/api/v0/evidence/relationships/" + url.PathEscape(str(args, "resolved_id"))}, true, nil
	case "list_admission_decisions":
		return admissionDecisionsRoute(args), true, nil
	case "list_package_registry_packages":
		return packageRegistryPackagesRoute(args), true, nil
	case "count_package_registry_packages":
		return packageRegistryAggregateCountRoute(args), true, nil
	case "get_package_registry_package_inventory":
		return packageRegistryAggregateInventoryRoute(args), true, nil
	case "list_package_registry_versions":
		return packageRegistryVersionsRoute(args), true, nil
	case "list_package_registry_dependencies":
		return packageRegistryDependenciesRoute(args), true, nil
	case "list_package_registry_correlations":
		return packageRegistryCorrelationsRoute(args), true, nil
	case "list_ci_cd_run_correlations":
		return cicdRunCorrelationsRoute(args), true, nil
	case "count_ci_cd_run_correlations":
		return cicdRunCorrelationAggregateCountRoute(args), true, nil
	case "get_ci_cd_run_correlation_inventory":
		return cicdRunCorrelationAggregateInventoryRoute(args), true, nil
	case "list_service_catalog_correlations":
		return serviceCatalogCorrelationsRoute(args), true, nil
	case "list_codeowners_ownership":
		return codeownersOwnershipRoute(args), true, nil
	case "list_kubernetes_correlations":
		return kubernetesCorrelationsRoute(args), true, nil
	case "list_secrets_iam_identity_trust_chains":
		return secretsIAMIdentityTrustChainsRoute(args), true, nil
	case "list_secrets_iam_privilege_posture_observations":
		return secretsIAMPrivilegePostureObservationsRoute(args), true, nil
	case "list_secrets_iam_secret_access_paths":
		return secretsIAMSecretAccessPathsRoute(args), true, nil
	case "list_secrets_iam_posture_gaps":
		return secretsIAMPostureGapsRoute(args), true, nil
	case "count_secrets_iam_posture":
		return secretsIAMPostureSummaryRoute(args), true, nil
	case "list_observability_coverage_correlations":
		return observabilityCoverageCorrelationsRoute(args), true, nil
	case "list_container_image_identities":
		return containerImageIdentitiesRoute(args), true, nil
	case "list_container_image_tag_history":
		return containerImageTagHistoryRoute(args), true, nil
	case "count_container_image_identities":
		return containerImageIdentityAggregateCountRoute(args), true, nil
	case "get_container_image_identity_inventory":
		return containerImageIdentityAggregateInventoryRoute(args), true, nil
	case "list_advisory_evidence":
		return advisoryEvidenceRoute(args), true, nil
	case "get_vulnerability_scanner_read_contract":
		return vulnerabilityScannerReadContractRoute(args), true, nil
	case "list_supply_chain_impact_findings":
		return supplyChainImpactFindingsRoute(args), true, nil
	case "count_supply_chain_impact_findings":
		return supplyChainImpactAggregateCountRoute(args), true, nil
	case "get_supply_chain_impact_inventory":
		return supplyChainImpactAggregateInventoryRoute(args), true, nil
	case "explain_supply_chain_impact":
		return supplyChainImpactExplanationRoute(args), true, nil
	case "list_security_alert_reconciliations":
		return securityAlertReconciliationsRoute(args), true, nil
	case "count_security_alert_reconciliations":
		return securityAlertReconciliationAggregateCountRoute(args), true, nil
	case "get_security_alert_reconciliation_inventory":
		return securityAlertReconciliationAggregateInventoryRoute(args), true, nil
	case "list_sbom_attestation_attachments":
		return sbomAttestationAttachmentsRoute(args), true, nil
	case "count_sbom_attestation_attachments":
		return sbomAttestationAttachmentAggregateCountRoute(args), true, nil
	case "get_sbom_attestation_attachment_inventory":
		return sbomAttestationAttachmentAggregateInventoryRoute(args), true, nil
	case "get_repo_story":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/story"}, true, nil
	case "get_repo_summary":
		selector, err := repositorySummarySelector(args)
		if err != nil {
			return nil, true, err
		}
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(selector) + "/stats"}, true, nil
	case "get_repository_coverage":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/coverage"}, true, nil
	case "get_repository_freshness":
		return &route{method: "GET", path: "/api/v0/repositories/" + url.PathEscape(str(args, "repo_id")) + "/freshness", query: map[string]string{
			"expected_commit": str(args, "expected_commit"),
		}}, true, nil
	default:
		return nil, false, nil
	}
}
