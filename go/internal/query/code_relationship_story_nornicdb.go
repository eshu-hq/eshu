package query

import (
	"context"
	"strings"
)

func (h *CodeHandler) nornicDBRelationshipStoryGraphRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
) ([]map[string]any, error) {
	entityID := strings.TrimSpace(req.EntityID)
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		entityID = strings.TrimSpace(entity.EntityID)
	}
	entityLabel := ""
	if entity != nil {
		entityLabel = nornicDBGraphLabelForContentEntityType(entity.EntityType)
	}
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBRelationshipStoryGraphCypher(req, entityID, entityLabel, property, direction)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return normalizeNornicDBRelationshipRows(rows), nil
		}
	}
	return []map[string]any{}, nil
}

func nornicDBRelationshipStoryGraphCypher(
	req relationshipStoryRequest,
	entityID string,
	entityLabel string,
	property string,
	direction string,
) (string, map[string]any) {
	relationshipType, _ := req.normalizedRelationshipType()
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.normalizedLimit() + 1,
		"offset":    req.Offset,
	}
	relPattern := ":" + relationshipType
	entityPattern := nornicDBNodePatternWithProperty("anchor", entityLabel, property, "$entity_id")
	if direction == "incoming" {
		return `
		MATCH (source)-[rel` + relPattern + `]->` + entityPattern + `
		RETURN 'incoming' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(anchor.id, anchor.uid) as target_id,
		       anchor.name as target_name
		ORDER BY source.name, source_id
		SKIP $offset
		LIMIT $limit
	`, params
	}
	return `
		MATCH ` + entityPattern + `-[rel` + relPattern + `]->(target)
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       coalesce(anchor.id, anchor.uid) as source_id,
		       anchor.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
		ORDER BY target.name, target_id
		SKIP $offset
		LIMIT $limit
	`, params
}
