// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import "strings"

// Index is one #8880 fact_records index. Statement is the sole statement of
// its migration file, which TestNewIndexStatementsMatchMigrations pins byte
// for byte.
type Index struct {
	Name      string
	Migration string
	Statement string
	// Reason states why the index exists (census class and anchor gap).
	Reason string
}

// Index names the catalog anchors on. Existing indexes are named where a
// branch plans on them; new ones are created by migrations 212 to 225.
const (
	IndexVulnerabilityCVEV2        = "fact_records_vulnerability_active_cve_lookup_v2_idx"
	IndexVulnerabilityAdvisoryV2   = "fact_records_vulnerability_active_advisory_lookup_v2_idx"
	IndexAffectedPackagePackageID  = "fact_records_vulnerability_affected_package_lookup_idx"
	IndexAffectedPackagePURL       = "fact_records_vulnerability_package_purl_lookup_idx"
	IndexSBOMComponentPURL         = "fact_records_sbom_component_purl_idx"
	IndexSBOMComponentCPE          = "fact_records_sbom_component_cpe_idx"
	IndexSBOMComponentDocumentID   = "fact_records_sbom_component_document_id_idx"
	IndexOCIReferrerSubject        = "fact_records_oci_image_referrer_subject_idx"
	IndexAttachmentSubject         = "fact_records_sbom_attestation_attachments_subject_idx"
	IndexAttachmentDocument        = "fact_records_sbom_attestation_attachments_document_idx"
	IndexCICDArtifact              = "fact_records_ci_cd_run_correlations_artifact_lookup_idx"
	IndexCICDImageRef              = "fact_records_ci_cd_run_correlations_image_ref_idx"
	IndexCICDRepository            = "fact_records_ci_cd_run_correlations_lookup_idx"
	IndexConsumptionPackageID      = "fact_records_package_correlations_v2_lookup_idx"
	IndexConsumptionRepository     = "fact_records_package_correlations_v2_repository_lookup_idx"
	IndexWorkloadIdentityScope     = "fact_records_workload_names_scope_idx"
	IndexServiceCatalogEntity      = "fact_records_service_catalog_correlations_entity_idx"
	IndexScopeGeneration           = "fact_records_scope_generation_idx"
	IndexAffectedProductCriteria   = "fact_records_vulnerability_affected_product_criteria_idx"
	IndexPackageID                 = "fact_records_supply_chain_impact_package_id_idx"
	IndexPackageVersionPURL        = "fact_records_package_version_purl_idx"
	IndexSecurityAlertCVEID        = "fact_records_security_alert_cve_id_idx"
	IndexVulnerabilityHintAdvisory = "fact_records_vulnerability_hint_advisory_id_idx"
	IndexOCIImageDigest            = "fact_records_oci_image_digest_idx"
	IndexOCITagResolvedDigest      = "fact_records_oci_tag_resolved_digest_idx"
	IndexOCIReferrerDigest         = "fact_records_oci_referrer_digest_idx"
	IndexRepositoryPayloadScopeID  = "fact_records_supply_chain_impact_payload_scope_id_idx"
	IndexRepositoryScope           = "fact_records_supply_chain_impact_repository_scope_idx"
	IndexSuppressionActiveScan     = "fact_records_vulnerability_suppression_active_scan_idx"
	IndexPlatformActiveScan        = "fact_records_platform_materialization_active_scan_idx"
	IndexJSTSReachabilityRepoFile  = "fact_records_jsts_reachability_repo_file_idx"
	IndexSupplyChainImpactKeyGuard = "fact_records_supply_chain_impact_foreign_key_guard_idx"
)

// keyIndex renders the v2-shape key index: the payload key leads, then the
// active-generation join columns, the kind, and the fact ID, so a probe can
// join ingestion_scopes and return (fact_id, fact_kind) without the heap.
func keyIndex(name, key string, kinds []string) string {
	return "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + name + "\n" +
		"    ON fact_records ((payload->>'" + key + "'), scope_id, generation_id, fact_kind, fact_id)\n" +
		"    WHERE " + kindPredicate("fact_kind", kinds) + "\n" +
		"      AND is_tombstone = FALSE;"
}

