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
	Target            string `json:"target"`
	Name              string `json:"name"`
	EntityID          string `json:"entity_id"`
	RepoID            string `json:"repo_id"`
	Language          string `json:"language"`
	Direction         string `json:"direction"`
	RelationshipType  string `json:"relationship_type"`
	IncludeTransitive bool   `json:"include_transitive"`
	MaxDepth          int    `json:"max_depth"`
	Limit             int    `json:"limit"`
	Offset            int    `json:"offset"`
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
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
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

	relationships, sourceBackend, basis, err := h.relationshipStoryRelationships(r.Context(), req, entity)
	if err != nil {
		if errors.Is(err, errSymbolBackendUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := relationshipStoryData(req, resolution, relationships)
	data["source_backend"] = sourceBackend
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), relationshipStoryCapability, basis, "resolved from bounded relationship story lookup"),
	)
}

func (r relationshipStoryRequest) validate() error {
	if strings.TrimSpace(r.EntityID) == "" && strings.TrimSpace(r.target()) == "" {
		return errors.New("entity_id or target is required")
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
		return "CALLS", nil
	}
	switch relationshipType {
	case "CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES":
		return relationshipType, nil
	default:
		return "", fmt.Errorf("relationship_type %q is not supported", strings.TrimSpace(r.RelationshipType))
	}
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
	availableByDirection := relationshipStoryDirectionCounts(rows)
	truncatedByDirection := relationshipStoryDirectionTruncation(availableByDirection, req, limit)
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	rows = relationshipStoryRowsWithHandles(rows)
	returnedByDirection := relationshipStoryDirectionCounts(rows)
	direction, _ := req.normalizedDirection()
	relationshipType, _ := req.normalizedRelationshipType()
	queryShape := "entity_anchor_one_hop"
	if req.IncludeTransitive {
		queryShape = "entity_anchor_bounded_bfs"
	}
	return map[string]any{
		"target_resolution": resolution,
		"scope": map[string]any{
			"repo_id":            strings.TrimSpace(req.RepoID),
			"language":           strings.TrimSpace(req.Language),
			"direction":          direction,
			"relationship_type":  relationshipType,
			"limit":              limit,
			"offset":             req.Offset,
			"max_depth":          relationshipStoryEffectiveMaxDepth(req),
			"include_transitive": req.IncludeTransitive,
		},
		"relationships": rows,
		"summary": map[string]any{
			"relationship_count":    len(rows),
			"returned_by_direction": returnedByDirection,
			"truncated":             truncated,
		},
		"coverage": map[string]any{
			"query_shape":            queryShape,
			"directions":             relationshipStoryDirections(direction),
			"relationship_types":     []string{relationshipType},
			"max_depth":              relationshipStoryEffectiveMaxDepth(req),
			"available_by_direction": availableByDirection,
			"returned_by_direction":  returnedByDirection,
			"truncated_by_direction": truncatedByDirection,
			"truncated":              truncated,
		},
	}
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
		if sourceID := StringVal(item, "source_id"); sourceID != "" {
			item["source_handle"] = "entity:" + sourceID
		}
		if targetID := StringVal(item, "target_id"); targetID != "" {
			item["target_handle"] = "entity:" + targetID
		}
		out = append(out, item)
	}
	return out
}

func relationshipStoryDirections(direction string) []string {
	if direction == "both" {
		return []string{"incoming", "outgoing"}
	}
	return []string{direction}
}
