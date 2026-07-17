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
	properties := []string{"uid", "id"}
	if req.graphAnchorPropertyResolved {
		if req.graphAnchorProperty == "" {
			return []map[string]any{}, nil
		}
		properties = []string{req.graphAnchorProperty}
	}
	for _, property := range properties {
		cypher, params := nornicDBRelationshipStoryGraphCypher(req, entityID, entityLabel, property, direction, access)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return normalizeNornicDBRelationshipStoryRows(rows), nil
		}
	}
	return []map[string]any{}, nil
}

// nornicDBRelationshipStoryAnchorPreflightSupported reports whether the
// one-time uid-first preflight has a bounded multi-type lookup. Repository-
// scoped requests can anchor every supported entity label through the selected
// Repository and its owned File before checking entity identity. Unscoped
// requests retain the indexed Function-only path.
func nornicDBRelationshipStoryAnchorPreflightSupported(
	req relationshipStoryRequest,
	entity *EntityContent,
) bool {
	if entity == nil {
		return false
	}
	label := nornicDBGraphLabelForContentEntityType(entity.EntityType)
	if label == "" {
		return false
	}
	return strings.TrimSpace(req.RepoID) != "" || label == "Function"
}

// resolveNornicDBRelationshipStoryAnchorProperty selects the canonical identity
// property for a supported target once per request. Content entity IDs are
// canonical graph uids; a legacy id lookup is used only when no uid anchor
// exists, so an unrelated node whose legacy id collides cannot contribute edges.
func (h *CodeHandler) resolveNornicDBRelationshipStoryAnchorProperty(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) (relationshipStoryRequest, error) {
	if !nornicDBRelationshipStoryAnchorPreflightSupported(req, entity) {
		return req, nil
	}
	entityID := strings.TrimSpace(req.EntityID)
	entityLabel := ""
	if entity != nil {
		if strings.TrimSpace(entity.EntityID) != "" {
			entityID = strings.TrimSpace(entity.EntityID)
		}
		entityLabel = nornicDBGraphLabelForContentEntityType(entity.EntityType)
	}
	if entityID == "" || entityLabel == "" {
		return req, nil
	}
	params := map[string]any{"entity_id": entityID}
	repoScoped := strings.TrimSpace(req.RepoID) != ""
	if repoScoped {
		params["repo_id"] = strings.TrimSpace(req.RepoID)
	}
	uidRow, err := h.Neo4j.RunSingle(
		ctx,
		nornicDBRelationshipStoryAnchorLookupCypher(entityLabel, "uid", repoScoped),
		params,
	)
	if err != nil {
		return req, err
	}
	if len(uidRow) > 0 {
		req.graphAnchorPropertyResolved = true
		req.graphAnchorProperty = "uid"
		return req, nil
	}
	idRow, err := h.Neo4j.RunSingle(
		ctx,
		nornicDBRelationshipStoryAnchorLookupCypher(entityLabel, "id", repoScoped),
		params,
	)
	if err != nil {
		return req, err
	}
	req.graphAnchorPropertyResolved = true
	if len(idRow) > 0 {
		req.graphAnchorProperty = "id"
	}
	return req, nil
}

