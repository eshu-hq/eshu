// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	relationshipStoryCapability   = "call_graph.relationship_story"
	relationshipStoryDefaultLimit = 25
	relationshipStoryMaxLimit     = 200
	relationshipStoryMaxOffset    = 10000
)

type relationshipStoryRequest struct {
	QueryType         string `json:"query_type"`
	Target            string `json:"target"`
	Name              string `json:"name"`
	EntityID          string `json:"entity_id"`
	RepoID            string `json:"repo_id"`
	Language          string `json:"language"`
	CrossRepo         bool   `json:"cross_repo"`
	Direction         string `json:"direction"`
	RelationshipType  string `json:"relationship_type"`
	IncludeTransitive bool   `json:"include_transitive"`
	MaxDepth          int    `json:"max_depth"`
	Limit             int    `json:"limit"`
	Offset            int    `json:"offset"`
	// MinConfidence is an optional response-only floor. A nil floor preserves
	// legacy and low-confidence rows; a positive floor keeps only rows with a
	// numeric confidence at or above the threshold.
	MinConfidence *float64 `json:"min_confidence"`
	// RelationshipTypes is an optional additive multi-type filter. When set it
	// supersedes RelationshipType: each requested type is followed with the same
	// bounded single-type query and the results are merged. It applies only to
	// direct (non-transitive) relationship lookups.
	RelationshipTypes []string `json:"relationship_types"`
	// TokenBudget optionally caps the response by an estimated serialized token
	// cost. Zero or absent means no budget. It is a second, tighter bound applied
	// after the count limit so an agent can cap prompt cost; cuts are reported
	// with guidance to narrow.
	TokenBudget int `json:"token_budget"`
	// graphAnchorProperty records the single identity property selected
	// for this resolved NornicDB target. It is request-internal and reused by
	// every requested relationship type and direction.
	graphAnchorProperty string
	// graphAnchorPropertyResolved distinguishes a confirmed missing graph anchor
	// from an unresolved/ambiguous uid-id collision that must retain the legacy
	// per-query fallback behavior.
	graphAnchorPropertyResolved bool
}

type relationshipStoryResolution struct {
	Status     string           `json:"status"`
	Target     string           `json:"target,omitempty"`
	EntityID   string           `json:"entity_id,omitempty"`
	Name       string           `json:"name,omitempty"`
	RepoID     string           `json:"repo_id,omitempty"`
	Language   string           `json:"language,omitempty"`
	Candidates []map[string]any `json:"candidates,omitempty"`
	Truncated  bool             `json:"truncated,omitempty"`
}

