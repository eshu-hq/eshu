// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// supplyToolIntents curates router intents for the supply-chain, package
// registry, container image, SBOM, security alert, CI/CD, documentation, and
// secrets/IAM tools. Families are named in annotations_intents_families.go.
func supplyToolIntents() map[string]toolIntent {
	return map[string]toolIntent{
		"search_dockerfile_installs": intents("which dockerfiles install this package", "dockerfile package install declarations", "find dockerfile downloads by name", "search source image build installs"),
		// vulnerability-impact
		"list_supply_chain_impact_findings":  canonicalIntents("which cves affect us", "vulnerability impact findings", "vulnerable packages in repository", "cve impact by image digest", "affected vs not affected findings"),
		"count_supply_chain_impact_findings": intents("how many vulnerabilities", "count of cve findings", "vulnerability totals by severity", "number of affected packages"),
		"get_supply_chain_impact_inventory":  intents("vulnerabilities grouped by priority", "impact findings by repository", "vulnerability inventory by severity", "grouped cve impact counts"),
		"explain_supply_chain_impact":        intents("explain this vulnerability finding", "why is this cve flagged", "remediation for finding", "evidence behind cve impact", "path from advisory to workload"),
		"list_advisory_evidence":             intents("advisory evidence for cve", "ghsa advisory lookup", "advisories for package", "source advisory records"),
		"export_supply_chain_impact_packet":  intents("export supply chain evidence packet", "vulnerability investigation packet", "shareable cve impact report", "supply chain impact artifact"),
		// security-alert-reconciliation
		"list_security_alert_reconciliations":         canonicalIntents("provider security alerts", "dependabot alert reconciliation", "github security alerts vs eshu impact", "alerts by repository", "alert state for cve"),
		"count_security_alert_reconciliations":        intents("how many security alerts", "alert reconciliation totals", "count of open provider alerts", "alerts by provider state"),
		"get_security_alert_reconciliation_inventory": intents("alerts grouped by status", "alert reconciliation by package", "provider alert inventory", "grouped alert counts"),
		// sbom-attestation
		"list_sbom_attestation_attachments":         canonicalIntents("sbom for image", "attestations for image digest", "sbom attachment evidence", "which images have an sbom", "provenance attestations"),
		"count_sbom_attestation_attachments":        intents("how many sboms", "attestation totals", "count of attached sboms", "sbom attachment counts by status"),
		"get_sbom_attestation_attachment_inventory": intents("sbom attachments grouped by status", "attestations by artifact kind", "sbom inventory by subject digest", "grouped attestation counts"),
		// container-image-identity
		"list_container_image_identities":        canonicalIntents("container image identities", "image digest for tag", "which image is this reference", "image to source repository bridge", "oci repository images"),
		"count_container_image_identities":       intents("how many container images", "image identity totals", "count of exact digest images", "images resolved by tag count"),
		"get_container_image_identity_inventory": intents("images grouped by outcome", "image identity strength breakdown", "image inventory by repository", "grouped image identity counts"),
		// container image tag history (not in a family)
		"list_container_image_tag_history": intents("image tag history", "digest changes for tag", "when did tag move", "tag to digest timeline"),
		// package-registry-packages
		"list_package_registry_packages":         canonicalIntents("list packages in registry", "find package by name", "package identities by ecosystem", "npm or pypi package lookup", "registry package list"),
		"count_package_registry_packages":        intents("how many packages", "package totals by ecosystem", "count of registry packages", "number of npm packages"),
		"get_package_registry_package_inventory": intents("packages grouped by ecosystem", "package inventory by namespace", "packages by visibility", "grouped package counts"),
		"search_registry_bundles":                intents("search package bundles", "find package by purl", "registry catalog search", "package bundle by namespace"),
		// package registry versions, dependencies, correlations (not in a family)
		"list_package_registry_versions":     intents("package versions", "list versions of package", "release history of package", "version identities for package"),
		"list_package_registry_dependencies": intents("package dependencies", "dependency edges of package version", "what does this package depend on", "package native dependencies"),
		"list_package_registry_correlations": intents("who owns this package", "package publication evidence", "which repo consumes package", "package ownership candidates", "manifest backed package consumption"),
		// ci-cd
		"list_ci_cd_run_correlations":         canonicalIntents("ci cd runs for commit", "pipeline run correlation", "which build produced this image", "deployment runs by environment", "workflow run for artifact digest"),
		"count_ci_cd_run_correlations":        intents("how many pipeline runs", "ci run totals", "count of run correlations by outcome", "runs by provider count"),
		"get_ci_cd_run_correlation_inventory": intents("runs grouped by outcome", "ci runs by environment", "pipeline inventory by repository", "grouped run correlation counts"),
		// documentation-findings
		"list_documentation_findings":         canonicalIntents("documentation findings", "stale documentation claims", "docs that disagree with code", "documentation truth findings", "doc drift list"),
		"count_documentation_findings":        intents("how many documentation findings", "doc finding totals", "count of stale docs", "findings by freshness state"),
		"get_documentation_finding_inventory": intents("doc findings grouped by status", "documentation findings by type", "findings by source", "grouped documentation finding counts"),
		// documentation facts and evidence packets (not in a family)
		"list_documentation_facts":                      intents("documentation facts", "collected documentation sections", "doc links and mentions", "claim candidates from docs"),
		"get_documentation_evidence_packet":             intents("documentation evidence packet", "evidence for documentation finding", "proof behind doc finding", "evidence packet by finding id"),
		"check_documentation_evidence_packet_freshness": intents("is documentation packet current", "evidence packet freshness", "saved packet still valid", "packet version up to date"),
		// secrets-iam-posture
		"count_secrets_iam_posture":                       canonicalIntents("secrets and iam posture summary", "identity trust and secret access overview", "iam posture counts", "how risky is our secrets posture", "posture gaps overview"),
		"list_secrets_iam_identity_trust_chains":          intents("identity trust chains", "workload to service account to iam role", "vault policy trust chain", "irsa role trust"),
		"list_secrets_iam_privilege_posture_observations": intents("risky iam trust", "role with external trust and no external id", "privilege posture observations", "overly broad iam role"),
		"list_secrets_iam_secret_access_paths":            intents("who can read this secret", "vault policy to kv path", "secret access paths", "reachable secret paths"),
		"list_secrets_iam_posture_gaps":                   intents("missing secrets iam evidence", "posture gaps", "permission hidden iam evidence", "why is trust chain not exact"),
		// vulnerability scanner contract (not in a family)
		"get_vulnerability_scanner_read_contract": intents("vulnerability scanner read contract", "supported vulnerability filters", "remediation packet schema", "scanner api contract"),
	}
}
