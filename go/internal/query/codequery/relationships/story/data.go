// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package story

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Response assembly and the small pure shapers behind it live here,
// split out of codequery/relationship_story.go (#6060). The HTTP
// handlers stay in codequery and call Data.

// NormalizeMaxDepth bounds a caller-supplied traversal depth to the
// story's supported range.
func NormalizeMaxDepth(maxDepth int) int {
	switch {
	case maxDepth <= 0:
		return 5
	case maxDepth > 10:
		return 10
	default:
		return maxDepth
	}
}

// EffectiveMaxDepth reports the traversal depth a request resolves to:
// class-hierarchy and transitive stories honor the caller's bound,
// anything else reads one hop.
func EffectiveMaxDepth(req codemodel.RelationshipStoryRequest) int {
	if req.NormalizedQueryType() == "class_hierarchy" {
		return NormalizeMaxDepth(req.MaxDepth)
	}
	if !req.IncludeTransitive {
		return 1
	}
	return NormalizeMaxDepth(req.MaxDepth)
}

// Data assembles the story payload: confidence floor, per-direction
// counts, bounded-centrality ranking, handle attachment, token budget,
// and the coverage/scope envelopes.
func Data(
	req codemodel.RelationshipStoryRequest,
	resolution codemodel.RelationshipStoryResolution,
	rows []map[string]any,
) map[string]any {
	limit := req.NormalizedLimit()
	rawCount := len(rows)
	// The confidence floor is applied before count truncation, so a floor that
	// empties the set leaves nothing to truncate: afterFloorCount == 0 implies
	// the later countTruncated is false. The evidence classifier relies on this
	// ordering — the floor-filtered and count-truncated reasons never collide.
	rows = codemodel.RelationshipStoryRowsAboveConfidenceFloor(rows, req)
	floorApplied := req.MinConfidence != nil && *req.MinConfidence > 0
	afterFloorCount := len(rows)
	availableByDirection := DirectionCounts(rows)
	truncatedByDirection := DirectionTruncation(availableByDirection, req, limit)
	// Rank by bounded centrality before the count limit so the most-connected
	// neighbors survive a small limit or token_budget.
	rows = codeshaping.RelationshipStoryRankByCentrality(rows)
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	rows = RowsWithHandles(rows)
	availableBeforeBudget := len(rows)
	budget := codeshaping.RelationshipStoryApplyTokenBudget(req, &rows)
	returnedByDirection := DirectionCounts(rows)
	direction, _ := req.NormalizedDirection()
	relationshipTypes, _ := req.NormalizedRelationshipTypes()
	queryShape := "entity_anchor_one_hop"
	if req.IncludeTransitive {
		queryShape = "entity_anchor_bounded_bfs"
	}
	summary := map[string]any{
		"relationship_count":    len(rows),
		"returned_by_direction": returnedByDirection,
		"truncated":             truncated,
	}
	coverage := map[string]any{
		"query_shape":            queryShape,
		"scope_mode":             ScopeMode(req),
		"directions":             Directions(direction),
		"relationship_types":     relationshipTypes,
		"max_depth":              EffectiveMaxDepth(req),
		"available_by_direction": availableByDirection,
		"returned_by_direction":  returnedByDirection,
		"truncated_by_direction": truncatedByDirection,
		"truncated":              truncated,
		"ranked_by":              codeshaping.RelationshipStoryRankBasis,
	}
	budgetTruncated := budget != nil && availableBeforeBudget > len(rows)
	if budget != nil {
		budget["available_before_budget"] = availableBeforeBudget
		summary["token_budget"] = budget
		coverage["token_budget"] = budget
	}
	evidence := codemodel.ClassifyRelationshipStoryEvidence(codemodel.RelationshipStoryEvidenceInputs{
		ResolutionStatus: resolution.Status,
		RawCount:         rawCount,
		AfterFloorCount:  afterFloorCount,
		FloorApplied:     floorApplied,
		CountTruncated:   truncated,
		BudgetTruncated:  budgetTruncated,
		// The graph/content fetch caps at normalizedLimit()+1, so rawCount > limit
		// means the edge set was paged and not exhausted.
		RawPaged: rawCount > limit,
	})
	coverage["missing_edge_reason"] = evidence.Reason
	coverage["truncation_state"] = evidence.Truncation
	coverage["evidence_explanation"] = evidence.Explanation
	if req.MinConfidence != nil {
		coverage["min_confidence"] = *req.MinConfidence
	}
	scope := map[string]any{
		"repo_id":            strings.TrimSpace(req.RepoID),
		"language":           strings.TrimSpace(req.Language),
		"direction":          direction,
		"relationship_type":  relationshipTypes[0],
		"relationship_types": relationshipTypes,
		"cross_repo":         req.CrossRepo,
		"limit":              limit,
		"offset":             req.Offset,
		"max_depth":          EffectiveMaxDepth(req),
		"include_transitive": req.IncludeTransitive,
	}
	if req.MinConfidence != nil {
		scope["min_confidence"] = *req.MinConfidence
	}
	return map[string]any{
		"target_resolution": resolution,
		"scope":             scope,
		"relationships":     rows,
		"summary":           summary,
		"coverage":          coverage,
	}
}

