// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"errors"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	relationshipStoryCapability = "call_graph.relationship_story"
)

// The codemodel.RelationshipStoryRequest/codemodel.RelationshipStoryResolution types, the page
// limit bounds, and the request's methods split to
// codemodel/code_relationship_story_evidence_state.go (#6060 lane A L1);
// the evidence-state classifier takes the request there. Root's
// family_code_shim.go aliases the types back so the staying handlers,
// resolvers, and tests keep their names.

func (h *CodeHandler) handleRelationshipStory(w http.ResponseWriter, r *http.Request) {
	var req codemodel.RelationshipStoryRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), relationshipStoryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code relationship story requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			relationshipStoryCapability,
			h.profile(),
			querycontract.RequiredProfile(relationshipStoryCapability),
		)
		return
	}
	if err := req.Validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, relationshipStoryCapability) {
		return
	}

	if req.IsRepoScopedOverrideStory() {
		h.handleRepoScopedOverrideStory(w, r, req)
		return
	}

	resolution, entity, err := h.resolveRelationshipStoryTarget(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resolution.Status != "resolved" {
		h.writeRelationshipStory(w, r, req, resolution, nil, TruthBasisContentIndex)
		return
	}
	if req.NormalizedQueryType() == "class_hierarchy" && entity != nil &&
		strings.TrimSpace(entity.EntityType) != "" &&
		!relationshipStoryClassHierarchyEntityType(entity.EntityType) {
		WriteError(w, http.StatusBadRequest, "class_hierarchy target must resolve to a class or inheritable entity")
		return
	}

	relationships, sourceBackend, basis, err := h.relationshipStoryRelationships(r.Context(), req, entity)
	if err != nil {
		if errors.Is(err, errSymbolBackendUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if WriteGraphReadError(w, r, err, relationshipStoryCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := relationshipStoryData(req, resolution, relationships)
	data["source_backend"] = sourceBackend
	if req.NormalizedQueryType() == "class_hierarchy" {
		hierarchy, err := h.relationshipStoryClassHierarchy(r.Context(), req, entity, relationships)
		if err != nil {
			if WriteGraphReadError(w, r, err, relationshipStoryCapability) {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["class_hierarchy"] = hierarchy
		markRelationshipStoryClassHierarchyCoverage(data, req)
	}
	if req.NormalizedQueryType() == "overrides" {
		data["override_story"] = relationshipStoryOverrideData(req, relationships)
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), relationshipStoryCapability, basis, "resolved from bounded relationship story lookup"),
	)
}

// The request validation and accessors moved to
// codemodel/code_relationship_story_evidence_state.go with the request type
// (#6060 lane A L1).

func normalizedRelationshipStoryMaxDepth(maxDepth int) int {
	switch {
	case maxDepth <= 0:
		return 5
	case maxDepth > 10:
		return 10
	default:
		return maxDepth
	}
}

func relationshipStoryEffectiveMaxDepth(req codemodel.RelationshipStoryRequest) int {
	if req.NormalizedQueryType() == "class_hierarchy" {
		return normalizedRelationshipStoryMaxDepth(req.MaxDepth)
	}
	if !req.IncludeTransitive {
		return 1
	}
	return normalizedRelationshipStoryMaxDepth(req.MaxDepth)
}

func (h *CodeHandler) writeRelationshipStory(
	w http.ResponseWriter,
	r *http.Request,
	req codemodel.RelationshipStoryRequest,
	resolution codemodel.RelationshipStoryResolution,
	relationships []map[string]any,
	basis TruthBasis,
) {
	data := relationshipStoryData(req, resolution, relationships)
	if basis == TruthBasisContentIndex {
		data["source_backend"] = "postgres_content_store"
		if h == nil || h.Content == nil {
			data["source_backend"] = "unavailable"
		}
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), relationshipStoryCapability, basis, "resolved from bounded relationship story lookup"),
	)
}

func relationshipStoryData(
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
	availableByDirection := relationshipStoryDirectionCounts(rows)
	truncatedByDirection := relationshipStoryDirectionTruncation(availableByDirection, req, limit)
	// Rank by bounded centrality before the count limit so the most-connected
	// neighbors survive a small limit or token_budget.
	rows = codeshaping.RelationshipStoryRankByCentrality(rows)
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	rows = relationshipStoryRowsWithHandles(rows)
	availableBeforeBudget := len(rows)
	budget := codeshaping.RelationshipStoryApplyTokenBudget(req, &rows)
	returnedByDirection := relationshipStoryDirectionCounts(rows)
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
		"scope_mode":             relationshipStoryScopeMode(req),
		"directions":             relationshipStoryDirections(direction),
		"relationship_types":     relationshipTypes,
		"max_depth":              relationshipStoryEffectiveMaxDepth(req),
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
		"max_depth":          relationshipStoryEffectiveMaxDepth(req),
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

func relationshipStoryScopeMode(req codemodel.RelationshipStoryRequest) string {
	if req.CrossRepo {
		return "cross_repo"
	}
	return "repo_scoped"
}

func markRelationshipStoryClassHierarchyCoverage(data map[string]any, req codemodel.RelationshipStoryRequest) {
	maxDepth := normalizedRelationshipStoryMaxDepth(req.MaxDepth)
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

func relationshipStoryDirectionCounts(rows []map[string]any) map[string]int {
	counts := map[string]int{"incoming": 0, "outgoing": 0}
	for _, row := range rows {
		direction := StringVal(row, "direction")
		if direction == "incoming" || direction == "outgoing" {
			counts[direction]++
		}
	}
	return counts
}

func relationshipStoryDirectionTruncation(counts map[string]int, req codemodel.RelationshipStoryRequest, limit int) map[string]bool {
	direction, _ := req.NormalizedDirection()
	truncated := map[string]bool{"incoming": false, "outgoing": false}
	if req.IncludeTransitive {
		truncated[direction] = counts[direction] > limit
		return truncated
	}
	for _, current := range relationshipStoryDirections(direction) {
		truncated[current] = counts[current] > limit
	}
	return truncated
}

func relationshipStoryRowsWithHandles(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := cloneQueryAnyMap(row)
		querycontract.AddRelationshipConfidenceBasis(item)
		item["provenance"] = codemodel.RelationshipStoryProvenance(item)
		if sourceID := StringVal(item, "source_id"); sourceID != "" {
			item["source_handle"] = "entity:" + sourceID
		}
		if targetID := StringVal(item, "target_id"); targetID != "" {
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

func relationshipStoryDirections(direction string) []string {
	if direction == "both" {
		return []string{"incoming", "outgoing"}
	}
	return []string{direction}
}
