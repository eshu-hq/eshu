// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DecodeSupplyChainImpactFindingRow decodes one reducer_supply_chain_impact_finding
// fact payload (supply_chain_impact_findings_queries.go's
// SupplyChainImpactFindingFactKind) into the query-side row shape.
//
// The reducer writer now emits a governed factschema payload for #4810/W1h.
// This query-side decoder remains the W2 consumer seam: it preserves the
// existing row projection until the Postgres selection and explanation paths can
// hydrate through sdk/go/factschema without changing filter/index behavior.
func DecodeSupplyChainImpactFindingRow(
	factID string,
	sourceConfidence string,
	payloadBytes []byte,
) (SupplyChainImpactFindingRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return SupplyChainImpactFindingRow{}, fmt.Errorf("decode supply chain impact finding: %w", err)
	}
	row := SupplyChainImpactFindingRow{
		FindingID:           factID,
		CVEID:               querycontract.StringVal(payload, "cve_id"),
		AdvisoryID:          querycontract.StringVal(payload, "advisory_id"),
		PackageID:           querycontract.StringVal(payload, "package_id"),
		Ecosystem:           querycontract.StringVal(payload, "ecosystem"),
		PackageName:         querycontract.StringVal(payload, "package_name"),
		PURL:                querycontract.StringVal(payload, "purl"),
		ProductCriteria:     querycontract.StringVal(payload, "product_criteria"),
		MatchCriteriaID:     querycontract.StringVal(payload, "match_criteria_id"),
		ObservedVersion:     querycontract.StringVal(payload, "observed_version"),
		RequestedRange:      querycontract.StringVal(payload, "requested_range"),
		FixedVersion:        querycontract.StringVal(payload, "fixed_version"),
		VulnerableRange:     querycontract.StringVal(payload, "vulnerable_range"),
		MatchReason:         querycontract.StringVal(payload, "match_reason"),
		ImpactStatus:        querycontract.StringVal(payload, "impact_status"),
		Confidence:          querycontract.StringVal(payload, "confidence"),
		CVSSScore:           querycontract.FloatVal(payload, "cvss_score"),
		AdvisoryPublishedAt: querycontract.StringVal(payload, "advisory_published_at"),
		AdvisoryUpdatedAt:   querycontract.StringVal(payload, "advisory_updated_at"),
		EPSSProbability:     querycontract.StringVal(payload, "epss_probability"),
		EPSSPercentile:      querycontract.StringVal(payload, "epss_percentile"),
		KnownExploited:      querycontract.BoolVal(payload, "known_exploited"),
		PriorityReason:      querycontract.StringVal(payload, "priority_reason"),
		PriorityScore:       int(querycontract.FloatVal(payload, "priority_score")),
		PriorityBucket:      querycontract.StringVal(payload, "priority_bucket"),
		PriorityReasonCodes: querycontract.StringSliceVal(payload, "priority_reason_codes"),
		PriorityContributions: decodeSupplyChainImpactPriorityContributions(
			payload["priority_contributions"],
		),
		RuntimeReachability:      querycontract.StringVal(payload, "runtime_reachability"),
		Reachability:             decodeSupplyChainReachability(payload),
		RepositoryID:             querycontract.StringVal(payload, "repository_id"),
		SubjectDigest:            querycontract.StringVal(payload, "subject_digest"),
		ImageRef:                 querycontract.StringVal(payload, "image_ref"),
		DependencyScope:          querycontract.StringVal(payload, "dependency_scope"),
		WorkloadIDs:              querycontract.StringSliceVal(payload, "workload_ids"),
		DeploymentIDs:            querycontract.StringSliceVal(payload, "deployment_ids"),
		ServiceIDs:               querycontract.StringSliceVal(payload, "service_ids"),
		Environments:             querycontract.StringSliceVal(payload, "environments"),
		EnvironmentEvidence:      stringMapVal(payload, "environment_evidence"),
		CIDeclaredArtifactDigest: querycontract.StringVal(payload, "ci_declared_artifact_digest"),
		CIDeclaredImageRef:       querycontract.StringVal(payload, "ci_declared_image_ref"),
		CatalogEntityRefs:        querycontract.StringSliceVal(payload, "catalog_entity_refs"),
		CatalogOwnerRefs:         querycontract.StringSliceVal(payload, "catalog_owner_refs"),
		DependencyPath:           querycontract.StringSliceVal(payload, "dependency_path"),
		DependencyDepth:          int(querycontract.FloatVal(payload, "dependency_depth")),
		DirectDependency:         boolPointerVal(payload, "direct_dependency"),
		MissingEvidence:          querycontract.StringSliceVal(payload, "missing_evidence"),
		EvidencePath:             querycontract.StringSliceVal(payload, "evidence_path"),
		EvidenceFactIDs:          querycontract.StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:          "active",
		SourceConfidence:         sourceConfidence,
		Provenance:               decodeSupplyChainImpactProvenance(payload),
		DetectionProfile:         querycontract.StringVal(payload, "detection_profile"),
		Suppression:              decodeSupplyChainSuppressionDecision(payload),
		Remediation:              DecodeSupplyChainImpactRemediation(payload),
	}
	if row.DetectionProfile == "" {
		row.DetectionProfile = inferLegacyDetectionProfile(row.ImpactStatus, row.ObservedVersion, row.MatchReason)
	}
	return row, nil
}

