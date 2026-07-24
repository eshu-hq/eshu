// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"strings"
)

// decodeSupplyChainImpactFindingRow decodes one reducer_supply_chain_impact_finding
// fact payload (supply_chain_impact_findings_queries.go's
// supplyChainImpactFindingFactKind) into the query-side row shape.
//
// The reducer writer now emits a governed factschema payload for #4810/W1h.
// This query-side decoder remains the W2 consumer seam: it preserves the
// existing row projection until the Postgres selection and explanation paths can
// hydrate through sdk/go/factschema without changing filter/index behavior.
func decodeSupplyChainImpactFindingRow(
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
		CVEID:               StringVal(payload, "cve_id"),
		AdvisoryID:          StringVal(payload, "advisory_id"),
		PackageID:           StringVal(payload, "package_id"),
		Ecosystem:           StringVal(payload, "ecosystem"),
		PackageName:         StringVal(payload, "package_name"),
		PURL:                StringVal(payload, "purl"),
		ProductCriteria:     StringVal(payload, "product_criteria"),
		MatchCriteriaID:     StringVal(payload, "match_criteria_id"),
		ObservedVersion:     StringVal(payload, "observed_version"),
		RequestedRange:      StringVal(payload, "requested_range"),
		FixedVersion:        StringVal(payload, "fixed_version"),
		VulnerableRange:     StringVal(payload, "vulnerable_range"),
		MatchReason:         StringVal(payload, "match_reason"),
		ImpactStatus:        StringVal(payload, "impact_status"),
		Confidence:          StringVal(payload, "confidence"),
		CVSSScore:           floatVal(payload, "cvss_score"),
		AdvisoryPublishedAt: StringVal(payload, "advisory_published_at"),
		AdvisoryUpdatedAt:   StringVal(payload, "advisory_updated_at"),
		EPSSProbability:     StringVal(payload, "epss_probability"),
		EPSSPercentile:      StringVal(payload, "epss_percentile"),
		KnownExploited:      BoolVal(payload, "known_exploited"),
		PriorityReason:      StringVal(payload, "priority_reason"),
		PriorityScore:       int(floatVal(payload, "priority_score")),
		PriorityBucket:      StringVal(payload, "priority_bucket"),
		PriorityReasonCodes: StringSliceVal(payload, "priority_reason_codes"),
		PriorityContributions: decodeSupplyChainImpactPriorityContributions(
			payload["priority_contributions"],
		),
		RuntimeReachability: StringVal(payload, "runtime_reachability"),
		Reachability:        decodeSupplyChainReachability(payload),
		RepositoryID:        StringVal(payload, "repository_id"),
		SubjectDigest:       StringVal(payload, "subject_digest"),
		ImageRef:            StringVal(payload, "image_ref"),
		DependencyScope:     StringVal(payload, "dependency_scope"),
		WorkloadIDs:         StringSliceVal(payload, "workload_ids"),
		DeploymentIDs:       StringSliceVal(payload, "deployment_ids"),
		ServiceIDs:          StringSliceVal(payload, "service_ids"),
		Environments:        StringSliceVal(payload, "environments"),
		CatalogEntityRefs:   StringSliceVal(payload, "catalog_entity_refs"),
		CatalogOwnerRefs:    StringSliceVal(payload, "catalog_owner_refs"),
		DependencyPath:      StringSliceVal(payload, "dependency_path"),
		DependencyDepth:     int(floatVal(payload, "dependency_depth")),
		DirectDependency:    boolPointerVal(payload, "direct_dependency"),
		MissingEvidence:     StringSliceVal(payload, "missing_evidence"),
		EvidencePath:        StringSliceVal(payload, "evidence_path"),
		EvidenceFactIDs:     StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:     "active",
		SourceConfidence:    sourceConfidence,
		Provenance:          decodeSupplyChainImpactProvenance(payload),
		DetectionProfile:    StringVal(payload, "detection_profile"),
		Suppression:         decodeSupplyChainSuppressionDecision(payload),
		Remediation:         decodeSupplyChainImpactRemediation(payload),
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
	state := StringVal(raw, "state")
	if state == "" {
		return nil
	}
	return &SupplyChainReachabilityResult{
		State:            state,
		Confidence:       StringVal(raw, "confidence"),
		Source:           StringVal(raw, "source"),
		Evidence:         StringVal(raw, "evidence"),
		Reason:           StringVal(raw, "reason"),
		LanguageMaturity: StringVal(raw, "language_maturity"),
		MissingEvidence:  StringSliceVal(raw, "missing_evidence"),
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
		state := StringVal(payload, "suppression_state")
		if state == "" {
			return nil
		}
		return &SupplyChainSuppressionDecisionRow{State: state}
	}
	row := SupplyChainSuppressionDecisionRow{
		State:          StringVal(raw, "state"),
		SuppressionID:  StringVal(raw, "suppression_id"),
		Source:         StringVal(raw, "source"),
		Justification:  StringVal(raw, "justification"),
		Author:         StringVal(raw, "author"),
		AuthoredAt:     StringVal(raw, "authored_at"),
		ExpiresAt:      StringVal(raw, "expires_at"),
		Reason:         StringVal(raw, "reason"),
		EvidenceRef:    StringVal(raw, "evidence_ref"),
		VEXDocumentID:  StringVal(raw, "vex_document_id"),
		VEXStatementID: StringVal(raw, "vex_statement_id"),
	}
	if row.State == "" {
		row.State = StringVal(payload, "suppression_state")
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
		SelectedSeveritySource:     StringVal(raw, "selected_severity_source"),
		SelectedSeverityScore:      floatVal(raw, "selected_severity_score"),
		SelectedSeverityVector:     StringVal(raw, "selected_severity_vector"),
		SelectedSeverityLabel:      StringVal(raw, "selected_severity_label"),
		SelectedFixedVersionSource: StringVal(raw, "selected_fixed_version_source"),
		SelectedRangeSource:        StringVal(raw, "selected_range_source"),
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
			Source: StringVal(row, "source"),
			Score:  floatVal(row, "score"),
			Vector: StringVal(row, "vector"),
			Label:  StringVal(row, "label"),
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
			Version: StringVal(row, "version"),
			Source:  StringVal(row, "source"),
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
			Source:          StringVal(row, "source"),
			AdvisoryID:      StringVal(row, "advisory_id"),
			SourceUpdatedAt: StringVal(row, "source_updated_at"),
			WithdrawnAt:     StringVal(row, "withdrawn_at"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
