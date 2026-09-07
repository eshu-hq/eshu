// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeshaping

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// relationshipStorySupportedType, normalizedRelationshipTypes, and
// normalizedTokenBudget moved to
// codemodel/code_relationship_story_evidence_state.go with the request type
// (#6060 lane A L1); the budget applier below calls them through the leaf.
// This file followed the request to codeshaping in L3; the staying story
// handler calls the exported applier through root's
// family_code_shim_shaping.go forwarder.

// RelationshipStoryApplyTokenBudget trims rows in place so their estimated
// serialized token cost stays within req.token_budget. It returns nil when no
// budget is set, otherwise an accounting map describing the budget, the
// estimated tokens kept, whether the budget forced a cut, how many rows were
// dropped, and guidance for narrowing. The count limit is applied before this;
// the budget is a second, tighter bound that lets an agent cap prompt cost.
//
// Rows are kept in their incoming order: callers that want the most useful rows
// to survive a small budget must order rows by relevance before calling this.
func RelationshipStoryApplyTokenBudget(req codemodel.RelationshipStoryRequest, rows *[]map[string]any) map[string]any {
	budget := req.NormalizedTokenBudget()
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
// It stays leaf-private: only the budget applier above calls it.
func relationshipStoryBudgetGuidance(req codemodel.RelationshipStoryRequest) string {
	parts := []string{"request a single relationship_type"}
	if direction, _ := req.NormalizedDirection(); direction == "both" {
		parts = append(parts, "set direction to incoming or outgoing")
	}
	parts = append(parts, "lower limit", "scope with repo_id", "then drill into source_handle/target_handle")
	return "relationships were trimmed to fit token_budget; " + strings.Join(parts, ", ")
}
