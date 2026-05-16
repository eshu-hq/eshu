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
		strings.TrimSpace(fmt.Sprint(identity["repository_id"])),
		strings.TrimSpace(fmt.Sprint(identity["subject_digest"])),
	}, ":")
}

func supplyChainImpactIdentity(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) map[string]any {
	return map[string]any{
		"scope_id":       strings.TrimSpace(write.ScopeID),
		"generation_id":  strings.TrimSpace(write.GenerationID),
		"cve_id":         strings.TrimSpace(finding.CVEID),
		"package_id":     strings.TrimSpace(finding.PackageID),
		"subject_digest": strings.TrimSpace(finding.SubjectDigest),
		"repository_id":  strings.TrimSpace(finding.RepositoryID),
	}
}

func supplyChainImpactPayload(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) map[string]any {
	return map[string]any{
		"reducer_domain":       string(DomainSupplyChainImpact),
		"intent_id":            write.IntentID,
		"scope_id":             write.ScopeID,
		"generation_id":        write.GenerationID,
		"source_system":        write.SourceSystem,
		"cause":                write.Cause,
		"cve_id":               finding.CVEID,
		"advisory_id":          finding.AdvisoryID,
		"package_id":           finding.PackageID,
		"ecosystem":            finding.Ecosystem,
		"package_name":         finding.PackageName,
		"purl":                 finding.PURL,
		"observed_version":     finding.ObservedVersion,
		"fixed_version":        finding.FixedVersion,
		"impact_status":        string(finding.Status),
		"confidence":           finding.Confidence,
		"cvss_score":           finding.CVSSScore,
		"epss_probability":     finding.EPSSProbability,
		"epss_percentile":      finding.EPSSPercentile,
		"known_exploited":      finding.KnownExploited,
		"priority_reason":      finding.PriorityReason,
		"runtime_reachability": finding.RuntimeReachability,
		"repository_id":        finding.RepositoryID,
		"subject_digest":       finding.SubjectDigest,
		"missing_evidence":     uniqueSortedStrings(finding.MissingEvidence),
		"evidence_path":        orderedUniqueStrings(finding.EvidencePath),
		"evidence_fact_ids":    uniqueSortedStrings(finding.EvidenceFactIDs),
		"canonical_writes":     finding.CanonicalWrites,
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
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