// ScopeMode names the repository scope a request reads under.
func ScopeMode(req codemodel.RelationshipStoryRequest) string {
	if req.CrossRepo {
		return "cross_repo"
	}
	return "repo_scoped"
}

// MarkClassHierarchyCoverage retargets an assembled payload's coverage
// envelope at the class-hierarchy story.
func MarkClassHierarchyCoverage(data map[string]any, req codemodel.RelationshipStoryRequest) {
	maxDepth := NormalizeMaxDepth(req.MaxDepth)
	if scope, ok := data["scope"].(map[string]any); ok {
		scope["max_depth"] = maxDepth
	}
	coverage, ok := data["coverage"].(map[string]any)
	if !ok {
		return
	}
	coverage["query_shape"] = "entity_anchor_class_hierarchy_story"
	coverage["relationship_types"] = []string{"INHERITS", "CONTAINS"}
	coverage["max_depth"] = maxDepth
}

// MarkRepoOverrideCoverage retargets an assembled payload's coverage
// envelope at the repo-anchored override story.
func MarkRepoOverrideCoverage(data map[string]any) {
	if coverage, ok := data["coverage"].(map[string]any); ok {
		coverage["query_shape"] = "repo_anchor_override_story"
		coverage["relationship_types"] = []string{"OVERRIDES"}
	}
}

// DirectionCounts tallies rows by their direction column.
func DirectionCounts(rows []map[string]any) map[string]int {
	counts := map[string]int{"incoming": 0, "outgoing": 0}
	for _, row := range rows {
		direction := querycontract.StringVal(row, "direction")
		if direction == "incoming" || direction == "outgoing" {
			counts[direction]++
		}
	}
	return counts
}

// DirectionTruncation reports, per direction, whether that direction's
// available rows exceed the page the caller asked for. A transitive read
// pages only its own direction; a direct read pages both.
func DirectionTruncation(counts map[string]int, req codemodel.RelationshipStoryRequest, limit int) map[string]bool {
	direction, _ := req.NormalizedDirection()
	truncated := map[string]bool{"incoming": false, "outgoing": false}
	if req.IncludeTransitive {
		truncated[direction] = counts[direction] > limit
		return truncated
	}
	for _, current := range Directions(direction) {
		truncated[current] = counts[current] > limit
	}
	return truncated
}

// RowsWithHandles clones rows and attaches entity handles plus the
// confidence-basis and provenance readers expect, omitting optional
// provenance keys a legacy edge never recorded rather than surfacing
// them as a null tier.
func RowsWithHandles(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := querycontract.CloneAnyMap(row)
		querycontract.AddRelationshipConfidenceBasis(item)
		item["provenance"] = codemodel.RelationshipStoryProvenance(item)
		if sourceID := querycontract.StringVal(item, "source_id"); sourceID != "" {
			item["source_handle"] = "entity:" + sourceID
		}
		if targetID := querycontract.StringVal(item, "target_id"); targetID != "" {
			item["target_handle"] = "entity:" + targetID
		}
		// Per ADR #2222 a legacy edge without recorded provenance omits the
		// fields rather than surfacing a null tier; readers treat absence as
		// unspecified.
		dropNilOrEmptyRowKey(item, "confidence")
		dropNilOrEmptyRowKey(item, "resolution_method")
		out = append(out, item)
	}
	return out
}

// dropNilOrEmptyRowKey removes a row key whose value is nil or an empty or
// whitespace-only string, so optional per-edge provenance fields are omitted
// rather than surfaced as a null tier.
func dropNilOrEmptyRowKey(row map[string]any, key string) {
	value, ok := row[key]
	if !ok {
		return
	}
	if value == nil {
		delete(row, key)
		return
	}
	if text, isString := value.(string); isString && strings.TrimSpace(text) == "" {
		delete(row, key)
	}
}

// Directions expands a requested direction into the legs a direct read
// pages.
func Directions(direction string) []string {
	if direction == "both" {
		return []string{"incoming", "outgoing"}
	}
	return []string{direction}
}
