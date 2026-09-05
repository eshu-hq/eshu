// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RelationshipGraphRowCypher returns the single-row relationship Cypher fragment.
func RelationshipGraphRowCypher(predicate string) string {
	return `
		MATCH (e) WHERE ` + predicate + `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		OPTIONAL MATCH (e)-[outgoingRel]->(target)
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)<-[:REPO_CONTAINS]-(targetRepo:Repository)
		OPTIONAL MATCH (source)-[incomingRel]->(e)
		OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:File)<-[:REPO_CONTAINS]-(sourceRepo:Repository)
		RETURN coalesce(e.id, e.uid) as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		       ,collect(DISTINCT {
		           direction: 'outgoing',
		           type: type(outgoingRel),
		           call_kind: outgoingRel.call_kind,
		           reason: outgoingRel.reason,
		           confidence: outgoingRel.confidence,
		           resolution_method: outgoingRel.resolution_method,
		           source_name: e.name,
		           source_id: coalesce(e.id, e.uid),
		           source_repo_id: repo.id,
		           source_repo_name: repo.name,
		           source_file_path: f.relative_path,
		           source_language: coalesce(e.language, f.language),
		           source_type: head(labels(e)),
		           source_start_line: e.start_line,
		           source_end_line: e.end_line,
		           target_name: target.name,
		           target_id: coalesce(target.id, target.uid),
		           target_repo_id: targetRepo.id,
		           target_repo_name: targetRepo.name,
		           target_file_path: targetFile.relative_path,
		           target_language: coalesce(target.language, targetFile.language),
		           target_type: head(labels(target)),
		           target_start_line: target.start_line,
		           target_end_line: target.end_line
		       }) as outgoing,
		       collect(DISTINCT {
		           direction: 'incoming',
		           type: type(incomingRel),
		           call_kind: incomingRel.call_kind,
		           reason: incomingRel.reason,
		           confidence: incomingRel.confidence,
		           resolution_method: incomingRel.resolution_method,
		           source_name: source.name,
		           source_id: coalesce(source.id, source.uid),
		           source_repo_id: sourceRepo.id,
		           source_repo_name: sourceRepo.name,
		           source_file_path: sourceFile.relative_path,
		           source_language: coalesce(source.language, sourceFile.language),
		           source_type: head(labels(source)),
		           source_start_line: source.start_line,
		           source_end_line: source.end_line,
		           target_name: e.name,
		           target_id: coalesce(e.id, e.uid),
		           target_repo_id: repo.id,
		           target_repo_name: repo.name,
		           target_file_path: f.relative_path,
		           target_language: coalesce(e.language, f.language),
		           target_type: head(labels(e)),
		           target_start_line: e.start_line,
		           target_end_line: e.end_line
		       }) as incoming
		LIMIT 2
	`
}

// BuildTransitiveRelationshipRowsCypher returns the bounded transitive CALLS traversal Cypher.
func BuildTransitiveRelationshipRowsCypher(
	entityID string,
	direction string,
	maxDepth int,
	backend querycontract.GraphBackend,
) (string, map[string]any) {
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
	}
	var cypher strings.Builder
	if backend == querycontract.GraphBackendNornicDB {
		if direction == "incoming" {
			cypher.WriteString("\n\t\tMATCH (e)\n")
			cypher.WriteString("\t\tWHERE ")
			cypher.WriteString(GraphEntityIDPredicate("e", "$entity_id"))
			cypher.WriteString("\n\t\tMATCH path = (e)<-[:CALLS*1..")
			fmt.Fprint(&cypher, maxDepth)
			cypher.WriteString("]-(source)\n")
			cypher.WriteString("\t\tRETURN source.name as source_name,\n")
			cypher.WriteString("\t\t       coalesce(source.id, source.uid) as source_id,\n")
			cypher.WriteString("\t\t       length(path) as depth\n\t")
			return cypher.String(), params
		}

		cypher.WriteString("\n\t\tMATCH (e)\n")
		cypher.WriteString("\t\tWHERE ")
		cypher.WriteString(GraphEntityIDPredicate("e", "$entity_id"))
		cypher.WriteString("\n\t\tMATCH path = (e)-[:CALLS*1..")
		fmt.Fprint(&cypher, maxDepth)
		cypher.WriteString("]->(target)\n")
		cypher.WriteString("\t\tRETURN target.name as target_name,\n")
		cypher.WriteString("\t\t       coalesce(target.id, target.uid) as target_id,\n")
		cypher.WriteString("\t\t       length(path) as depth\n\t")
		return cypher.String(), params
	}

	cypher.WriteString("\n\t\tMATCH (e)\n")
	cypher.WriteString("\t\tWHERE ")
	cypher.WriteString(GraphEntityIDPredicate("e", "$entity_id"))
	cypher.WriteString("\n")
	if direction == "incoming" {
		cypher.WriteString("\t\tMATCH path = (e)<-[:CALLS*1..")
		fmt.Fprint(&cypher, maxDepth)
		cypher.WriteString("]-(source)\n")
		cypher.WriteString("\t\tRETURN source.name as source_name,\n")
		cypher.WriteString("\t\t       coalesce(source.id, source.uid) as source_id,\n")
		cypher.WriteString("\t\t       length(path) as depth\n\t")
		return cypher.String(), params
	}

	cypher.WriteString("\t\tMATCH path = (e)-[:CALLS*1..")
	fmt.Fprint(&cypher, maxDepth)
	cypher.WriteString("]->(target)\n")
	cypher.WriteString("\t\tRETURN target.name as target_name,\n")
	cypher.WriteString("\t\t       coalesce(target.id, target.uid) as target_id,\n")
	cypher.WriteString("\t\t       length(path) as depth\n\t")
	return cypher.String(), params
}

