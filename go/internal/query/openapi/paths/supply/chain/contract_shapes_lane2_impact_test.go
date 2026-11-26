// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"testing"
)

// TestContractShapesLane2Impact pins the lane-2 shape-completeness fixes for
// the impact findings and explanation fragments: the full reducer-owned
// finding shape (packet.FindingResult), the explanation envelope
// (packet.ExplanationResult and its nested builders), and the readiness
// envelope (packet.ReadinessEnvelope) they share.
func TestContractShapesLane2Impact(t *testing.T) {
	t.Parallel()

	t.Run("impact findings", func(t *testing.T) {
		t.Parallel()
		// Owners: listImpactFindings (findings_handler.go) serializes
		// packet.FindingResult rows built by impact.BuildFindingResult;
		// readiness is packet.BuildReadiness / BuildReadinessUnavailable
		// (packet.ReadinessEnvelope); the cursor is built inline.
		doc := lane2Fragment(t, impactFindings, "impactFindings")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/impact/findings", "get")
		props := lane2PropsOf(t, schema, "findings")
		lane2RequireExactKeys(t, props, "findings",
			"findings", "count", "limit", "truncated", "next_cursor",
			"detection_profile", "readiness")
		lane2RequireScalarTypes(t, props, "findings", map[string]string{
			"findings": "array", "count": "integer", "limit": "integer",
			"truncated": "boolean", "next_cursor": "object",
			"detection_profile": "string", "readiness": "object",
		})
		lane2RequireDescribed(t, props, "findings",
			"findings", "count", "limit", "truncated", "next_cursor",
			"detection_profile", "readiness")

		row := lane2ItemsOf(t, lane2Child(t, props, "findings", "findings"), "findings[]")
		rowProps := lane2PropsOf(t, row, "findings[]")
		lane2RequireExactKeys(t, rowProps, "findings[]",
			"finding_id", "cve_id", "advisory_id", "package_id", "ecosystem",
			"package_name", "purl", "product_criteria", "match_criteria_id",
			"observed_version", "requested_range", "fixed_version",
			"vulnerable_range", "match_reason", "impact_status", "confidence",
			"cvss_score", "advisory_published_at", "advisory_updated_at",
			"epss_probability", "epss_percentile", "known_exploited",
			"priority_reason", "priority_score", "priority_bucket",
			"priority_reason_codes", "priority_contributions",
			"exploited_tier", "runtime_reachability", "reachability",
			"repository_id", "subject_digest", "image_ref", "dependency_scope",
			"workload_ids", "deployment_ids", "service_ids", "environments",
			"environment_evidence", "ci_declared_artifact_digest",
			"ci_declared_image_ref", "cloud_runtime_resource_refs",
			"kubernetes_runtime_workload_refs", "kubernetes_runtime_probe",
			"runtime_context", "catalog_entity_refs", "catalog_owner_refs",
			"dependency_path", "dependency_depth", "direct_dependency",
			"missing_evidence", "evidence_path", "evidence_fact_ids",
			"source_freshness", "source_confidence", "provenance",
			"suppression", "detection_profile", "remediation",
			"deployment_truth_tier", "version_resolution_tier",
			"version_resolution_corroboration")
		lane2RequireScalarTypes(t, rowProps, "findings[]", map[string]string{
			"finding_id": "string", "impact_status": "string",
			"confidence": "string", "cvss_score": "number",
			"known_exploited": "boolean", "priority_score": "integer",
			"dependency_depth": "integer", "direct_dependency": "boolean",
		})
		lane2RequireDescribed(t, rowProps, "findings[]",
			"finding_id", "cve_id", "advisory_id", "package_id", "ecosystem",
			"package_name", "purl", "product_criteria", "match_criteria_id",
			"observed_version", "requested_range", "fixed_version",
			"vulnerable_range", "match_reason", "impact_status", "confidence",
			"cvss_score", "advisory_published_at", "advisory_updated_at",
			"epss_probability", "epss_percentile", "known_exploited",
			"priority_reason", "priority_score", "priority_bucket",
			"priority_reason_codes", "priority_contributions",
			"exploited_tier", "runtime_reachability", "reachability",
			"repository_id", "subject_digest", "image_ref", "dependency_scope",
			"workload_ids", "deployment_ids", "service_ids", "environments",
			"environment_evidence", "ci_declared_artifact_digest",
			"ci_declared_image_ref", "cloud_runtime_resource_refs",
			"kubernetes_runtime_workload_refs", "kubernetes_runtime_probe",
			"runtime_context", "catalog_entity_refs", "catalog_owner_refs",
			"dependency_path", "dependency_depth", "direct_dependency",
			"missing_evidence", "evidence_path", "evidence_fact_ids",
			"source_freshness", "source_confidence", "provenance",
			"suppression", "detection_profile", "remediation",
			"deployment_truth_tier", "version_resolution_tier",
			"version_resolution_corroboration")

		// Owner: packet.PriorityContribution.
		pc := lane2ItemsOf(t, lane2Child(t, rowProps, "priority_contributions", "findings.pc"), "findings.pc[]")
		pcProps := lane2PropsOf(t, pc, "findings.pc[]")
		lane2RequireExactKeys(t, pcProps, "findings.pc[]",
			"reason_code", "input", "value", "contribution")
		lane2RequireDescribed(t, pcProps, "findings.pc[]",
			"reason_code", "input", "value", "contribution")

		// Owner: packet.ReachabilityResult.
		reach := lane2Child(t, rowProps, "reachability", "findings.reachability")
		reachProps := lane2PropsOf(t, reach, "findings.reachability")
		lane2RequireExactKeys(t, reachProps, "findings.reachability",
			"state", "confidence", "source", "evidence", "reason",
			"language_maturity", "missing_evidence", "scan_level",
			"scan_mode", "scanner_version", "trace_count")
		lane2RequireDescribed(t, reachProps, "findings.reachability",
			"state", "confidence", "source", "evidence", "reason",
			"language_maturity", "missing_evidence", "scan_level",
			"scan_mode", "scanner_version", "trace_count")

		// Owner: packet.SuppressionDecisionRow.
		supp := lane2Child(t, rowProps, "suppression", "findings.suppression")
		suppProps := lane2PropsOf(t, supp, "findings.suppression")
		lane2RequireExactKeys(t, suppProps, "findings.suppression",
			"state", "suppression_id", "source", "justification", "author",
			"authored_at", "expires_at", "reason", "evidence_ref",
			"vex_document_id", "vex_statement_id", "matched_id",
			"matched_field", "matched_via")
		lane2RequireDescribed(t, suppProps, "findings.suppression",
			"state", "suppression_id", "source", "justification", "author",
			"authored_at", "expires_at", "reason", "evidence_ref",
			"vex_document_id", "vex_statement_id", "matched_id",
			"matched_field", "matched_via")

		// Owner: packet.Remediation.
		rem := lane2Child(t, rowProps, "remediation", "findings.remediation")
		remProps := lane2PropsOf(t, rem, "findings.remediation")
		lane2RequireExactKeys(t, remProps, "findings.remediation",
			"ecosystem", "current_version", "vulnerable_range",
			"fixed_version_source", "match_reason", "first_patched_version",
			"patched_version_branches", "manifest_range",
			"manifest_allows_fix", "direct", "parent_package", "confidence",
			"reason", "missing_evidence")
		lane2RequireScalarTypes(t, remProps, "findings.remediation", map[string]string{
			"direct": "boolean",
		})
		lane2RequireDescribed(t, remProps, "findings.remediation",
			"ecosystem", "current_version", "vulnerable_range",
			"fixed_version_source", "match_reason", "first_patched_version",
			"patched_version_branches", "manifest_range",
			"manifest_allows_fix", "direct", "parent_package", "confidence",
			"reason", "missing_evidence")
		branch := lane2ItemsOf(t, lane2Child(t, remProps, "patched_version_branches", "findings.branch"), "findings.branch[]")
		branchProps := lane2PropsOf(t, branch, "findings.branch[]")
		lane2RequireExactKeys(t, branchProps, "findings.branch[]", "version", "source")
		lane2RequireDescribed(t, branchProps, "findings.branch[]", "version", "source")

		// Owner: packet.Provenance and its nested alternates.
		prov := lane2Child(t, rowProps, "provenance", "findings.provenance")
		provProps := lane2PropsOf(t, prov, "findings.provenance")
		lane2RequireExactKeys(t, provProps, "findings.provenance",
			"selected_severity_source", "selected_severity_score",
			"selected_severity_score_source", "selected_severity_vector",
			"selected_severity_label", "selected_fixed_version_source",
			"selected_range_source", "selected_epss_score_date",
			"selected_epss_model_version", "alternate_severities",
			"fixed_version_branches", "advisory_sources",
			"advisory_retraction")
		lane2RequireScalarTypes(t, provProps, "findings.provenance", map[string]string{
			"selected_severity_score": "number",
		})
		lane2RequireDescribed(t, provProps, "findings.provenance",
			"selected_severity_source", "selected_severity_score",
			"selected_severity_score_source", "selected_severity_vector",
			"selected_severity_label", "selected_fixed_version_source",
			"selected_range_source", "selected_epss_score_date",
			"selected_epss_model_version", "alternate_severities",
			"fixed_version_branches", "advisory_sources",
			"advisory_retraction")
		alt := lane2ItemsOf(t, lane2Child(t, provProps, "alternate_severities", "findings.alt"), "findings.alt[]")
		altProps := lane2PropsOf(t, alt, "findings.alt[]")
		lane2RequireExactKeys(t, altProps, "findings.alt[]",
			"source", "score", "score_source", "vector", "label")
		lane2RequireDescribed(t, altProps, "findings.alt[]",
			"source", "score", "score_source", "vector", "label")
		asrc := lane2ItemsOf(t, lane2Child(t, provProps, "advisory_sources", "findings.asrc"), "findings.asrc[]")
		asrcProps := lane2PropsOf(t, asrc, "findings.asrc[]")
		lane2RequireExactKeys(t, asrcProps, "findings.asrc[]",
			"source", "advisory_id", "source_updated_at", "withdrawn_at",
			"status", "replaced_by")
		lane2RequireDescribed(t, asrcProps, "findings.asrc[]",
			"source", "advisory_id", "source_updated_at", "withdrawn_at",
			"status", "replaced_by")
		retr := lane2Child(t, provProps, "advisory_retraction", "findings.retr")
		retrProps := lane2PropsOf(t, retr, "findings.retr")
		lane2RequireExactKeys(t, retrProps, "findings.retr",
			"reason_code", "reason", "replaced_by", "sources")
		lane2RequireDescribed(t, retrProps, "findings.retr",
			"reason_code", "reason", "replaced_by", "sources")
		rsrc := lane2ItemsOf(t, lane2Child(t, retrProps, "sources", "findings.rsrc"), "findings.rsrc[]")
		rsrcProps := lane2PropsOf(t, rsrc, "findings.rsrc[]")
		lane2RequireExactKeys(t, rsrcProps, "findings.rsrc[]",
			"source", "advisory_id", "reason_code", "status", "withdrawn_at",
			"replaced_by")
		lane2RequireDescribed(t, rsrcProps, "findings.rsrc[]",
			"source", "advisory_id", "reason_code", "status", "withdrawn_at",
			"replaced_by")

		// Owner: packet.KubernetesRuntimeWorkloadRef (required workload_uid)
		// and packet.KubernetesRuntimeProbeMetadata.
		kref := lane2ItemsOf(t, lane2Child(t, rowProps, "kubernetes_runtime_workload_refs", "findings.kref"), "findings.kref[]")
		krefProps := lane2PropsOf(t, kref, "findings.kref[]")
		lane2RequireExactKeys(t, krefProps, "findings.kref[]",
			"workload_uid", "cluster_id", "namespace", "name")
		lane2RequireDescribed(t, krefProps, "findings.kref[]",
			"workload_uid", "cluster_id", "namespace", "name")
		probe := lane2Child(t, rowProps, "kubernetes_runtime_probe", "findings.probe")
		probeProps := lane2PropsOf(t, probe, "findings.probe")
		lane2RequireExactKeys(t, probeProps, "findings.probe",
			"candidate_limit", "workload_refs_truncated")
		lane2RequireDescribed(t, probeProps, "findings.probe",
			"candidate_limit", "workload_refs_truncated")

		// Owner: packet.VersionResolutionCorroboration.
		corro := lane2ItemsOf(t, lane2Child(t, rowProps, "version_resolution_corroboration", "findings.corro"), "findings.corro[]")
		corroProps := lane2PropsOf(t, corro, "findings.corro[]")
		lane2RequireExactKeys(t, corroProps, "findings.corro[]",
			"tier", "digest_or_version", "evidence_kind", "agreement")
		lane2RequireDescribed(t, corroProps, "findings.corro[]",
			"tier", "digest_or_version", "evidence_kind", "agreement")

		// Owner: listImpactFindings builds next_cursor only when
		// truncated, carrying the last finding id forward.
		cursor := lane2Child(t, props, "next_cursor", "findings")
		cursorProps := lane2PropsOf(t, cursor, "findings.next_cursor")
		lane2RequireExactKeys(t, cursorProps, "findings.next_cursor",
			"after_finding_id")
		lane2RequireScalarTypes(t, cursorProps, "findings.next_cursor",
			map[string]string{"after_finding_id": "string"})
		lane2RequireDescribed(t, cursorProps, "findings.next_cursor",
			"after_finding_id")

		lane2PinReadiness(t, doc, "/api/v0/supply-chain/impact/findings", "get", "findings")
		lane2RequireNoBareObjects(t, schema, "findings")
	})

	t.Run("impact explain", func(t *testing.T) {
		t.Parallel()
		// Owners: explainImpact (explain_handler.go) assembles the envelope
		// via impact.BuildExplanation / BuildNoEvidenceExplanation /
		// BuildAmbiguousExplanation (impact/explain.go) into
		// packet.ExplanationResult and its nested packet builders.
		doc := lane2Fragment(t, impactExplain, "impactExplain")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/impact/explain", "get")
		props := lane2PropsOf(t, schema, "explain")
		lane2RequireExactKeys(t, props, "explain",
			"outcome", "evidence_packet_handle", "input", "finding",
			"advisory", "component", "version", "dependency_chain",
			"anchors", "impact_path", "evidence", "readiness",
			"missing_evidence", "freshness", "remediation")
		lane2RequireScalarTypes(t, props, "explain", map[string]string{
			"outcome": "string", "input": "object", "finding": "object",
			"advisory": "object", "component": "object", "version": "object",
			"anchors": "object", "impact_path": "array", "evidence": "array",
			"readiness": "object", "missing_evidence": "array",
			"freshness": "object",
		})
		lane2RequireDescribed(t, props, "explain",
			"outcome", "evidence_packet_handle", "input", "finding",
			"advisory", "component", "version", "dependency_chain",
			"anchors", "impact_path", "evidence", "readiness",
			"missing_evidence", "freshness", "remediation")

		// Owner: packet.ExplanationFilter echoes the bounded scope the
		// caller supplied (grants are excluded from JSON).
		input := lane2Child(t, props, "input", "explain.input")
		inputProps := lane2PropsOf(t, input, "explain.input")
		lane2RequireExactKeys(t, inputProps, "explain.input",
			"finding_id", "advisory_id", "cve_id", "package_id",
			"repository_id", "subject_digest", "image_ref", "workload_id",
			"service_id")
		lane2RequireDescribed(t, inputProps, "explain.input",
			"finding_id", "advisory_id", "cve_id", "package_id",
			"repository_id", "subject_digest", "image_ref", "workload_id",
			"service_id")

		// Owner: packet.AdvisoryExplanation.
		adv := lane2Child(t, props, "advisory", "explain.advisory")
		advProps := lane2PropsOf(t, adv, "explain.advisory")
		lane2RequireExactKeys(t, advProps, "explain.advisory",
			"cve_id", "advisory_id", "vulnerable_range", "range_source",
			"selected_severity_source", "selected_fixed_version_source",
			"sources", "references")
		lane2RequireDescribed(t, advProps, "explain.advisory",
			"cve_id", "advisory_id", "vulnerable_range", "range_source",
			"selected_severity_source", "selected_fixed_version_source",
			"sources", "references")
		asrc := lane2ItemsOf(t, lane2Child(t, advProps, "sources", "explain.asrc"), "explain.asrc[]")
		asrcProps := lane2PropsOf(t, asrc, "explain.asrc[]")
		lane2RequireExactKeys(t, asrcProps, "explain.asrc[]",
			"source", "advisory_id", "source_updated_at", "withdrawn_at",
			"status", "replaced_by")
		lane2RequireDescribed(t, asrcProps, "explain.asrc[]",
			"source", "advisory_id", "source_updated_at", "withdrawn_at",
			"status", "replaced_by")

		// Owner: packet.ComponentExplanation.
		comp := lane2Child(t, props, "component", "explain.component")
		compProps := lane2PropsOf(t, comp, "explain.component")
		lane2RequireExactKeys(t, compProps, "explain.component",
			"package_id", "ecosystem", "package_name", "purl",
			"product_criteria", "match_criteria_id", "observed_version",
			"manifest_range")
		lane2RequireDescribed(t, compProps, "explain.component",
			"package_id", "ecosystem", "package_name", "purl",
			"product_criteria", "match_criteria_id", "observed_version",
			"manifest_range")

		// Owner: packet.VersionExplanation.
		ver := lane2Child(t, props, "version", "explain.version")
		verProps := lane2PropsOf(t, ver, "explain.version")
		lane2RequireExactKeys(t, verProps, "explain.version",
			"observed_version", "manifest_range", "vulnerable_range",
			"fixed_version", "version_evidence")
		lane2RequireScalarTypes(t, verProps, "explain.version", map[string]string{
			"version_evidence": "string",
		})
		lane2RequireDescribed(t, verProps, "explain.version",
			"observed_version", "manifest_range", "vulnerable_range",
			"fixed_version", "version_evidence")

		// Owner: packet.DependencyChain.
		dep := lane2Child(t, props, "dependency_chain", "explain.dependency_chain")
		depProps := lane2PropsOf(t, dep, "explain.dependency_chain")
		lane2RequireExactKeys(t, depProps, "explain.dependency_chain",
			"path", "depth", "direct_dependency")
		lane2RequireScalarTypes(t, depProps, "explain.dependency_chain", map[string]string{
			"depth": "integer", "direct_dependency": "boolean",
		})
		lane2RequireDescribed(t, depProps, "explain.dependency_chain",
			"path", "depth", "direct_dependency")

		// Owner: packet.ExplanationAnchors + packet.ProviderAlertAnchor.
		anch := lane2Child(t, props, "anchors", "explain.anchors")
		anchProps := lane2PropsOf(t, anch, "explain.anchors")
		lane2RequireExactKeys(t, anchProps, "explain.anchors",
			"repository_id", "subject_digest", "manifest_paths",
			"lockfile_paths", "sbom_documents", "image_digests", "image_refs",
			"workloads", "deployments", "services", "environments",
			"catalog_entities", "catalog_owners", "provider_alerts",
			"evidence_fact_ids")
		lane2RequireDescribed(t, anchProps, "explain.anchors",
			"repository_id", "subject_digest", "manifest_paths",
			"lockfile_paths", "sbom_documents", "image_digests", "image_refs",
			"workloads", "deployments", "services", "environments",
			"catalog_entities", "catalog_owners", "provider_alerts",
			"evidence_fact_ids")
		pa := lane2ItemsOf(t, lane2Child(t, anchProps, "provider_alerts", "explain.pa"), "explain.pa[]")
		paProps := lane2PropsOf(t, pa, "explain.pa[]")
		lane2RequireExactKeys(t, paProps, "explain.pa[]",
			"provider", "alert_id", "state", "manifest_path")
		lane2RequireDescribed(t, paProps, "explain.pa[]",
			"provider", "alert_id", "state", "manifest_path")

		// Owner: packet.PathHop.
		hop := lane2ItemsOf(t, lane2Child(t, props, "impact_path", "explain.hop"), "explain.hop[]")
		hopProps := lane2PropsOf(t, hop, "explain.hop[]")
		lane2RequireExactKeys(t, hopProps, "explain.hop[]",
			"hop", "status", "evidence_fact_ids", "missing_evidence")
		lane2RequireDescribed(t, hopProps, "explain.hop[]",
			"hop", "status", "evidence_fact_ids", "missing_evidence")

		// Owner: packet.EvidenceFactSummary.
		ev := lane2ItemsOf(t, lane2Child(t, props, "evidence", "explain.ev"), "explain.ev[]")
		evProps := lane2PropsOf(t, ev, "explain.ev[]")
		lane2RequireExactKeys(t, evProps, "explain.ev[]",
			"fact_id", "fact_kind", "source_system", "source_confidence",
			"observed_at")
		lane2RequireDescribed(t, evProps, "explain.ev[]",
			"fact_id", "fact_kind", "source_system", "source_confidence",
			"observed_at")

		// Owner: packet.ExplanationFreshness.
		fresh := lane2Child(t, props, "freshness", "explain.freshness")
		freshProps := lane2PropsOf(t, fresh, "explain.freshness")
		lane2RequireExactKeys(t, freshProps, "explain.freshness",
			"state", "latest_observed_at", "evidence_fact_count")
		lane2RequireScalarTypes(t, freshProps, "explain.freshness", map[string]string{
			"state": "string", "evidence_fact_count": "integer",
		})
		lane2RequireDescribed(t, freshProps, "explain.freshness",
			"state", "latest_observed_at", "evidence_fact_count")

		// Owner: packet.Remediation, enriched with advisory-fact evidence.
		rem := lane2Child(t, props, "remediation", "explain.remediation")
		remProps := lane2PropsOf(t, rem, "explain.remediation")
		lane2RequireExactKeys(t, remProps, "explain.remediation",
			"ecosystem", "current_version", "vulnerable_range",
			"fixed_version_source", "match_reason", "first_patched_version",
			"patched_version_branches", "manifest_range",
			"manifest_allows_fix", "direct", "parent_package", "confidence",
			"reason", "missing_evidence")
		lane2RequireDescribed(t, remProps, "explain.remediation",
			"ecosystem", "current_version", "vulnerable_range",
			"fixed_version_source", "match_reason", "first_patched_version",
			"patched_version_branches", "manifest_range",
			"manifest_allows_fix", "direct", "parent_package", "confidence",
			"reason", "missing_evidence")
		branch := lane2ItemsOf(t, lane2Child(t, remProps, "patched_version_branches", "explain.branch"), "evidence.branch[]")
		branchProps := lane2PropsOf(t, branch, "explain.branch[]")
		lane2RequireExactKeys(t, branchProps, "explain.branch[]", "version", "source")
		lane2RequireDescribed(t, branchProps, "explain.branch[]", "version", "source")

		// The finding object carries the same FindingResult shape the
		// list route pins; the fragment names the explain-specific
		// sub-objects and keeps additionalProperties for the rest.
		finding := lane2Child(t, props, "finding", "explain.finding")
		if add, ok := finding["additionalProperties"]; !ok || add != true {
			t.Errorf("explain.finding: expected additionalProperties true for the FindingResult passthrough")
		}
		if _, ok := finding["properties"]; !ok {
			t.Fatalf("explain.finding: named explain sub-objects missing")
		}
		findingProps := lane2PropsOf(t, finding, "explain.finding")
		lane2RequireDescribed(t, findingProps, "explain.finding",
			"kubernetes_runtime_workload_refs", "kubernetes_runtime_probe",
			"runtime_context")
		kref := lane2ItemsOf(t, lane2Child(t, findingProps, "kubernetes_runtime_workload_refs", "explain.kref"), "explain.kref[]")
		krefProps := lane2PropsOf(t, kref, "explain.kref[]")
		lane2RequireExactKeys(t, krefProps, "explain.kref[]",
			"workload_uid", "cluster_id", "namespace", "name")
		lane2RequireDescribed(t, krefProps, "explain.kref[]",
			"workload_uid", "cluster_id", "namespace", "name")

		lane2PinReadiness(t, doc, "/api/v0/supply-chain/impact/explain", "get", "explain")
		lane2RequireNoBareObjects(t, schema, "explain")
	})
}
