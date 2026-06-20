package query

import "strings"

const (
	relationshipStoryProvenanceCodeEdge        = "code_edge"
	relationshipStoryProvenanceCorrelationEdge = "correlation_edge"
	relationshipStoryProvenanceUnsupported     = "unsupported"

	relationshipStoryTruthDerived     = "derived"
	relationshipStoryTruthHeuristic   = "heuristic"
	relationshipStoryTruthUnsupported = "unsupported"

	relationshipStoryTierHigh        = "high"
	relationshipStoryTierMedium      = "medium"
	relationshipStoryTierLow         = "low"
	relationshipStoryTierUnsupported = "unsupported"
)

func relationshipStoryProvenance(row map[string]any) map[string]any {
	confidence, hasConfidence := relationshipStoryNumericConfidence(row)
	sourceFamily := relationshipStoryProvenanceSourceFamily(row)
	truthState := relationshipStoryProvenanceTruthState(row, sourceFamily, hasConfidence)
	provenance := map[string]any{
		"confidence_state": relationshipStoryConfidenceState(hasConfidence),
		"confidence_tier":  relationshipStoryConfidenceTier(confidence, hasConfidence),
		"method":           relationshipStoryProvenanceMethod(row),
		"source_family":    sourceFamily,
		"reason":           relationshipStoryProvenanceReason(row),
		"truth_state":      truthState,
		"derived":          truthState == relationshipStoryTruthDerived,
		"heuristic":        truthState == relationshipStoryTruthHeuristic,
		"unsupported":      truthState == relationshipStoryTruthUnsupported,
	}
	if hasConfidence {
		provenance["confidence"] = confidence
	}
	return provenance
}

// relationshipStoryConfidenceTier maps a numeric confidence to a named tier so
// agents can weight an edge without hard-coding the ADR #2222 tier numbers. It
// is a presentation derivation of confidence; it never changes truth_state and
// never upgrades a heuristic or unsupported edge into canonical truth. A row
// with no recorded confidence is "unsupported", not silently promoted.
func relationshipStoryConfidenceTier(confidence float64, hasConfidence bool) string {
	switch {
	case !hasConfidence:
		return relationshipStoryTierUnsupported
	case confidence >= 0.90:
		return relationshipStoryTierHigh
	case confidence >= 0.70:
		return relationshipStoryTierMedium
	default:
		return relationshipStoryTierLow
	}
}

func relationshipStoryConfidenceState(hasConfidence bool) string {
	if hasConfidence {
		return "reported"
	}
	return relationshipStoryProvenanceUnsupported
}

func relationshipStoryProvenanceMethod(row map[string]any) string {
	for _, key := range []string{"resolution_method", "confidence_basis", "resolution_source", "evidence_type", "call_kind"} {
		if value := strings.TrimSpace(StringVal(row, key)); value != "" {
			return value
		}
	}
	return relationshipStoryProvenanceUnsupported
}

func relationshipStoryProvenanceReason(row map[string]any) string {
	if reason := strings.TrimSpace(StringVal(row, "reason")); reason != "" {
		return reason
	}
	return "not_reported"
}

func relationshipStoryProvenanceSourceFamily(row map[string]any) string {
	if strings.TrimSpace(StringVal(row, "resolution_method")) != "" {
		return relationshipStoryProvenanceCodeEdge
	}
	if strings.TrimSpace(StringVal(row, "confidence_basis")) != "" ||
		strings.TrimSpace(StringVal(row, "resolution_source")) != "" ||
		strings.TrimSpace(StringVal(row, "evidence_type")) != "" ||
		len(StringSliceVal(row, "evidence_kinds")) > 0 ||
		IntVal(row, "evidence_count") > 0 {
		return relationshipStoryProvenanceCorrelationEdge
	}
	return relationshipStoryProvenanceUnsupported
}

func relationshipStoryProvenanceTruthState(row map[string]any, sourceFamily string, hasConfidence bool) string {
	if sourceFamily == relationshipStoryProvenanceUnsupported || !hasConfidence {
		return relationshipStoryTruthUnsupported
	}
	if sourceFamily == relationshipStoryProvenanceCorrelationEdge {
		return relationshipStoryTruthHeuristic
	}
	method := strings.ToLower(strings.TrimSpace(relationshipStoryProvenanceMethod(row)))
	if strings.Contains(method, "semantic") || strings.Contains(method, "heuristic") {
		return relationshipStoryTruthHeuristic
	}
	return relationshipStoryTruthDerived
}