// GraphEntityIDPredicate returns the entity-identity MATCH predicate for one alias.
func GraphEntityIDPredicate(alias string, param string) string {
	return fmt.Sprintf("(%s.id = %s OR %s.uid = %s)", alias, param, alias, param)
}

// BuildTransitiveRelationshipGraphResponse shapes transitive relationship rows into the response envelope.
func BuildTransitiveRelationshipGraphResponse(metadataRow map[string]any, rows []map[string]any, direction string) map[string]any {
	response := cloneQueryAnyMap(metadataRow)
	response["outgoing"] = []map[string]any{}
	response["incoming"] = []map[string]any{}

	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		depth := querycontract.IntVal(row, "depth")
		if depth <= 0 {
			continue
		}
		if direction == "incoming" {
			sourceID := querycontract.StringVal(row, "source_id")
			sourceName := querycontract.StringVal(row, "source_name")
			key := fmt.Sprintf("incoming:%s:%s:%d", sourceID, sourceName, depth)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			response["incoming"] = append(response["incoming"].([]map[string]any), map[string]any{
				"direction":   "incoming",
				"type":        "CALLS",
				"source_name": sourceName,
				"source_id":   sourceID,
				"depth":       depth,
				"reason":      "transitive_call_graph",
			})
			continue
		}
		targetID := querycontract.StringVal(row, "target_id")
		targetName := querycontract.StringVal(row, "target_name")
		key := fmt.Sprintf("outgoing:%s:%s:%d", targetID, targetName, depth)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		response["outgoing"] = append(response["outgoing"].([]map[string]any), map[string]any{
			"direction":   "outgoing",
			"type":        "CALLS",
			"target_name": targetName,
			"target_id":   targetID,
			"depth":       depth,
			"reason":      "transitive_call_graph",
		})
	}

	return response
}

// mapRelationships is a family-local copy of root's code_relationships.go
// helper of the same name, with filterNullRelationships copied from root's
// infra_relationship_filter.go. The graph response normalizer shapes
// nullable relationship slices through them, and the staying
// relationship/infra readers that share them cannot cross the package
// boundary, so the leaf carries these byte-identical copies instead of
// importing root. Keep them behavior-identical to their root sources.
func mapRelationships(value any) []map[string]any {
	relationships, ok := value.([]map[string]any)
	if ok {
		return relationships
	}
	return filterNullRelationships(value)
}

// filterNullRelationships removes entries where type is nil (from OPTIONAL MATCH with no matches).
func filterNullRelationships(v any) []map[string]any {
	switch slice := v.(type) {
	case []map[string]any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			if item["type"] == nil {
				continue
			}
			result = append(result, item)
		}
		return result
	case []any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			// Skip entries where type is nil (no relationship matched)
			if m["type"] == nil {
				continue
			}
			result = append(result, m)
		}
		return result
	default:
		return nil
	}
}

// dropNilOrEmptyRowKey is a family-local copy of root's
// code_relationship_story.go helper of the same name. The graph response
// normalizer omits null optional per-edge provenance fields through it,
// and the staying story shaper that shares it cannot cross the package
// boundary, so the leaf carries this byte-identical copy instead of
// importing root. Keep it behavior-identical to its root source.
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

// NormalizeGraphRelationships normalizes the relationship slices of a graph response.
func NormalizeGraphRelationships(response map[string]any) {
	response["outgoing"] = normalizeGraphRelationshipSlice(mapRelationships(response["outgoing"]))
	response["incoming"] = normalizeGraphRelationshipSlice(mapRelationships(response["incoming"]))
}

func normalizeGraphRelationshipSlice(relationships []map[string]any) []map[string]any {
	if len(relationships) == 0 {
		return relationships
	}
	normalized := make([]map[string]any, 0, len(relationships))
	for _, relationship := range relationships {
		item := make(map[string]any, len(relationship)+1)
		for key, value := range relationship {
			item[key] = value
		}
		if querycontract.StringVal(item, "type") == "CALLS" && querycontract.StringVal(item, "call_kind") == "jsx_component" {
			item["type"] = "REFERENCES"
			if querycontract.StringVal(item, "reason") == "" {
				item["reason"] = "jsx_component_call_kind"
			}
		}
		dropNilOrEmptyRowKey(item, "confidence")
		dropNilOrEmptyRowKey(item, "resolution_method")
		normalized = append(normalized, item)
	}
	return normalized
}
