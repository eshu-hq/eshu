package query

import (
	"context"
	"fmt"
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

func (h *CodeHandler) nornicDBRelationshipStoryClassMethods(
	ctx context.Context,
	req relationshipStoryRequest,
	entityID string,
) ([]map[string]any, error) {
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBRelationshipStoryClassMethodsCypher(req, entityID, property)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}
	}
	return []map[string]any{}, nil
}

func nornicDBRelationshipStoryClassMethodsCypher(
	req relationshipStoryRequest,
	entityID string,
	property string,
) (string, map[string]any) {
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.normalizedLimit() + 1,
		"offset":    req.Offset,
	}
	classPattern := nornicDBNodePatternWithProperty("class", "Class", property, "$entity_id")
	return `
		MATCH ` + classPattern + `-[:CONTAINS]->(method:Function)
		RETURN coalesce(method.id, method.uid) as method_id,
		       method.name as method_name,
		       method.path as file_path,
		       method.start_line as start_line,
		       method.end_line as end_line
		ORDER BY method.name, method_id
		SKIP $offset
		LIMIT $limit
	`, params
}

func (h *CodeHandler) nornicDBRelationshipStoryInheritanceDepthRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entityID string,
	direction string,
) ([]map[string]any, error) {
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBRelationshipStoryInheritanceDepthCypher(req, entityID, direction, property)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}
	}
	return []map[string]any{}, nil
}

func nornicDBRelationshipStoryInheritanceDepthCypher(
	req relationshipStoryRequest,
	entityID string,
	direction string,
	property string,
) (string, map[string]any) {
	maxDepth := normalizedRelationshipStoryMaxDepth(req.MaxDepth)
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.normalizedLimit() + 1,
	}
	anchorPattern := nornicDBNodePatternWithProperty("anchor", "Class", property, "$entity_id")
	if direction == "incoming" {
		return fmt.Sprintf(`
		MATCH path = (source:Class)-[:INHERITS*1..%d]->%s
		RETURN 'incoming' as direction,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(anchor.id, anchor.uid) as target_id,
		       anchor.name as target_name,
		       length(path) as depth
		ORDER BY depth, source.name, source_id
		LIMIT $limit
	`, maxDepth, anchorPattern), params
	}
	return fmt.Sprintf(`
		MATCH path = %s-[:INHERITS*1..%d]->(target:Class)
		RETURN 'outgoing' as direction,
		       coalesce(anchor.id, anchor.uid) as source_id,
		       anchor.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name,
		       length(path) as depth
		ORDER BY depth, target.name, target_id
		LIMIT $limit
	`, anchorPattern, maxDepth), params
}
