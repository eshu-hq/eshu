package doctruth

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func findingPayload(finding VerificationFinding) map[string]any {
	return map[string]any{
		"finding_id":          finding.FindingID,
		"finding_version":     finding.FindingVersion,
		"finding_type":        finding.FindingType,
		"status":              finding.Status,
		"truth_level":         finding.TruthLevel,
		"freshness_state":     finding.FreshnessState,
		"source_id":           finding.SourceID,
		"document_id":         finding.DocumentID,
		"section_id":          finding.SectionID,
		"claim_id":            finding.ClaimID,
		"claim_type":          finding.ClaimType,
		"claim_text":          finding.ClaimText,
		"normalized_claim":    finding.NormalizedClaim,
		"summary":             finding.Summary,
		"evidence_packet_id":  finding.EvidencePacketID,
		"evidence_packet_url": "/api/v0/documentation/findings/" + finding.FindingID + "/evidence-packet",
		"permissions":         visibilityPayload(),
		"states": map[string]any{
			"finding_state":       finding.Status,
			"freshness_state":     finding.FreshnessState,
			"permission_decision": "allowed",
		},
	}
}

func visibilityPayload() map[string]any {
	return map[string]any{
		"viewer_can_read_source":    true,
		"source_acl_evaluated":      true,
		"packet_redacted":           false,
		"write_permission_decision": "external_updater_must_check",
	}
}

func (v *Verifier) envelope(kind, stableKey string, payload map[string]any) facts.Envelope {
	observedAt := v.now().UTC()
	return facts.Envelope{
		FactID: facts.StableID("DocumentationVerificationFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    stableKey,
			"scope_id":      v.scopeID,
			"generation_id": v.generationID,
		}),
		ScopeID:          v.scopeID,
		GenerationID:     v.generationID,
		FactKind:         kind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.DocumentationFactSchemaVersion,
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceDerived,
		ObservedAt:       observedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem: v.sourceSystem,
			ScopeID:      v.scopeID,
			GenerationID: v.generationID,
			FactKey:      stableKey,
		},
	}
}

func (s *VerificationSummary) add(status string) {
	s.ClaimsChecked++
	switch status {
	case VerificationStatusValid:
		s.Valid++
	case VerificationStatusContradicted:
		s.Contradicted++
	case VerificationStatusMissingEvidence:
		s.MissingEvidence++
	case VerificationStatusUnsupportedClaimType:
		s.UnsupportedClaimType++
	}
}

func commandKey(path []string) string {
	parts := make([]string, 0, len(path))
	for _, part := range path {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}

func endpointKey(method, path string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimRight(strings.TrimSpace(path), ".,);")
	if method == "" || path == "" {
		return ""
	}
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	return method + " " + path
}

func normalizeEshuCommand(text string) string {
	text = strings.TrimPrefix(strings.TrimSpace(text), "$ ")
	fields := strings.Fields(text)
	if len(fields) == 0 || fields[0] != "eshu" {
		return ""
	}
	parts := []string{}
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") || field == "." || strings.HasPrefix(field, "/") {
			break
		}
		parts = append(parts, strings.ToLower(strings.Trim(field, ".,);")))
	}
	return commandKey(parts)
}

func normalizeSnippet(raw string) string {
	return strings.TrimSpace(strings.Trim(raw, "`"))
}

func verificationSummaryText(claim extractedClaim, status string) string {
	return claim.claimType + " claim " + status + ": " + claim.normalized
}