// scanIndex renders a kind-partial (scope_id, generation_id, fact_id) anchor.
func scanIndex(name string, kinds []string) string {
	return "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + name + "\n" +
		"    ON fact_records (scope_id, generation_id, fact_id)\n" +
		"    WHERE " + kindPredicate("fact_kind", kinds) + "\n" +
		"      AND is_tombstone = FALSE;"
}

// unqualified drops the fact. qualifier so an index predicate repeats a
// branch expression over the bare columns.
func unqualified(expression string) string {
	return strings.ReplaceAll(expression, "fact.", "")
}

// NewIndexes returns the #8880 indexes in migration order.
func NewIndexes() []Index {
	return []Index{
		{
			IndexAffectedProductCriteria, "212_fact_records_vulnerability_affected_product_criteria_idx.sql",
			keyIndex(IndexAffectedProductCriteria, "criteria", []string{KindAffectedProduct}),
			"affected_product (growth) carries criteria; the existing lookup index leads on cve_id",
		},
		{
			IndexPackageID, "213_fact_records_supply_chain_impact_package_id_idx.sql",
			keyIndex(IndexPackageID, "package_id", packageIDKinds()),
			"security_alert, package_version, vulnerability_hint, and sbom.component (growth) carry package_id with no package_id-leading index",
		},
		{
			IndexPackageVersionPURL, "214_fact_records_package_version_purl_idx.sql",
			keyIndex(IndexPackageVersionPURL, "purl", []string{KindPackageVersion}),
			"package_version (growth) carries purl with no purl-leading index",
		},
		{
			IndexSecurityAlertCVEID, "215_fact_records_security_alert_cve_id_idx.sql",
			keyIndex(IndexSecurityAlertCVEID, "cve_id", []string{KindSecurityAlert}),
			"security_alert (growth) carries cve_id; the v2 cve index covers vulnerability kinds only",
		},
		{
			IndexVulnerabilityHintAdvisory, "216_fact_records_vulnerability_hint_advisory_id_idx.sql",
			keyIndex(IndexVulnerabilityHintAdvisory, "advisory_id", []string{KindVulnerabilityHint}),
			"vulnerability_hint (growth) carries advisory_id; the v2 advisory index covers vulnerability kinds only",
		},
		{
			IndexOCIImageDigest, "217_fact_records_oci_image_digest_idx.sql",
			keyIndex(IndexOCIImageDigest, "digest", []string{KindOCIImageManifest, KindOCIImageIndex}),
			"OCI manifests and indexes (growth) carry digest; the refs indexes need source_system, which the read does not state",
		},
		{
			IndexOCITagResolvedDigest, "218_fact_records_oci_tag_resolved_digest_idx.sql",
			keyIndex(IndexOCITagResolvedDigest, "resolved_digest", []string{KindOCIImageTagObservation}),
			"OCI tag observations (growth) carry resolved_digest with no resolved_digest-leading index",
		},
		{
			IndexOCIReferrerDigest, "219_fact_records_oci_referrer_digest_idx.sql",
			keyIndex(IndexOCIReferrerDigest, "referrer_digest", []string{KindOCIImageReferrer}),
			"OCI referrers (growth) carry referrer_digest; the existing referrer index leads on subject_digest",
		},
		{
			IndexRepositoryPayloadScopeID, "220_fact_records_supply_chain_impact_payload_scope_id_idx.sql",
			keyIndex(IndexRepositoryPayloadScopeID, "scope_id", repositoryGrowthKinds()),
			"ci_cd and consumption correlations (growth) carry payload scope_id, a repository arm, with no scope_id-leading payload index",
		},
		{
			IndexRepositoryScope, "221_fact_records_supply_chain_impact_repository_scope_idx.sql",
			scanIndex(IndexRepositoryScope, repositoryGrowthKinds()),
			"the resolved-scope repository arms of ci_cd and consumption (growth) need a scope-leading kind-partial anchor",
		},
		{
			IndexSuppressionActiveScan, "222_fact_records_vulnerability_suppression_active_scan_idx.sql",
			scanIndex(IndexSuppressionActiveScan, []string{KindSuppression}),
			"suppressions (bounded) are read by one full-filter statement; no index names the kind",
		},
		{
			IndexPlatformActiveScan, "223_fact_records_platform_materialization_active_scan_idx.sql",
			scanIndex(IndexPlatformActiveScan, []string{KindPlatformMaterialized}),
			"platform materializations (bounded) are read by one full-filter statement; no index names the kind",
		},
		{
			IndexJSTSReachabilityRepoFile, "224_fact_records_jsts_reachability_repo_file_idx.sql",
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + IndexJSTSReachabilityRepoFile + "\n" +
				"    ON fact_records ((payload->>'repo_id'), scope_id, generation_id, fact_id)\n" +
				"    WHERE fact_kind = 'file'\n" +
				"      AND is_tombstone = FALSE\n" +
				"      AND " + unqualified(JSTSLanguagePredicate) + ";",
			"the npm reachability file branch probes JS/TS files by repo_id without detoasting every file of the repository",
		},
		{
			IndexSupplyChainImpactKeyGuard, "225_fact_records_supply_chain_impact_foreign_key_guard_idx.sql",
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + IndexSupplyChainImpactKeyGuard + "\n" +
				"    ON fact_records (fact_kind, scope_id, generation_id, fact_id)\n" +
				"    WHERE is_tombstone = FALSE\n" +
				"      AND " + unqualified(GuardPredicate()) + ";",
			"growth and file rows carrying an identity key their branch does not probe; expected empty, makes the row set exact by construction",
		},
	}
}

