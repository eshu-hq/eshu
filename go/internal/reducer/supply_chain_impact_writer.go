// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const supplyChainImpactFactKind = "reducer_supply_chain_impact_finding"

// PostgresSupplyChainImpactWriter stores reducer-owned vulnerability impact
// findings in the shared fact store.
type PostgresSupplyChainImpactWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteSupplyChainImpactFindings persists every impact status so callers can
// see affected, fixed, possible, and unknown evidence without a hidden priority
// policy.
func (w PostgresSupplyChainImpactWriter) WriteSupplyChainImpactFindings(
	ctx context.Context,
	write SupplyChainImpactWrite,
) (SupplyChainImpactWriteResult, error) {
	if w.DB == nil {
		return SupplyChainImpactWriteResult{}, fmt.Errorf("supply chain impact database is required")
	}
	now := reducerWriterNow(w.Now)
	for _, finding := range write.Findings {
		payloadJSON, err := json.Marshal(supplyChainImpactPayload(write, finding))
		if err != nil {
			return SupplyChainImpactWriteResult{}, fmt.Errorf("marshal supply chain impact payload: %w", err)
		}
		if _, err := w.DB.ExecContext(
			ctx,
			canonicalReducerFactInsertQuery,
			supplyChainImpactFactID(write, finding),
			write.ScopeID,
			write.GenerationID,
			supplyChainImpactFactKind,
			supplyChainImpactStableFactKey(write, finding),
			reducerFactCollectorKind(write.SourceSystem),
			facts.SourceConfidenceInferred,
			write.SourceSystem,
			write.IntentID,
			nil,
			nil,
			now,
			now,
			false,
			payloadJSON,
		); err != nil {
			return SupplyChainImpactWriteResult{}, fmt.Errorf("write supply chain impact fact: %w", err)
		}
	}
	canonicalWrites := supplyChainImpactCanonicalWrites(write.Findings)
	return SupplyChainImpactWriteResult{
		CanonicalWrites: canonicalWrites,
		FactsWritten:    len(write.Findings),
		EvidenceSummary: fmt.Sprintf("wrote supply chain impact findings=%d canonical_writes=%d", len(write.Findings), canonicalWrites),
	}, nil
}

func supplyChainImpactFactID(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) string {
	return supplyChainImpactFactKind + ":" + facts.StableID(
		supplyChainImpactFactKind,
		supplyChainImpactFactRowIdentity(write, finding),
	)
}

func supplyChainImpactStableFactKey(_ SupplyChainImpactWrite, finding SupplyChainImpactFinding) string {
	identity := supplyChainImpactLogicalIdentity(finding)
	return strings.Join([]string{
		"supply_chain_impact",
		strings.TrimSpace(fmt.Sprint(identity["cve_id"])),
		strings.TrimSpace(fmt.Sprint(identity["advisory_id"])),
		strings.TrimSpace(fmt.Sprint(identity["package_id"])),
		strings.TrimSpace(fmt.Sprint(identity["purl"])),
		strings.TrimSpace(fmt.Sprint(identity["product_criteria"])),
		strings.TrimSpace(fmt.Sprint(identity["match_criteria_id"])),
		strings.TrimSpace(fmt.Sprint(identity["observed_version"])),
		strings.TrimSpace(fmt.Sprint(identity["requested_range"])),
		strings.TrimSpace(fmt.Sprint(identity["impact_status"])),
		strings.TrimSpace(fmt.Sprint(identity["repository_id"])),
		strings.TrimSpace(fmt.Sprint(identity["subject_digest"])),
	}, ":")
}

func supplyChainImpactFactRowIdentity(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) map[string]any {
	identity := supplyChainImpactLogicalIdentity(finding)
	identity["scope_id"] = strings.TrimSpace(write.ScopeID)
	identity["generation_id"] = strings.TrimSpace(write.GenerationID)
	return identity
}

func supplyChainImpactFindingID(finding SupplyChainImpactFinding) string {
	return supplyChainImpactFactKind + ":" + facts.StableID(
		supplyChainImpactFactKind,
		supplyChainImpactLogicalIdentity(finding),
	)
}

func supplyChainImpactLogicalIdentity(finding SupplyChainImpactFinding) map[string]any {
	return map[string]any{
		"cve_id":            strings.TrimSpace(finding.CVEID),
		"advisory_id":       strings.TrimSpace(finding.AdvisoryID),
		"package_id":        strings.TrimSpace(finding.PackageID),
		"purl":              strings.TrimSpace(finding.PURL),
		"product_criteria":  strings.TrimSpace(finding.ProductCriteria),
		"match_criteria_id": strings.TrimSpace(finding.MatchCriteriaID),
		"observed_version":  strings.TrimSpace(finding.ObservedVersion),
		"requested_range":   strings.TrimSpace(finding.RequestedRange),
		"impact_status":     strings.TrimSpace(string(finding.Status)),
		"subject_digest":    strings.TrimSpace(finding.SubjectDigest),
		"repository_id":     strings.TrimSpace(finding.RepositoryID),
	}
}

