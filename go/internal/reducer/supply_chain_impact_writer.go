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
		supplyChainImpactIdentity(write, finding),
	)
}

func supplyChainImpactStableFactKey(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) string {
	identity := supplyChainImpactIdentity(write, finding)
	return strings.Join([]string{
		"supply_chain_impact",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["cve_id"])),
		strings.TrimSpace(fmt.Sprint(identity["package_id"])),
		strings.TrimSpace(fmt.Sprint(identity["product_criteria"])),
		strings.TrimSpace(fmt.Sprint(identity["match_criteria_id"])),
		strings.TrimSpace(fmt.Sprint(identity["repository_id"])),
		strings.TrimSpace(fmt.Sprint(identity["subject_digest"])),
	}, ":")
}

func supplyChainImpactIdentity(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) map[string]any {
	return map[string]any{
		"scope_id":          strings.TrimSpace(write.ScopeID),
		"generation_id":     strings.TrimSpace(write.GenerationID),
		"cve_id":            strings.TrimSpace(finding.CVEID),
		"package_id":        strings.TrimSpace(finding.PackageID),
		"product_criteria":  strings.TrimSpace(finding.ProductCriteria),
		"match_criteria_id": strings.TrimSpace(finding.MatchCriteriaID),
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
		"service_ids":            uniqueSortedStrings(finding.ServiceIDs),
		"environments":           uniqueSortedStrings(finding.Environments),
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
	return payload
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
