package query

// Evidence-state classification for relationship-story answers (issue #3158).
//
// These fields make code-relationship uncertainty explicit instead of leaving a
// caller to guess why a result is empty or short: a resolved target with no
// edges reads differently from a target that did not resolve, a confidence
// floor that removed every row, or a result capped by limit or token budget.
// The classification is descriptive only — it never changes the answer's
// TruthEnvelope and never upgrades a heuristic or unsupported edge into
// canonical truth.

const (
	relationshipStoryReasonComplete         = "complete"
	relationshipStoryReasonTargetUnresolved = "target_unresolved"
	relationshipStoryReasonNoEdges          = "no_relationships_found"
	relationshipStoryReasonFloorFiltered    = "all_below_confidence_floor"
	relationshipStoryReasonTruncatedLimit   = "truncated_by_limit"
	relationshipStoryReasonTruncatedBudget  = "truncated_by_token_budget"

	relationshipStoryTruncationNone   = "none"
	relationshipStoryTruncationCount  = "count"
	relationshipStoryTruncationBudget = "token_budget"
	relationshipStoryTruncationBoth   = "count_and_token_budget"
)

// relationshipStoryEvidenceInputs carries the counts and flags needed to explain
// why a relationship-story result is complete, empty, filtered, or truncated.
type relationshipStoryEvidenceInputs struct {
	resolutionStatus string
	rawCount         int
	afterFloorCount  int
	floorApplied     bool
	countTruncated   bool
	budgetTruncated  bool
	// rawPaged is true when the underlying fetch returned a bounded page that did
	// not exhaust the edge set (the graph reader caps at normalizedLimit()+1).
	// When the page was floor-emptied this prevents claiming an exhaustive
	// all_below_confidence_floor result over a partial page.
	rawPaged bool
}

// relationshipStoryTargetResolved reports whether a resolution status means the
// target was found well enough to read relationships. The repo-scoped override
// story sets "repo_scoped" and returns real rows, so it is resolved-equivalent;
// only genuinely unresolved statuses (ambiguous, not_found, content-fallback)
// are treated as unresolved.
func relationshipStoryTargetResolved(status string) bool {
	switch status {
	case "", "resolved", "repo_scoped":
		return true
	default:
		return false
	}
}

// relationshipStoryEvidenceState is the classified missing-edge reason,
// truncation state, and a bounded human explanation.
type relationshipStoryEvidenceState struct {
	reason      string
	truncation  string
	explanation string
}

// classifyRelationshipStoryEvidence classifies the result. Reason priority is
// fixed: an unresolved target and an empty graph are reported before truncation,
// because truncation of an already-empty result would be misleading.
//
// A floor that empties the page is reported as all_below_confidence_floor only
// when the page was the complete edge set (!rawPaged). When the fetch was paged,
// a later page may hold a qualifying edge, so the honest reason is truncation,
// not an exhaustive floor verdict.
func classifyRelationshipStoryEvidence(in relationshipStoryEvidenceInputs) relationshipStoryEvidenceState {
	// A paged raw fetch means more edges existed than were returned, which is a
	// count truncation even when the post-floor row count fits under the limit.
	countTruncated := in.countTruncated || in.rawPaged
	truncation := relationshipStoryTruncationState(countTruncated, in.budgetTruncated)
	switch {
	case !relationshipStoryTargetResolved(in.resolutionStatus) && in.rawCount == 0:
		return relationshipStoryEvidenceState{
			reason:      relationshipStoryReasonTargetUnresolved,
			truncation:  truncation,
			explanation: "the target did not resolve to a known entity, so no relationships could be read",
		}
	case in.rawCount == 0:
		return relationshipStoryEvidenceState{
			reason:      relationshipStoryReasonNoEdges,
			truncation:  truncation,
			explanation: "the target resolved but has no relationships of the requested type and direction",
		}
	case in.floorApplied && in.afterFloorCount == 0 && !in.rawPaged:
		return relationshipStoryEvidenceState{
			reason:      relationshipStoryReasonFloorFiltered,
			truncation:  truncation,
			explanation: "relationships exist but all fell below the requested min_confidence floor",
		}
	case countTruncated:
		return relationshipStoryEvidenceState{
			reason:      relationshipStoryReasonTruncatedLimit,
			truncation:  truncation,
			explanation: "more relationships exist than the limit; raise limit or page with offset to evaluate them, including against any min_confidence floor",
		}
	case in.budgetTruncated:
		return relationshipStoryEvidenceState{
			reason:      relationshipStoryReasonTruncatedBudget,
			truncation:  truncation,
			explanation: "results were trimmed to fit token_budget; raise token_budget or narrow the query",
		}
	default:
		return relationshipStoryEvidenceState{
			reason:      relationshipStoryReasonComplete,
			truncation:  truncation,
			explanation: "all matching relationships were returned",
		}
	}
}

// relationshipStoryRowsAboveConfidenceFloor drops rows below an optional
// min_confidence floor. It filters the response only; it never changes the
// answer's canonical truth (ADR #2222).
func relationshipStoryRowsAboveConfidenceFloor(rows []map[string]any, req relationshipStoryRequest) []map[string]any {
	if req.MinConfidence == nil || *req.MinConfidence <= 0 {
		return rows
	}
	floor := *req.MinConfidence
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		confidence, ok := relationshipStoryNumericConfidence(row)
		if ok && confidence >= floor {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// relationshipStoryNumericConfidence reads a row's numeric confidence, returning
// false when the edge carries none (a legacy or unsupported edge), so callers
// never coerce a missing confidence into 0.
func relationshipStoryNumericConfidence(row map[string]any) (float64, bool) {
	value, ok := row["confidence"]
	if !ok || value == nil {
		return 0, false
	}
	switch confidence := value.(type) {
	case float64:
		return confidence, true
	case float32:
		return float64(confidence), true
	case int:
		return float64(confidence), true
	case int64:
		return float64(confidence), true
	default:
		return 0, false
	}
}

func relationshipStoryTruncationState(countTruncated, budgetTruncated bool) string {
	switch {
	case countTruncated && budgetTruncated:
		return relationshipStoryTruncationBoth
	case countTruncated:
		return relationshipStoryTruncationCount
	case budgetTruncated:
		return relationshipStoryTruncationBudget
	default:
		return relationshipStoryTruncationNone
	}
}