func supplyChainImpactPayload(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) map[string]any {
	payload := map[string]any{
		"reducer_domain":         string(DomainSupplyChainImpact),
		"intent_id":              write.IntentID,
		"scope_id":               write.ScopeID,
		"generation_id":          write.GenerationID,
		"source_system":          write.SourceSystem,
		"cause":                  write.Cause,
		"finding_id":             supplyChainImpactFindingID(finding),
		"cve_id":                 finding.CVEID,
		"advisory_id":            finding.AdvisoryID,
		"package_id":             finding.PackageID,
		"ecosystem":              finding.Ecosystem,
		"package_name":           finding.PackageName,
		"purl":                   finding.PURL,
		"product_criteria":       finding.ProductCriteria,
		"match_criteria_id":      finding.MatchCriteriaID,
		"observed_version":       finding.ObservedVersion,
		"requested_range":        finding.RequestedRange,
		"fixed_version":          finding.FixedVersion,
		"vulnerable_range":       finding.VulnerableRange,
		"match_reason":           finding.MatchReason,
		"impact_status":          string(finding.Status),
		"confidence":             finding.Confidence,
		"cvss_score":             finding.CVSSScore,
		"advisory_published_at":  finding.AdvisoryPublishedAt,
		"advisory_updated_at":    finding.AdvisoryUpdatedAt,
		"epss_probability":       finding.EPSSProbability,
		"epss_percentile":        finding.EPSSPercentile,
		"known_exploited":        finding.KnownExploited,
		"priority_reason":        finding.PriorityReason,
		"priority_score":         finding.PriorityScore,
		"priority_bucket":        finding.PriorityBucket,
		"priority_reason_codes":  uniqueSortedStrings(finding.PriorityReasonCodes),
		"priority_contributions": serializePriorityContributions(finding.PriorityContributions),
		"runtime_reachability":   finding.RuntimeReachability,
		"repository_id":          finding.RepositoryID,
		"subject_digest":         finding.SubjectDigest,
		"image_ref":              finding.ImageRef,
		"dependency_scope":       finding.DependencyScope,
		"workload_ids":           uniqueSortedStrings(finding.WorkloadIDs),
		"deployment_ids":         uniqueSortedStrings(finding.DeploymentIDs),
		"service_ids":            uniqueSortedStrings(finding.ServiceIDs),
		"environments":           uniqueSortedStrings(finding.Environments),
		"catalog_entity_refs":    uniqueSortedStrings(finding.CatalogEntityRefs),
		"catalog_owner_refs":     uniqueSortedStrings(finding.CatalogOwnerRefs),
		"detection_profile":      string(finding.DetectionProfile),
		"missing_evidence":       uniqueSortedStrings(finding.MissingEvidence),
		"evidence_path":          orderedUniqueStrings(finding.EvidencePath),
		"evidence_fact_ids":      uniqueSortedStrings(finding.EvidenceFactIDs),
		"canonical_writes":       finding.CanonicalWrites,
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
	if len(finding.DependencyPath) > 0 {
		payload["dependency_path"] = orderedStrings(finding.DependencyPath)
		payload["dependency_depth"] = finding.DependencyDepth
	}
	if finding.DirectDependency != nil {
		payload["direct_dependency"] = *finding.DirectDependency
	}
	if reachability := supplyChainReachabilityPayload(finding.Reachability); reachability != nil {
		payload["reachability"] = reachability
	}
	provenance := supplyChainImpactProvenancePayload(finding)
	if len(provenance) > 0 {
		payload["provenance"] = provenance
	}
	if suppression := supplyChainImpactSuppressionPayload(finding.Suppression); suppression != nil {
		payload["suppression"] = suppression
		// Persist the state as a top-level payload key as well so the
		// Postgres read model can filter on it without parsing nested JSON.
		payload["suppression_state"] = string(supplyChainImpactSuppressionState(finding.Suppression))
	}
	if remediation := supplyChainImpactRemediationPayload(finding.Remediation); remediation != nil {
		payload["remediation"] = remediation
	}
	return payload
}

// supplyChainImpactRemediationPayload serializes the advisory-only safe
// upgrade explanation onto the canonical finding payload. Reason and
// confidence are always present so the Postgres read model can filter on
// them without parsing nested JSON.
func supplyChainImpactRemediationPayload(r SupplyChainImpactRemediation) map[string]any {
	if r.Reason == "" && r.Confidence == "" && r.FirstPatchedVersion == "" &&
		r.ManifestRange == "" && r.CurrentVersion == "" && r.VulnerableRange == "" &&
		len(r.PatchedVersionBranches) == 0 && len(r.MissingEvidence) == 0 &&
		r.ParentPackage == "" && r.Ecosystem == "" && r.Direct == nil {
		return nil
	}
	out := map[string]any{
		"reason":              r.Reason,
		"confidence":          r.Confidence,
		"manifest_allows_fix": r.ManifestAllowsFix,
	}
	if r.Ecosystem != "" {
		out["ecosystem"] = r.Ecosystem
	}
	if r.CurrentVersion != "" {
		out["current_version"] = r.CurrentVersion
	}
	if r.VulnerableRange != "" {
		out["vulnerable_range"] = r.VulnerableRange
	}
	if r.FixedVersionSource != "" {
		out["fixed_version_source"] = r.FixedVersionSource
	}
	if r.MatchReason != "" {
		out["match_reason"] = r.MatchReason
	}
	if r.FirstPatchedVersion != "" {
		out["first_patched_version"] = r.FirstPatchedVersion
	}
	if r.ManifestRange != "" {
		out["manifest_range"] = r.ManifestRange
	}
	if r.Direct != nil {
		out["direct"] = *r.Direct
	}
	if r.ParentPackage != "" {
		out["parent_package"] = r.ParentPackage
	}
	if branches := serializeFixedVersionBranches(r.PatchedVersionBranches); len(branches) > 0 {
		out["patched_version_branches"] = branches
	}
	if len(r.MissingEvidence) > 0 {
		out["missing_evidence"] = uniqueSortedStrings(r.MissingEvidence)
	}
	return out
}

func serializePriorityContributions(values []SupplyChainImpactPriorityContribution) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if value.ReasonCode == "" {
			continue
		}
		row := map[string]any{
			"reason_code":  value.ReasonCode,
			"input":        value.Input,
			"contribution": value.Contribution,
		}
		if value.Value != "" {
			row["value"] = value.Value
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func supplyChainImpactSuppressionState(decision SupplyChainSuppressionDecision) SupplyChainSuppressionState {
	if decision.State == "" {
		return SupplyChainSuppressionStateActive
	}
	return decision.State
}

func supplyChainImpactSuppressionPayload(decision SupplyChainSuppressionDecision) map[string]any {
	state := supplyChainImpactSuppressionState(decision)
	out := map[string]any{
		"state": string(state),
	}
	if decision.SuppressionID != "" {
		out["suppression_id"] = decision.SuppressionID
	}
	if decision.Source != "" {
		out["source"] = decision.Source
	}
	if decision.Justification != "" {
		out["justification"] = decision.Justification
	}
	if decision.Author != "" {
		out["author"] = decision.Author
	}
	if !decision.AuthoredAt.IsZero() {
		out["authored_at"] = decision.AuthoredAt.UTC().Format(time.RFC3339)
	}
	if !decision.ExpiresAt.IsZero() {
		out["expires_at"] = decision.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if decision.Reason != "" {
		out["reason"] = decision.Reason
	}
	if decision.EvidenceRef != "" {
		out["evidence_ref"] = decision.EvidenceRef
	}
	if decision.VEXDocumentID != "" {
		out["vex_document_id"] = decision.VEXDocumentID
	}
	if decision.VEXStatementID != "" {
		out["vex_statement_id"] = decision.VEXStatementID
	}
	// Always return the payload so the reducer publishes a deterministic
	// "active" decision when no suppression matched; this lets the API
	// filter by suppression_state without nullable-payload guards.
	return out
}

func supplyChainImpactProvenancePayload(finding SupplyChainImpactFinding) map[string]any {
	out := map[string]any{}
	if finding.SeveritySource != "" {
		out["selected_severity_source"] = finding.SeveritySource
	}
	if finding.SeverityVector != "" {
		out["selected_severity_vector"] = finding.SeverityVector
	}
	if finding.SeverityLabel != "" {
		out["selected_severity_label"] = finding.SeverityLabel
	}
	if finding.CVSSScore != 0 {
		out["selected_severity_score"] = finding.CVSSScore
	}
	if finding.FixedVersionSource != "" {
		out["selected_fixed_version_source"] = finding.FixedVersionSource
	}
	if finding.RangeSource != "" {
		out["selected_range_source"] = finding.RangeSource
	}
	if alternates := serializeAlternateSeverities(finding.AlternateSeverities); len(alternates) > 0 {
		out["alternate_severities"] = alternates
	}
	if branches := serializeFixedVersionBranches(finding.FixedVersionBranches); len(branches) > 0 {
		out["fixed_version_branches"] = branches
	}
	if advisories := serializeAdvisorySources(finding.AdvisorySources); len(advisories) > 0 {
		out["advisory_sources"] = advisories
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func serializeAlternateSeverities(values []AlternateSeverity) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row := map[string]any{"source": value.Source}
		if value.Score != 0 {
			row["score"] = value.Score
		}
		if value.Vector != "" {
			row["vector"] = value.Vector
		}
		if value.Label != "" {
			row["label"] = value.Label
		}
		out = append(out, row)
	}
	return out
}

func serializeFixedVersionBranches(values []FixedVersionBranch) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]any{
			"version": value.Version,
			"source":  value.Source,
		})
	}
	return out
}

func serializeAdvisorySources(values []AdvisorySourceObservation) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row := map[string]any{"source": value.Source}
		if value.AdvisoryID != "" {
			row["advisory_id"] = value.AdvisoryID
		}
		if value.SourceUpdatedAt != "" {
			row["source_updated_at"] = value.SourceUpdatedAt
		}
		if value.WithdrawnAt != "" {
			row["withdrawn_at"] = value.WithdrawnAt
		}
		out = append(out, row)
	}
	return out
}

func orderedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func orderedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
