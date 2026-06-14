package query

import "strings"

const (
	relationshipStoryProvenanceCodeEdge        = "code_edge"
	relationshipStoryProvenanceCorrelationEdge = "correlation_edge"
	relationshipStoryProvenanceUnsupported     = "unsupported"

	relationshipStoryTruthDerived     = "derived"
	relationshipStoryTruthHeuristic   = "heuristic"
	relationshipStoryTruthUnsupported = "unsupported"
)

func relationshipStoryProvenance(row map[string]any) map[string]any {
	confidence, hasConfidence := relationshipStoryNumericConfidence(row)
	sourceFamily := relationshipStoryProvenanceSourceFamily(row)
	truthState := relationshipStoryProvenanceTruthState(row, sourceFamily, hasConfidence)
	provenance := map[string]any{
		"confidence_state": relationshipStoryConfidenceState(hasConfidence),
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