func decodeSupplyChainReachability(payload map[string]any) *SupplyChainReachabilityResult {
	raw, ok := payload["reachability"].(map[string]any)
	if !ok {
		return nil
	}
	state := querycontract.StringVal(raw, "state")
	if state == "" {
		return nil
	}
	return &SupplyChainReachabilityResult{
		State:            state,
		Confidence:       querycontract.StringVal(raw, "confidence"),
		Source:           querycontract.StringVal(raw, "source"),
		Evidence:         querycontract.StringVal(raw, "evidence"),
		Reason:           querycontract.StringVal(raw, "reason"),
		LanguageMaturity: querycontract.StringVal(raw, "language_maturity"),
		MissingEvidence:  querycontract.StringSliceVal(raw, "missing_evidence"),
	}
}

// inferLegacyDetectionProfile classifies pre-profile findings written before
// the reducer tagged detection_profile using the same rule the reducer applies
// live. Range-only, derived, product-only, malformed, and missing-version rows
// still land in comprehensive.
func inferLegacyDetectionProfile(impactStatus string, observedVersion string, matchReason string) string {
	switch impactStatus {
	case "affected_exact", "not_affected_known_fixed":
	default:
		return SupplyChainImpactProfileComprehensive
	}
	if strings.TrimSpace(observedVersion) == "" {
		return SupplyChainImpactProfileComprehensive
	}
	switch matchReason {
	case "npm_semver_affected_range",
		"npm_semver_known_fixed",
		"nuget_semver_affected_range",
		"nuget_semver_known_fixed",
		"cargo_semver_affected_range",
		"cargo_semver_known_fixed",
		"hex_semver_affected_range",
		"hex_semver_known_fixed",
		"maven_range_match",
		"maven_known_fixed",
		"swift_semver_affected_range",
		"swift_semver_known_fixed":
		return SupplyChainImpactProfilePrecise
	default:
		return SupplyChainImpactProfileComprehensive
	}
}

func decodeSupplyChainSuppressionDecision(payload map[string]any) *SupplyChainSuppressionDecisionRow {
	raw, ok := payload["suppression"].(map[string]any)
	if !ok || len(raw) == 0 {
		state := querycontract.StringVal(payload, "suppression_state")
		if state == "" {
			return nil
		}
		return &SupplyChainSuppressionDecisionRow{State: state}
	}
	row := SupplyChainSuppressionDecisionRow{
		State:          querycontract.StringVal(raw, "state"),
		SuppressionID:  querycontract.StringVal(raw, "suppression_id"),
		Source:         querycontract.StringVal(raw, "source"),
		Justification:  querycontract.StringVal(raw, "justification"),
		Author:         querycontract.StringVal(raw, "author"),
		AuthoredAt:     querycontract.StringVal(raw, "authored_at"),
		ExpiresAt:      querycontract.StringVal(raw, "expires_at"),
		Reason:         querycontract.StringVal(raw, "reason"),
		EvidenceRef:    querycontract.StringVal(raw, "evidence_ref"),
		VEXDocumentID:  querycontract.StringVal(raw, "vex_document_id"),
		VEXStatementID: querycontract.StringVal(raw, "vex_statement_id"),
	}
	if row.State == "" {
		row.State = querycontract.StringVal(payload, "suppression_state")
	}
	if row.State == "" {
		return nil
	}
	return &row
}

func decodeSupplyChainImpactProvenance(payload map[string]any) *SupplyChainImpactProvenance {
	raw, ok := payload["provenance"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	provenance := SupplyChainImpactProvenance{
		SelectedSeveritySource:     querycontract.StringVal(raw, "selected_severity_source"),
		SelectedSeverityScore:      querycontract.FloatVal(raw, "selected_severity_score"),
		SelectedSeverityVector:     querycontract.StringVal(raw, "selected_severity_vector"),
		SelectedSeverityLabel:      querycontract.StringVal(raw, "selected_severity_label"),
		SelectedFixedVersionSource: querycontract.StringVal(raw, "selected_fixed_version_source"),
		SelectedRangeSource:        querycontract.StringVal(raw, "selected_range_source"),
	}
	provenance.AlternateSeverities = decodeAlternateSeverities(raw["alternate_severities"])
	provenance.FixedVersionBranches = decodeFixedVersionBranches(raw["fixed_version_branches"])
	provenance.AdvisorySources = decodeAdvisorySources(raw["advisory_sources"])
	if provenance.isEmpty() {
		return nil
	}
	return &provenance
}

func (p SupplyChainImpactProvenance) isEmpty() bool {
	return p.SelectedSeveritySource == "" &&
		p.SelectedSeverityScore == 0 &&
		p.SelectedSeverityVector == "" &&
		p.SelectedSeverityLabel == "" &&
		p.SelectedFixedVersionSource == "" &&
		p.SelectedRangeSource == "" &&
		len(p.AlternateSeverities) == 0 &&
		len(p.FixedVersionBranches) == 0 &&
		len(p.AdvisorySources) == 0
}

func decodeAlternateSeverities(raw any) []SupplyChainAlternateSeverity {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SupplyChainAlternateSeverity, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SupplyChainAlternateSeverity{
			Source: querycontract.StringVal(row, "source"),
			Score:  querycontract.FloatVal(row, "score"),
			Vector: querycontract.StringVal(row, "vector"),
			Label:  querycontract.StringVal(row, "label"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeFixedVersionBranches(raw any) []SupplyChainFixedVersionBranch {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SupplyChainFixedVersionBranch, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SupplyChainFixedVersionBranch{
			Version: querycontract.StringVal(row, "version"),
			Source:  querycontract.StringVal(row, "source"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeAdvisorySources(raw any) []SupplyChainAdvisorySource {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]SupplyChainAdvisorySource, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SupplyChainAdvisorySource{
			Source:          querycontract.StringVal(row, "source"),
			AdvisoryID:      querycontract.StringVal(row, "advisory_id"),
			SourceUpdatedAt: querycontract.StringVal(row, "source_updated_at"),
			WithdrawnAt:     querycontract.StringVal(row, "withdrawn_at"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