func (h *CodeHandler) handleRelationshipStory(w http.ResponseWriter, r *http.Request) {
	var req relationshipStoryRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), relationshipStoryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code relationship story requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			relationshipStoryCapability,
			h.profile(),
			requiredProfile(relationshipStoryCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, relationshipStoryCapability) {
		return
	}

	if req.isRepoScopedOverrideStory() {
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
	if req.normalizedQueryType() == "class_hierarchy" && entity != nil &&
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
	if req.normalizedQueryType() == "class_hierarchy" {
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
	if req.normalizedQueryType() == "overrides" {
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

func (r relationshipStoryRequest) validate() error {
	if strings.TrimSpace(r.EntityID) == "" && strings.TrimSpace(r.target()) == "" && !r.isRepoScopedOverrideStory() {
		return errors.New("entity_id or target is required")
	}
	if r.CrossRepo && strings.TrimSpace(r.RepoID) == "" {
		return errors.New("cross_repo relationship story requires repo_id")
	}
	if r.CrossRepo && r.normalizedQueryType() == "class_hierarchy" {
		return errors.New("cross_repo class_hierarchy enrichment is not supported; use relationship_type INHERITS")
	}
	if r.CrossRepo && r.normalizedQueryType() == "overrides" {
		return errors.New("cross_repo overrides enrichment is not supported; use relationship_type OVERRIDES")
	}
	if r.Offset < 0 {
		return errors.New("offset must be >= 0")
	}
	if r.Offset > relationshipStoryMaxOffset {
		return errors.New("offset must be <= 10000")
	}
	if _, err := r.normalizedDirection(); err != nil {
		return err
	}
	if _, err := r.normalizedRelationshipType(); err != nil {
		return err
	}
	if r.TokenBudget < 0 {
		return errors.New("token_budget must be >= 0")
	}
	if r.MinConfidence != nil && (*r.MinConfidence < 0 || *r.MinConfidence > 1) {
		return errors.New("min_confidence must be between 0 and 1")
	}
	if _, err := r.normalizedRelationshipTypes(); err != nil {
		return err
	}
	if len(r.RelationshipTypes) > 0 {
		if r.IncludeTransitive {
			return errors.New("relationship_types cannot be combined with include_transitive")
		}
		switch r.normalizedQueryType() {
		case "class_hierarchy", "overrides":
			return errors.New("relationship_types cannot be combined with class_hierarchy or overrides query types")
		}
	}
	if r.IncludeTransitive {
		if r.Offset != 0 {
			return errors.New("include_transitive requires offset 0")
		}
		if relationshipType, _ := r.normalizedRelationshipType(); relationshipType != "CALLS" {
			return errors.New("include_transitive currently supports CALLS relationships only")
		}
		if direction, _ := r.normalizedDirection(); direction == "both" {
			return errors.New("set direction to incoming or outgoing when include_transitive is true")
		}
	}
	return nil
}

func (r relationshipStoryRequest) normalizedQueryType() string {
	return strings.ToLower(strings.TrimSpace(r.QueryType))
}

func (r relationshipStoryRequest) isRepoScopedOverrideStory() bool {
	return r.normalizedQueryType() == "overrides" &&
		strings.TrimSpace(r.EntityID) == "" &&
		strings.TrimSpace(r.target()) == ""
}

func (r relationshipStoryRequest) target() string {
	if target := strings.TrimSpace(r.Target); target != "" {
		return target
	}
	return strings.TrimSpace(r.Name)
}

func (r relationshipStoryRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return relationshipStoryDefaultLimit
	case r.Limit > relationshipStoryMaxLimit:
		return relationshipStoryMaxLimit
	default:
		return r.Limit
	}
}

func (r relationshipStoryRequest) normalizedDirection() (string, error) {
	switch direction := strings.ToLower(strings.TrimSpace(r.Direction)); direction {
	case "":
		return "both", nil
	case "incoming", "outgoing", "both":
		return direction, nil
	default:
		return "", errors.New("direction must be incoming, outgoing, or both")
	}
}

func (r relationshipStoryRequest) normalizedRelationshipType() (string, error) {
	relationshipType := strings.ToUpper(strings.TrimSpace(r.RelationshipType))
	if relationshipType == "" {
		switch r.normalizedQueryType() {
		case "class_hierarchy":
			return "INHERITS", nil
		case "overrides":
			return "OVERRIDES", nil
		}
		return "CALLS", nil
	}
	if !relationshipStorySupportedType(relationshipType) {
		return "", fmt.Errorf("relationship_type %q is not supported", strings.TrimSpace(r.RelationshipType))
	}
	return relationshipType, nil
}

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

func relationshipStoryEffectiveMaxDepth(req relationshipStoryRequest) int {
	if req.normalizedQueryType() == "class_hierarchy" {
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
	req relationshipStoryRequest,
	resolution relationshipStoryResolution,
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
	req relationshipStoryRequest,
	resolution relationshipStoryResolution,
	rows []map[string]any,
) map[string]any {
	limit := req.normalizedLimit()
	rawCount := len(rows)
	// The confidence floor is applied before count truncation, so a floor that
	// empties the set leaves nothing to truncate: afterFloorCount == 0 implies
	// the later countTruncated is false. The evidence classifier relies on this
	// ordering — the floor-filtered and count-truncated reasons never collide.
	rows = relationshipStoryRowsAboveConfidenceFloor(rows, req)
	floorApplied := req.MinConfidence != nil && *req.MinConfidence > 0
	afterFloorCount := len(rows)
	availableByDirection := relationshipStoryDirectionCounts(rows)
	truncatedByDirection := relationshipStoryDirectionTruncation(availableByDirection, req, limit)
	// Rank by bounded centrality before the count limit so the most-connected
	// neighbors survive a small limit or token_budget.
	rows = relationshipStoryRankByCentrality(rows)
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	rows = relationshipStoryRowsWithHandles(rows)
	availableBeforeBudget := len(rows)
	budget := relationshipStoryApplyTokenBudget(req, &rows)
	returnedByDirection := relationshipStoryDirectionCounts(rows)
	direction, _ := req.normalizedDirection()
	relationshipTypes, _ := req.normalizedRelationshipTypes()
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
		"ranked_by":              relationshipStoryRankBasis,
	}
	budgetTruncated := budget != nil && availableBeforeBudget > len(rows)
	if budget != nil {
		budget["available_before_budget"] = availableBeforeBudget
		summary["token_budget"] = budget
		coverage["token_budget"] = budget
	}
	evidence := classifyRelationshipStoryEvidence(relationshipStoryEvidenceInputs{
		resolutionStatus: resolution.Status,
		rawCount:         rawCount,
		afterFloorCount:  afterFloorCount,
		floorApplied:     floorApplied,
		countTruncated:   truncated,
		budgetTruncated:  budgetTruncated,
		// The graph/content fetch caps at normalizedLimit()+1, so rawCount > limit
		// means the edge set was paged and not exhausted.
		rawPaged: rawCount > limit,
	})
	coverage["missing_edge_reason"] = evidence.reason
	coverage["truncation_state"] = evidence.truncation
	coverage["evidence_explanation"] = evidence.explanation
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

func relationshipStoryScopeMode(req relationshipStoryRequest) string {
	if req.CrossRepo {
		return "cross_repo"
	}
	return "repo_scoped"
}

func markRelationshipStoryClassHierarchyCoverage(data map[string]any, req relationshipStoryRequest) {
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

func relationshipStoryDirectionTruncation(counts map[string]int, req relationshipStoryRequest, limit int) map[string]bool {
	direction, _ := req.normalizedDirection()
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
		addRelationshipConfidenceBasis(item)
		item["provenance"] = relationshipStoryProvenance(item)
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
