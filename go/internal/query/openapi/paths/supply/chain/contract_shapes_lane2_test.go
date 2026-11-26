// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"testing"
)

// TestContractShapesLane2Supply pins the lane-2 shape-completeness fixes for
// the supply-chain fragments owned by this package: every 200-response
// object names its properties (no bare {"type": "object"}), every property
// carries a description naming its emitting owner, fixed-key objects carry
// exact-set pins, scalars carry type pins, and string-map filter echoes
// carry additionalProperties plus key-semantics descriptions.
func TestContractShapesLane2Supply(t *testing.T) {
	t.Parallel()

	t.Run("advisory catalog", func(t *testing.T) {
		t.Parallel()
		// Owners: listAdvisoryCatalog (catalog_handler.go) emits the page
		// envelope; advisory.CatalogRow (catalog_model.go) emits each row;
		// advisoryCatalogResponseScope emits scope.
		doc := lane2Fragment(t, AdvisoryCatalog, "AdvisoryCatalog")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/advisories", "get")
		props := lane2PropsOf(t, schema, "advisories")
		lane2RequireExactKeys(t, props, "advisories",
			"advisories", "count", "limit", "scope", "truncated", "next_cursor")
		lane2RequireScalarTypes(t, props, "advisories", map[string]string{
			"advisories": "array", "count": "integer", "limit": "integer",
			"scope": "object", "truncated": "boolean", "next_cursor": "object",
		})
		lane2RequireDescribed(t, props, "advisories",
			"advisories", "count", "limit", "scope", "truncated", "next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "advisories", "advisories"), "advisories.items")
		rowProps := lane2PropsOf(t, row, "advisories.items")
		lane2RequireExactKeys(t, rowProps, "advisories.items",
			"advisory_key", "canonical_id", "cve_id", "ghsa_id",
			"severity_label", "cvss_score", "kev", "ecosystems",
			"package_ids", "published_at", "sources")
		lane2RequireScalarTypes(t, rowProps, "advisories.items", map[string]string{
			"advisory_key": "string", "canonical_id": "string", "cve_id": "string",
			"ghsa_id": "string", "severity_label": "string", "cvss_score": "number",
			"kev": "boolean", "ecosystems": "array", "package_ids": "array",
			"published_at": "string", "sources": "array",
		})
		lane2RequireDescribed(t, rowProps, "advisories.items",
			"advisory_key", "canonical_id", "cve_id", "ghsa_id",
			"severity_label", "cvss_score", "kev", "ecosystems",
			"package_ids", "published_at", "sources")

		// Owner: listAdvisoryCatalog builds next_cursor only when
		// truncated, with the keyset cursor pair.
		cursor := lane2Child(t, props, "next_cursor", "advisories")
		lane2RequireExactKeys(t, lane2PropsOf(t, cursor, "advisories.next_cursor"),
			"advisories.next_cursor", "after_cvss", "after_advisory_key")
		lane2RequireScalarTypes(t, lane2PropsOf(t, cursor, "advisories.next_cursor"),
			"advisories.next_cursor",
			map[string]string{"after_cvss": "number", "after_advisory_key": "string"})
		lane2RequireDescribed(t, lane2PropsOf(t, cursor, "advisories.next_cursor"),
			"advisories.next_cursor", "after_cvss", "after_advisory_key")

		// Owner: advisoryCatalogResponseScope echoes only non-empty
		// filters; kev is a boolean while the rest are strings.
		scope := lane2Child(t, props, "scope", "advisories")
		scopeProps := lane2PropsOf(t, scope, "advisories.scope")
		lane2RequireExactKeys(t, scopeProps, "advisories.scope",
			"severity", "ecosystem", "q", "kev")
		lane2RequireScalarTypes(t, scopeProps, "advisories.scope", map[string]string{
			"severity": "string", "ecosystem": "string", "q": "string", "kev": "boolean",
		})
		lane2RequireDescribed(t, scopeProps, "advisories.scope",
			"severity", "ecosystem", "q", "kev")

		lane2RequireNoBareObjects(t, schema, "advisories")
	})

	t.Run("advisory evidence page", func(t *testing.T) {
		t.Parallel()
		// Owners: listAdvisoryEvidence (evidence_handler.go) emits the page
		// envelope; advisory.EvidenceRow and its nested structs
		// (evidence.go) emit each row; advisoryEvidenceResponseScope emits
		// scope; the cursor is built inline in the handler.
		doc := lane2Fragment(t, AdvisoryEvidence, "AdvisoryEvidence")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/advisories/evidence", "get")
		props := lane2PropsOf(t, schema, "evidence")
		lane2RequireExactKeys(t, props, "evidence",
			"advisories", "count", "limit", "scope", "truncated", "next_cursor")
		lane2RequireScalarTypes(t, props, "evidence", map[string]string{
			"advisories": "array", "count": "integer", "limit": "integer",
			"scope": "object", "truncated": "boolean", "next_cursor": "object",
		})
		lane2RequireDescribed(t, props, "evidence",
			"advisories", "count", "limit", "scope", "truncated", "next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "advisories", "evidence"), "evidence.advisories[]")
		rowProps := lane2PropsOf(t, row, "evidence.advisories[]")
		lane2RequireExactKeys(t, rowProps, "evidence.advisories[]",
			"advisory_key", "canonical_id", "cve_ids", "ghsa_ids", "osv_ids",
			"source_ids", "sources", "affected_packages", "affected_products",
			"epss", "kev", "references", "source_disagreements",
			"evidence_fact_ids", "latest_observed_at", "source_freshness",
			"source_confidence")
		lane2RequireScalarTypes(t, rowProps, "evidence.advisories[]", map[string]string{
			"advisory_key": "string", "canonical_id": "string",
			"latest_observed_at": "string", "source_freshness": "string",
			"source_confidence": "string",
		})
		lane2RequireDescribed(t, rowProps, "evidence.advisories[]",
			"advisory_key", "canonical_id", "cve_ids", "ghsa_ids", "osv_ids",
			"source_ids", "sources", "affected_packages", "affected_products",
			"epss", "kev", "references", "source_disagreements",
			"evidence_fact_ids", "latest_observed_at", "source_freshness",
			"source_confidence")

		// Owner: advisory.SourceEvidence.
		src := lane2ItemsOf(t, lane2Child(t, rowProps, "sources", "evidence.sources"), "evidence.sources[]")
		srcProps := lane2PropsOf(t, src, "evidence.sources[]")
		lane2RequireExactKeys(t, srcProps, "evidence.sources[]",
			"source", "advisory_id", "cve_id", "ghsa_id", "aliases",
			"published_at", "modified_at", "withdrawn_at", "severity_label",
			"cvss_score", "cvss_vector", "cvss_v2", "cvss_v3", "cvss_v4",
			"cvss_metrics", "severity", "cwes", "source_fact_ids")
		lane2RequireScalarTypes(t, srcProps, "evidence.sources[]", map[string]string{
			"source": "string", "advisory_id": "string", "cve_id": "string",
			"ghsa_id": "string", "published_at": "string", "modified_at": "string",
			"withdrawn_at": "string", "severity_label": "string",
			"cvss_score": "number", "cvss_vector": "string", "cvss_v2": "string",
			"cvss_v3": "string", "cvss_v4": "string", "cvss_metrics": "object",
			"severity": "array",
		})
		lane2RequireDescribed(t, srcProps, "evidence.sources[]",
			"source", "advisory_id", "cve_id", "ghsa_id", "aliases",
			"published_at", "modified_at", "withdrawn_at", "severity_label",
			"cvss_score", "cvss_vector", "cvss_v2", "cvss_v3", "cvss_v4",
			"cvss_metrics", "severity", "cwes", "source_fact_ids")

		// Owner: advisory.AffectedPackage.
		pkg := lane2ItemsOf(t, lane2Child(t, rowProps, "affected_packages", "evidence.packages"), "evidence.packages[]")
		pkgProps := lane2PropsOf(t, pkg, "evidence.packages[]")
		lane2RequireExactKeys(t, pkgProps, "evidence.packages[]",
			"source", "advisory_id", "cve_id", "ghsa_id", "ecosystem",
			"package_id", "purl", "affected_range", "parsed_affected_range",
			"affected_ranges", "affected_versions", "fixed_versions",
			"source_fact_id")
		lane2RequireScalarTypes(t, pkgProps, "evidence.packages[]", map[string]string{
			"source": "string", "advisory_id": "string", "cve_id": "string",
			"ghsa_id": "string", "ecosystem": "string", "package_id": "string",
			"purl": "string", "affected_range": "string",
			"parsed_affected_range": "object", "affected_ranges": "array",
			"source_fact_id": "string",
		})
		lane2RequireDescribed(t, pkgProps, "evidence.packages[]",
			"source", "advisory_id", "cve_id", "ghsa_id", "ecosystem",
			"package_id", "purl", "affected_range", "parsed_affected_range",
			"affected_ranges", "affected_versions", "fixed_versions",
			"source_fact_id")

		// Owner: advisory.AffectedProduct.
		prod := lane2ItemsOf(t, lane2Child(t, rowProps, "affected_products", "evidence.products"), "evidence.products[]")
		prodProps := lane2PropsOf(t, prod, "evidence.products[]")
		lane2RequireExactKeys(t, prodProps, "evidence.products[]",
			"source", "cve_id", "criteria", "match_criteria_id", "vulnerable",
			"version_start_including", "version_start_excluding",
			"version_end_including", "version_end_excluding",
			"source_configuration_operator", "source_configuration_negate",
			"source_node_operator", "source_node_negate", "source_fact_id")
		lane2RequireScalarTypes(t, prodProps, "evidence.products[]", map[string]string{
			"source": "string", "cve_id": "string", "criteria": "string",
			"match_criteria_id": "string", "vulnerable": "boolean",
			"source_configuration_negate": "boolean", "source_node_negate": "boolean",
			"source_fact_id": "string",
		})
		lane2RequireDescribed(t, prodProps, "evidence.products[]",
			"source", "cve_id", "criteria", "match_criteria_id", "vulnerable",
			"version_start_including", "version_start_excluding",
			"version_end_including", "version_end_excluding",
			"source_configuration_operator", "source_configuration_negate",
			"source_node_operator", "source_node_negate", "source_fact_id")

		// Owner: advisory.EPSSObservation.
		epss := lane2ItemsOf(t, lane2Child(t, rowProps, "epss", "evidence.epss"), "evidence.epss[]")
		epssProps := lane2PropsOf(t, epss, "evidence.epss[]")
		lane2RequireExactKeys(t, epssProps, "evidence.epss[]",
			"source", "cve_id", "probability", "percentile", "score_date", "fact_id")
		lane2RequireScalarTypes(t, epssProps, "evidence.epss[]", map[string]string{
			"source": "string", "cve_id": "string", "probability": "string",
			"percentile": "string", "score_date": "string", "fact_id": "string",
		})
		lane2RequireDescribed(t, epssProps, "evidence.epss[]",
			"source", "cve_id", "probability", "percentile", "score_date", "fact_id")

		// Owner: advisory.KEVObservation.
		kev := lane2ItemsOf(t, lane2Child(t, rowProps, "kev", "evidence.kev"), "evidence.kev[]")
		kevProps := lane2PropsOf(t, kev, "evidence.kev[]")
		lane2RequireExactKeys(t, kevProps, "evidence.kev[]",
			"source", "cve_id", "date_added", "required_action", "due_date",
			"known_ransomware_campaign_use", "cwes", "fact_id")
		lane2RequireDescribed(t, kevProps, "evidence.kev[]",
			"source", "cve_id", "date_added", "required_action", "due_date",
			"known_ransomware_campaign_use", "cwes", "fact_id")

		// Owner: advisory.ReferenceEvidence.
		ref := lane2ItemsOf(t, lane2Child(t, rowProps, "references", "evidence.refs"), "evidence.refs[]")
		refProps := lane2PropsOf(t, ref, "evidence.refs[]")
		lane2RequireExactKeys(t, refProps, "evidence.refs[]",
			"source", "advisory_id", "cve_id", "reference_type", "url", "fact_id")
		lane2RequireDescribed(t, refProps, "evidence.refs[]",
			"source", "advisory_id", "cve_id", "reference_type", "url", "fact_id")

		// Owner: advisory.SourceDisagreement + advisory.DisagreementValue.
		dis := lane2ItemsOf(t, lane2Child(t, rowProps, "source_disagreements", "evidence.dis"), "evidence.dis[]")
		disProps := lane2PropsOf(t, dis, "evidence.dis[]")
		lane2RequireExactKeys(t, disProps, "evidence.dis[]", "field", "values")
		lane2RequireDescribed(t, disProps, "evidence.dis[]", "field", "values")
		val := lane2ItemsOf(t, lane2Child(t, disProps, "values", "evidence.dis.values"), "evidence.dis.values[]")
		valProps := lane2PropsOf(t, val, "evidence.dis.values[]")
		lane2RequireExactKeys(t, valProps, "evidence.dis.values[]", "source", "value")
		lane2RequireDescribed(t, valProps, "evidence.dis.values[]", "source", "value")

		// Owner: listAdvisoryEvidence builds next_cursor only when
		// truncated, carrying the last row key forward.
		cursor := lane2Child(t, props, "next_cursor", "evidence")
		cursorProps := lane2PropsOf(t, cursor, "evidence.next_cursor")
		lane2RequireExactKeys(t, cursorProps, "evidence.next_cursor", "after_advisory_key")
		lane2RequireScalarTypes(t, cursorProps, "evidence.next_cursor",
			map[string]string{"after_advisory_key": "string"})
		lane2RequireDescribed(t, cursorProps, "evidence.next_cursor", "after_advisory_key")

		// Owner: advisoryEvidenceResponseScope echoes only non-empty
		// filters as string pairs; source is excluded, so the closed set
		// is the six anchor filters.
		scope := lane2Child(t, props, "scope", "evidence")
		if add, ok := scope["additionalProperties"].(map[string]any); !ok {
			t.Errorf("evidence.scope: missing additionalProperties for the string filter map")
		} else if typ, _ := add["type"].(string); typ != "string" {
			t.Errorf("evidence.scope: additionalProperties type %q, want string", typ)
		}
		if desc, _ := scope["description"].(string); desc == "" {
			t.Errorf("evidence.scope: missing filter-echo description naming key semantics")
		}

		lane2RequireNoBareObjects(t, schema, "evidence")
	})

	t.Run("vulnerability detail", func(t *testing.T) {
		t.Parallel()
		// Owner: getVulnerabilityDetail (vulnerability_detail_handler.go)
		// writes rows[0] from the same advisory evidence read model, so
		// the shape is the advisory.EvidenceRow without the page envelope.
		doc := lane2Fragment(t, AdvisoryEvidence, "AdvisoryEvidence")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/vulnerabilities/{advisory_id}", "get")
		props := lane2PropsOf(t, schema, "vuln detail")
		lane2RequireExactKeys(t, props, "vuln detail",
			"advisory_key", "canonical_id", "cve_ids", "ghsa_ids", "osv_ids",
			"source_ids", "sources", "affected_packages", "affected_products",
			"epss", "kev", "references", "source_disagreements",
			"evidence_fact_ids",
			"latest_observed_at", "source_freshness", "source_confidence")
		lane2RequireDescribed(t, props, "vuln detail",
			"advisory_key", "canonical_id", "cve_ids", "ghsa_ids", "osv_ids",
			"source_ids", "sources", "affected_packages", "affected_products",
			"epss", "kev", "references", "source_disagreements",
			"evidence_fact_ids",
			"latest_observed_at", "source_freshness", "source_confidence")
		lane2RequireNoBareObjects(t, schema, "vuln detail")
	})

	t.Run("security alert reconciliations", func(t *testing.T) {
		t.Parallel()
		// Owners: listSecurityAlertReconciliations (security_alerts.go)
		// emits the envelope; alerts.decodeRow
		// (alerts/store.go) maps each fact payload into
		// SecurityAlertReconciliationRow and its nested rows
		// (store_security_alert.go); the cursor is built inline.
		doc := lane2Fragment(t, SecurityAlerts, "SecurityAlerts")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/security-alerts/reconciliations", "get")
		props := lane2PropsOf(t, schema, "reconciliations")
		lane2RequireExactKeys(t, props, "reconciliations",
			"reconciliations", "count", "coverage", "collector_readiness",
			"limit", "truncated", "next_cursor")
		lane2RequireScalarTypes(t, props, "reconciliations", map[string]string{
			"reconciliations": "array", "count": "integer",
			"coverage": "object", "limit": "integer", "truncated": "boolean",
			"next_cursor": "object",
		})
		lane2RequireDescribed(t, props, "reconciliations",
			"reconciliations", "count", "coverage",
			"limit", "truncated", "next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "reconciliations", "reconciliations"), "reconciliations[]")
		rowProps := lane2PropsOf(t, row, "reconciliations[]")
		lane2RequireExactKeys(t, rowProps, "reconciliations[]",
			"reconciliation_id", "provider_alert", "eshu_package",
			"eshu_impact", "reconciliation_status", "reason", "reason_code",
			"impact_wait_outcome", "missing_evidence", "evidence_fact_ids",
			"source_freshness", "source_confidence")
		lane2RequireScalarTypes(t, rowProps, "reconciliations[]", map[string]string{
			"reconciliation_id": "string", "provider_alert": "object",
			"eshu_package": "object", "eshu_impact": "object",
			"reconciliation_status": "string", "reason": "string",
			"reason_code": "string", "missing_evidence": "array",
		})
		lane2RequireDescribed(t, rowProps, "reconciliations[]",
			"reconciliation_id", "provider_alert", "eshu_package",
			"eshu_impact", "reconciliation_status", "reason", "reason_code",
			"impact_wait_outcome", "missing_evidence", "evidence_fact_ids",
			"source_freshness", "source_confidence")

		// Owner: ProviderSecurityAlertRow (store_security_alert.go),
		// populated by alerts.decodeRow from the fact payload.
		pa := lane2Child(t, rowProps, "provider_alert", "reconciliations.provider_alert")
		paProps := lane2PropsOf(t, pa, "reconciliations.provider_alert")
		lane2RequireExactKeys(t, paProps, "reconciliations.provider_alert",
			"provider", "provider_alert_id", "provider_alert_number",
			"provider_state", "repository_id", "package_id", "ecosystem",
			"package_name", "manifest_path", "dependency_scope", "relationship",
			"ghsa_ids", "cve_ids", "vulnerable_range", "patched_version",
			"severity", "cvss", "epss", "cwes", "summary", "source_url",
			"created_at", "updated_at", "fixed_at", "dismissed_at",
			"collection_coverage_state", "collection_truncated",
			"collection_pages_fetched", "collection_state_filter",
			"collection_incomplete_reasons")
		lane2RequireScalarTypes(t, paProps, "reconciliations.provider_alert", map[string]string{
			"provider": "string", "provider_alert_id": "string",
			"provider_alert_number": "integer", "provider_state": "string",
			"cvss": "object", "epss": "object", "cwes": "array",
			"summary": "string", "collection_truncated": "boolean",
			"collection_pages_fetched": "integer",
		})
		lane2RequireDescribed(t, paProps, "reconciliations.provider_alert",
			"provider", "provider_alert_id", "provider_alert_number",
			"provider_state", "repository_id", "package_id", "ecosystem",
			"package_name", "manifest_path", "dependency_scope", "relationship",
			"ghsa_ids", "cve_ids", "vulnerable_range", "patched_version",
			"severity", "cvss", "epss", "cwes", "summary", "source_url",
			"created_at", "updated_at", "fixed_at", "dismissed_at",
			"collection_coverage_state", "collection_truncated",
			"collection_pages_fetched", "collection_state_filter",
			"collection_incomplete_reasons")

		// Owner: SecurityAlertEshuPackageRow; observed_version comes only
		// from Eshu dependency evidence, never provider fields.
		pkg := lane2Child(t, rowProps, "eshu_package", "reconciliations.eshu_package")
		pkgProps := lane2PropsOf(t, pkg, "reconciliations.eshu_package")
		lane2RequireExactKeys(t, pkgProps, "reconciliations.eshu_package",
			"observed_version", "requested_range", "dependency_range",
			"dependency_evidence_id", "dependency_evidence_kind",
			"missing_evidence")
		lane2RequireDescribed(t, pkgProps, "reconciliations.eshu_package",
			"observed_version", "requested_range", "dependency_range",
			"dependency_evidence_id", "dependency_evidence_kind",
			"missing_evidence")

		// Owner: SecurityAlertEshuImpactRow.
		imp := lane2Child(t, rowProps, "eshu_impact", "reconciliations.eshu_impact")
		impProps := lane2PropsOf(t, imp, "reconciliations.eshu_impact")
		lane2RequireExactKeys(t, impProps, "reconciliations.eshu_impact",
			"impact_status", "finding_id")
		lane2RequireDescribed(t, impProps, "reconciliations.eshu_impact",
			"impact_status", "finding_id")

		// Owner: SecurityAlertMissingEvidence.
		me := lane2ItemsOf(t, lane2Child(t, rowProps, "missing_evidence", "reconciliations.me"), "reconciliations.me[]")
		meProps := lane2PropsOf(t, me, "reconciliations.me[]")
		lane2RequireExactKeys(t, meProps, "reconciliations.me[]",
			"kind", "reason", "evidence_id", "detail")
		lane2RequireDescribed(t, meProps, "reconciliations.me[]",
			"kind", "reason", "evidence_id", "detail")

		// Owner: listSecurityAlertReconciliations builds next_cursor only
		// when truncated, carrying the last reconciliation id forward.
		cursor := lane2Child(t, props, "next_cursor", "reconciliations")
		cursorProps := lane2PropsOf(t, cursor, "reconciliations.next_cursor")
		lane2RequireExactKeys(t, cursorProps, "reconciliations.next_cursor",
			"after_reconciliation_id")
		lane2RequireScalarTypes(t, cursorProps, "reconciliations.next_cursor",
			map[string]string{"after_reconciliation_id": "string"})
		lane2RequireDescribed(t, cursorProps, "reconciliations.next_cursor",
			"after_reconciliation_id")

		// Coverage is the shared securityAlertCoverageSchema; its state,
		// counts, and reasons carry descriptions in the shared const.
		cov := lane2Child(t, props, "coverage", "reconciliations.coverage")
		covProps := lane2PropsOf(t, cov, "reconciliations.coverage")
		lane2RequireDescribed(t, covProps, "reconciliations.coverage",
			"state", "partial_rows", "rows_considered", "reasons")

		lane2RequireNoBareObjects(t, schema, "reconciliations")
	})
}