func packageIDKinds() []string {
	return []string{KindSecurityAlert, KindPackageVersion, KindVulnerabilityHint, KindSBOMComponent}
}

func repositoryGrowthKinds() []string {
	return []string{KindPackageConsumption, KindCICDRunCorrelation}
}

// GuardPredicate renders the foreign-key guard predicate over fact.*: for
// every growth or file kind, the payload keys an arm applying to the kind
// compares that no branch probes for the kind. A row carrying such a key
// lands in the guard index and the guard statement evaluates the full
// retired predicate over it.
func GuardPredicate() string {
	var parts []string
	for _, info := range Kinds() {
		if info.Class == ClassBounded {
			continue
		}
		omitted := OmittedKeys(info.Kind)
		if len(omitted) == 0 {
			continue
		}
		presence := make([]string, 0, len(omitted))
		for _, key := range omitted {
			presence = append(presence, "fact.payload ? '"+key+"'")
		}
		parts = append(parts, "(fact.fact_kind = '"+info.Kind+"' AND ("+strings.Join(presence, " OR ")+"))")
	}
	return "(\n        " + strings.Join(parts, "\n        OR ") + "\n      )"
}

// OmittedKeys returns, in arm order, the payload keys an applicable arm
// compares for kind that no key-probe branch serves for kind.
func OmittedKeys(kind string) []string {
	served := map[string]bool{}
	for _, branch := range probeBranches() {
		if branch.ProbeKey == "" {
			continue
		}
		for _, branchKind := range branch.Kinds {
			if branchKind == kind {
				served[branch.ProbeKey] = true
			}
		}
	}
	seen := map[string]bool{}
	var omitted []string
	for _, candidate := range retiredArms() {
		if candidate.key == "" || !candidate.applies(kind) || served[candidate.key] || seen[candidate.key] {
			continue
		}
		seen[candidate.key] = true
		omitted = append(omitted, candidate.key)
	}
	return omitted
}