func nornicDBRelationshipStoryAnchorLookupCypher(
	entityLabel string,
	property string,
	repoScoped bool,
) string {
	if repoScoped {
		return "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->" +
			"(anchor:" + entityLabel + ") WHERE anchor." + property +
			" = $entity_id RETURN true AS found LIMIT 1"
	}
	return "MATCH (anchor:" + entityLabel + " {" + property +
		": $entity_id}) RETURN true AS found LIMIT 1"
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
		MATCH ` + entityPattern + `<-[rel` + relPattern + `]-(source)
		OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:File)
		OPTIONAL MATCH (sourceRepo:Repository)-[:REPO_CONTAINS]->(sourceFile)
		OPTIONAL MATCH (anchor)<-[:CONTAINS]-(targetFile:File)
		OPTIONAL MATCH (targetRepo:Repository)-[:REPO_CONTAINS]->(targetFile)
		` + nornicDBRelationshipStoryWhere(predicates) + `
		RETURN 'incoming' as direction,
		       '` + relationshipType + `' as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       source.id as source_legacy_id,
		       source.uid as source_uid,
		       source.name as source_name,
		       source.repo_id as source_node_repo_id,
		       sourceRepo.id as source_repo_fallback_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       source.language as source_language_value,
		       source.lang as source_lang_value,
		       sourceFile.language as source_file_language,
		       anchor.id as target_legacy_id,
		       anchor.uid as target_uid,
		       anchor.name as target_name,
		       anchor.repo_id as target_node_repo_id,
		       targetRepo.id as target_repo_fallback_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       anchor.language as target_language_value,
		       anchor.lang as target_lang_value,
		       targetFile.language as target_file_language
		ORDER BY source.name, source.id, source.uid
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
		       '` + relationshipType + `' as type,
		       'direct_code_edge' as edge_origin,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       rel.evidence_source as evidence_source,
		       rel.why_trail_json as why_trail_json,
		       rel.why_trail_truncated as why_trail_truncated,
		       anchor.id as source_legacy_id,
		       anchor.uid as source_uid,
		       anchor.name as source_name,
		       anchor.repo_id as source_node_repo_id,
		       sourceRepo.id as source_repo_fallback_id,
		       sourceRepo.name as source_repo_name,
		       sourceFile.relative_path as source_file_path,
		       anchor.language as source_language_value,
		       anchor.lang as source_lang_value,
		       sourceFile.language as source_file_language,
		       target.id as target_legacy_id,
		       target.uid as target_uid,
		       target.name as target_name,
		       target.repo_id as target_node_repo_id,
		       targetRepo.id as target_repo_fallback_id,
		       targetRepo.name as target_repo_name,
		       targetFile.relative_path as target_file_path,
		       target.language as target_language_value,
		       target.lang as target_lang_value,
		       targetFile.language as target_file_language
		ORDER BY target.name, target.id, target.uid
		SKIP $offset
		LIMIT $limit
	`, params
}

type nornicDBStoryProjectionCandidate struct {
	key          string
	placeholders []string
}

func nornicDBStoryProjection(key string, placeholders ...string) nornicDBStoryProjectionCandidate {
	return nornicDBStoryProjectionCandidate{key: key, placeholders: placeholders}
}

func normalizeNornicDBRelationshipStoryRows(rows []map[string]any) []map[string]any {
	normalized := normalizeNornicDBRelationshipRows(rows)
	for _, row := range normalized {
		collapseNornicDBStoryProjection(row, "source_id",
			nornicDBStoryProjection("source_legacy_id", "source.id", "anchor.id"),
			nornicDBStoryProjection("source_uid", "source.uid", "anchor.uid"),
		)
		collapseNornicDBStoryProjection(row, "source_repo_id",
			nornicDBStoryProjection("source_node_repo_id", "source.repo_id", "anchor.repo_id"),
			nornicDBStoryProjection("source_repo_fallback_id", "sourceRepo.id"),
		)
		collapseNornicDBStoryProjection(row, "source_language",
			nornicDBStoryProjection("source_language_value", "source.language", "anchor.language"),
			nornicDBStoryProjection("source_lang_value", "source.lang", "anchor.lang"),
			nornicDBStoryProjection("source_file_language", "sourceFile.language"),
		)
		collapseNornicDBStoryProjection(row, "target_id",
			nornicDBStoryProjection("target_legacy_id", "target.id", "anchor.id"),
			nornicDBStoryProjection("target_uid", "target.uid", "anchor.uid"),
		)
		collapseNornicDBStoryProjection(row, "target_repo_id",
			nornicDBStoryProjection("target_node_repo_id", "target.repo_id", "anchor.repo_id"),
			nornicDBStoryProjection("target_repo_fallback_id", "targetRepo.id"),
		)
		collapseNornicDBStoryProjection(row, "target_language",
			nornicDBStoryProjection("target_language_value", "target.language", "anchor.language"),
			nornicDBStoryProjection("target_lang_value", "target.lang", "anchor.lang"),
			nornicDBStoryProjection("target_file_language", "targetFile.language"),
		)
		collapseNornicDBStoryProjection(row, "method_id",
			nornicDBStoryProjection("method_legacy_id", "method.id"),
			nornicDBStoryProjection("method_uid", "method.uid"),
		)
	}
	return normalized
}

func collapseNornicDBStoryProjection(
	row map[string]any,
	targetKey string,
	candidates ...nornicDBStoryProjectionCandidate,
) {
	projected := false
	var selected any
	for _, candidate := range candidates {
		value, present := row[candidate.key]
		if !present {
			continue
		}
		projected = true
		delete(row, candidate.key)
		text := strings.TrimSpace(StringVal(map[string]any{candidate.key: value}, candidate.key))
		if selected == nil && text != "" && text != candidate.key && !containsNornicDBStoryPlaceholder(text, candidate.placeholders) {
			selected = value
		}
	}
	if !projected {
		return
	}
	if selected == nil {
		delete(row, targetKey)
		return
	}
	row[targetKey] = selected
}

func containsNornicDBStoryPlaceholder(value string, placeholders []string) bool {
	for _, placeholder := range placeholders {
		if value == placeholder {
			return true
		}
	}
	return false
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
			return normalizeNornicDBRelationshipStoryRows(rows), nil
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
		RETURN method.id as method_legacy_id,
		       method.uid as method_uid,
		       method.name as method_name,
		       method.path as file_path,
		       method.start_line as start_line,
		       method.end_line as end_line
		ORDER BY method.name, method.id, method.uid
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
			return normalizeNornicDBRelationshipStoryRows(rows), nil
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
		       source.id as source_legacy_id,
		       source.uid as source_uid,
		       source.name as source_name,
		       anchor.id as target_legacy_id,
		       anchor.uid as target_uid,
		       anchor.name as target_name,
		       length(path) as depth
		ORDER BY depth DESC, source.name, source.id, source.uid
		LIMIT $limit
	`, maxDepth, anchorPattern), params
	}
	return fmt.Sprintf(`
		MATCH path = %s-[:INHERITS*1..%d]->(target:Class)
		RETURN 'outgoing' as direction,
		       anchor.id as source_legacy_id,
		       anchor.uid as source_uid,
		       anchor.name as source_name,
		       target.id as target_legacy_id,
		       target.uid as target_uid,
		       target.name as target_name,
		       length(path) as depth
		ORDER BY depth DESC, target.name, target.id, target.uid
		LIMIT $limit
	`, anchorPattern, maxDepth), params
}
