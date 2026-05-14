package query

import (
	"context"
	"strings"
)

func (h *CodeHandler) relationshipStoryRelationships(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, string, TruthBasis, error) {
	if h != nil && h.Neo4j != nil {
		rows, err := h.relationshipStoryGraphRows(ctx, req, entity)
		if err != nil {
			return nil, "", "", err
		}
		return rows, "graph", TruthBasisAuthoritativeGraph, nil
	}
	if h != nil && h.Content != nil && entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		row, err := h.relationshipsFromEntity(ctx, *entity)
		if err != nil {
			return nil, "", "", err
		}
		return relationshipStoryContentRows(row, req), "postgres_content_store", TruthBasisContentIndex, nil
	}
	return nil, "", "", errSymbolBackendUnavailable
}

func (h *CodeHandler) relationshipStoryGraphRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	if req.IncludeTransitive {
		return h.relationshipStoryTransitiveGraphRows(ctx, req, entity)
	}
	direction, _ := req.normalizedDirection()
	if direction != "both" {
		return h.relationshipStoryGraphRowsForDirection(ctx, req, entity, direction)
	}

	type directionResult struct {
		direction string
		rows      []map[string]any
		err       error
	}
	results := make(chan directionResult, 2)
	for _, current := range []string{"incoming", "outgoing"} {
		go func(direction string) {
			rows, err := h.relationshipStoryGraphRowsForDirection(ctx, req, entity, direction)
			results <- directionResult{direction: direction, rows: rows, err: err}
		}(current)
	}
	byDirection := map[string][]map[string]any{}
	for range 2 {
		result := <-results
		if result.err != nil {
			return nil, result.err
		}
		byDirection[result.direction] = result.rows
	}
	rows := append([]map[string]any{}, byDirection["incoming"]...)
	rows = append(rows, byDirection["outgoing"]...)
	return rows, nil
}

func (h *CodeHandler) relationshipStoryTransitiveGraphRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	direction, _ := req.normalizedDirection()
	limit := req.normalizedLimit() + 1
	rootID := strings.TrimSpace(req.EntityID)
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		rootID = strings.TrimSpace(entity.EntityID)
	}
	if rootID == "" {
		return []map[string]any{}, nil
	}

	frontier := []string{rootID}
	seen := map[string]struct{}{rootID: {}}
	rows := make([]map[string]any, 0, limit)
	for depth := 1; depth <= normalizedRelationshipStoryMaxDepth(req.MaxDepth) && len(frontier) > 0 && len(rows) < limit; depth++ {
		next := make([]string, 0)
		for _, currentID := range frontier {
			hopReq := req
			hopReq.EntityID = currentID
			hopReq.Offset = 0
			hopReq.Limit = limit - len(rows)
			hopReq.IncludeTransitive = false
			hopRows, err := h.relationshipStoryGraphRowsForDirection(
				ctx,
				hopReq,
				&EntityContent{EntityID: currentID},
				direction,
			)
			if err != nil {
				return nil, err
			}
			for _, hop := range hopRows {
				nextID := relationshipStoryNextID(hop, direction)
				if nextID == "" {
					continue
				}
				item := cloneQueryAnyMap(hop)
				item["depth"] = depth
				rows = append(rows, item)
				if _, ok := seen[nextID]; !ok {
					seen[nextID] = struct{}{}
					next = append(next, nextID)
				}
				if len(rows) >= limit {
					break
				}
			}
			if len(rows) >= limit {
				break
			}
		}
		frontier = next
	}
	return rows, nil
}

func relationshipStoryNextID(row map[string]any, direction string) string {
	if direction == "incoming" {
		return StringVal(row, "source_id")
	}
	return StringVal(row, "target_id")
}

func (h *CodeHandler) relationshipStoryGraphRowsForDirection(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
) ([]map[string]any, error) {
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipStoryGraphRows(ctx, req, entity, direction)
	}
	cypher, params := relationshipStoryGraphCypher(req, entity, direction, graphEntityIDPredicate)
	return h.Neo4j.Run(ctx, cypher, params)
}

func relationshipStoryGraphCypher(
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
	predicate func(string, string) string,
) (string, map[string]any) {
	relationshipType, _ := req.normalizedRelationshipType()
	params := map[string]any{
		"entity_id": strings.TrimSpace(req.EntityID),
		"limit":     req.normalizedLimit() + 1,
		"offset":    req.Offset,
	}
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		params["entity_id"] = strings.TrimSpace(entity.EntityID)
	}
	relPattern := ":" + relationshipType
	if direction == "incoming" {
		return `
		MATCH (source)-[rel` + relPattern + `]->(target)
		WHERE ` + predicate("target", "$entity_id") + `
		RETURN 'incoming' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
		ORDER BY source.name, source_id
		SKIP $offset
		LIMIT $limit
	`, params
	}
	return `
		MATCH (source)-[rel` + relPattern + `]->(target)
		WHERE ` + predicate("source", "$entity_id") + `
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
		ORDER BY target.name, target_id
		SKIP $offset
		LIMIT $limit
	`, params
}

func relationshipStoryContentRows(row map[string]any, req relationshipStoryRequest) []map[string]any {
	relationshipType, _ := req.normalizedRelationshipType()
	direction, _ := req.normalizedDirection()
	rows := make([]map[string]any, 0)
	if direction != "outgoing" {
		rows = append(rows, filterRelationships(mapRelationships(row["incoming"]), relationshipType)...)
	}
	if direction != "incoming" {
		rows = append(rows, filterRelationships(mapRelationships(row["outgoing"]), relationshipType)...)
	}
	limit := req.normalizedLimit() + 1
	if len(rows) > req.Offset {
		rows = rows[req.Offset:]
	} else {
		rows = []map[string]any{}
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}
