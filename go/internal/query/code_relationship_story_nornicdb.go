// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	access := repositoryAccessFilterFromContext(ctx)
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBRelationshipStoryGraphCypher(req, entityID, entityLabel, property, direction, access)
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
	access repositoryAccessFilter,
) (string, map[string]any) {
	relationshipType, _ := req.normalizedRelationshipType()
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
		"limit":     req.normalizedLimit() + 1,
		"offset":    req.Offset,
	}
	params = relationshipStoryAccessParams(req, access, params)
	relPattern := ":" + relationshipType
	entityPattern := nornicDBNodePatternWithProperty("anchor", entityLabel, property, "$entity_id")
	if direction == "incoming" {
		predicates := relationshipStoryRepoPredicates(req, access, "targetRepo")
		return `
		MATCH (source)-[rel` + relPattern + `]->` + entityPattern + `
		OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:File)
		OPTIONAL MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		OPTIONAL MATCH (anchor)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		` + nornicDBRelationshipStoryWhere(predicates) + `
		RETURN 'incoming' as direction,
		       type(rel) as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(source.repo_id, sourceRepo.id) as source_repo_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       coalesce(source.language, source.lang, sourceFile.language) as source_language,
		       coalesce(anchor.id, anchor.uid) as target_id,
		       anchor.name as target_name,
		       coalesce(anchor.repo_id, targetRepo.id) as target_repo_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       coalesce(anchor.language, anchor.lang, targetFile.language) as target_language
		ORDER BY source.name, source_id
		SKIP $offset
		LIMIT $limit
	`, params
	}
	predicates := relationshipStoryRepoPredicates(req, access, "sourceRepo")
	return `
		MATCH ` + entityPattern + `-[rel` + relPattern + `]->(target)
		OPTIONAL MATCH (anchor)<-[:CONTAINS]-(sourceFile:File)
		OPTIONAL MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		` + nornicDBRelationshipStoryWhere(predicates) + `
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       coalesce(anchor.id, anchor.uid) as source_id,
		       anchor.name as source_name,
		       coalesce(anchor.repo_id, sourceRepo.id) as source_repo_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       coalesce(anchor.language, anchor.lang, sourceFile.language) as source_language,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name,
		       coalesce(target.repo_id, targetRepo.id) as target_repo_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       coalesce(target.language, target.lang, targetFile.language) as target_language
		ORDER BY target.name, target_id
		SKIP $offset
		LIMIT $limit
	`, params
}

func nornicDBRelationshipStoryWhere(predicates []string) string {
	if len(predicates) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(predicates, " AND ")
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
		ORDER BY depth DESC, source.name, source_id
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
		ORDER BY depth DESC, target.name, target_id
		LIMIT $limit
	`, anchorPattern, maxDepth), params
}
