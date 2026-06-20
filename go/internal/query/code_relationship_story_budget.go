package query

import (
	"encoding/json"
	"fmt"
	"strings"
)

// relationshipStorySupportedType reports whether t is a relationship type the
// bounded relationship-story query path can follow.
func relationshipStorySupportedType(t string) bool {
	switch t {
	case "CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO":
		return true
	default:
		return false
	}
}

// normalizedRelationshipTypes returns the effective, validated, de-duplicated
// set of relationship types to follow. When relationship_types is empty it
// falls back to the single normalizedRelationshipType. Caller order is
// preserved so multi-type merging is deterministic.
func (r relationshipStoryRequest) normalizedRelationshipTypes() ([]string, error) {
	if len(r.RelationshipTypes) == 0 {
		single, err := r.normalizedRelationshipType()
		if err != nil {
			return nil, err
		}
		return []string{single}, nil
	}
	seen := make(map[string]struct{}, len(r.RelationshipTypes))
	out := make([]string, 0, len(r.RelationshipTypes))
	for _, raw := range r.RelationshipTypes {
		relationshipType := strings.ToUpper(strings.TrimSpace(raw))
		if relationshipType == "" {
			continue
		}
		if !relationshipStorySupportedType(relationshipType) {
			return nil, fmt.Errorf("relationship_types entry %q is not supported", strings.TrimSpace(raw))
		}
		if _, ok := seen[relationshipType]; ok {
			continue
		}
		seen[relationshipType] = struct{}{}
		out = append(out, relationshipType)
	}
	if len(out) == 0 {
		single, err := r.normalizedRelationshipType()
		if err != nil {
			return nil, err
		}
		return []string{single}, nil
	}
	return out, nil
}

// normalizedTokenBudget returns the effective token budget, treating negative or
// zero values as "no budget".
func (r relationshipStoryRequest) normalizedTokenBudget() int {
	if r.TokenBudget < 0 {
		return 0
	}
	return r.TokenBudget
}

// relationshipStoryApplyTokenBudget trims rows in place so their estimated
// serialized token cost stays within req.token_budget. It returns nil when no
// budget is set, otherwise an accounting map describing the budget, the
// estimated tokens kept, whether the budget forced a cut, how many rows were
// dropped, and guidance for narrowing. The count limit is applied before this;
// the budget is a second, tighter bound that lets an agent cap prompt cost.
//
// Rows are kept in their incoming order: callers that want the most useful rows
// to survive a small budget must order rows by relevance before calling this.
func relationshipStoryApplyTokenBudget(req relationshipStoryRequest, rows *[]map[string]any) map[string]any {
	budget := req.normalizedTokenBudget()
	if budget <= 0 {
		return nil
	}
	original := *rows
	kept := make([]map[string]any, 0, len(original))
	used := 0
	dropped := 0
	for index, row := range original {
		cost := estimateRowTokens(row)
		if used+cost > budget {
			dropped = len(original) - index
			break
		}
		used += cost
		kept = append(kept, row)
	}
	*rows = kept
	accounting := map[string]any{
		"limit":            budget,
		"estimated_tokens": used,
		"truncated":        dropped > 0,
		"dropped":          dropped,
	}
	if dropped > 0 {
		accounting["guidance"] = relationshipStoryBudgetGuidance(req)
	}
	return accounting
}

// estimateRowTokens returns a deterministic, conservative token estimate for a
// single relationship row, derived from the byte length of its compact JSON
// encoding using the common ~4-bytes-per-token heuristic. It bounds response
// size against a caller token_budget; it is not a billing-grade tokenizer.
func estimateRowTokens(row map[string]any) int {
	encoded, err := json.Marshal(row)
	if err != nil {
		return 0
	}
	return (len(encoded) + 3) / 4
}

// relationshipStoryBudgetGuidance returns a deterministic instruction teaching
// the agent how to narrow a relationship query that exceeded its token_budget.
func relationshipStoryBudgetGuidance(req relationshipStoryRequest) string {
	parts := []string{"request a single relationship_type"}
	if direction, _ := req.normalizedDirection(); direction == "both" {
		parts = append(parts, "set direction to incoming or outgoing")
	}
	parts = append(parts, "lower limit", "scope with repo_id", "then drill into source_handle/target_handle")
	return "relationships were trimmed to fit token_budget; " + strings.Join(parts, ", ")
}
