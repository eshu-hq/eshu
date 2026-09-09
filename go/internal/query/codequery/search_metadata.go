// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func (h *CodeHandler) enrichGraphSearchResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	repoID string,
	query string,
	limit int,
) ([]map[string]any, error) {
	if len(results) == 0 {
		return results, nil
	}

	allHaveMetadata := true
	for i := range results {
		metadata, ok := results[i]["metadata"].(map[string]any)
		if !ok || len(metadata) == 0 {
			allHaveMetadata = false
			continue
		}
		entitysemantics.AttachSemanticSummary(results[i])
	}

	if allHaveMetadata || h == nil || h.Content == nil {
		return results, nil
	}

	rows, err := h.Content.SearchEntityContent(ctx, repoID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("enrich graph search results with content metadata: %w", err)
	}
	if len(rows) == 0 {
		return results, nil
	}

	metadataByKey := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		metadataByKey[querycontract.LanguageResultMatchKey(
			row.RelativePath,
			row.EntityType,
			row.EntityName,
			row.StartLine,
		)] = row.Metadata
	}

	for i := range results {
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			continue
		}
		entityType := ResultContentEntityType(results[i])
		if entityType == "" {
			continue
		}
		key := querycontract.LanguageResultMatchKey(
			StringVal(results[i], "file_path"),
			entityType,
			StringVal(results[i], "name"),
			IntVal(results[i], "start_line"),
		)
		metadata, ok := metadataByKey[key]
		if !ok || len(metadata) == 0 {
			continue
		}
		results[i]["metadata"] = metadata
		entitysemantics.AttachSemanticSummary(results[i])
	}

	return results, nil
}

func (h *CodeHandler) enrichGraphResultsWithContentMetadataByEntityID(
	ctx context.Context,
	results []map[string]any,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || len(results) == 0 {
		for i := range results {
			if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
				entitysemantics.AttachSemanticSummary(results[i])
			}
		}
		return results, nil
	}

	for i := range results {
		entityID := StringVal(results[i], "entity_id")
		if entityID == "" {
			continue
		}
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			entitysemantics.AttachSemanticSummary(results[i])
		}
		entity, err := h.Content.GetEntityContent(ctx, entityID)
		if err != nil {
			return nil, fmt.Errorf("enrich graph results by entity id: %w", err)
		}
		if entity == nil || len(entity.Metadata) == 0 {
			continue
		}
		results[i]["metadata"] = mergeGraphAndContentMetadata(results[i]["metadata"], entity.Metadata)
		entitysemantics.AttachSemanticSummary(results[i])
	}

	return results, nil
}

// ResultContentEntityType resolves a result row's content-entity type from
// its graph labels. The implementation moved to querycontract with lane B5
// of #6060; this wrapper keeps codequery callers unchanged.
func ResultContentEntityType(result map[string]any) string {
	return querycontract.ResultContentEntityType(result)
}

func mergeGraphAndContentMetadata(existing any, content map[string]any) map[string]any {
	if len(content) == 0 {
		merged, _ := existing.(map[string]any)
		if len(merged) == 0 {
			return nil
		}
		return cloneQueryAnyMap(merged)
	}

	merged, _ := existing.(map[string]any)
	if len(merged) == 0 {
		return cloneQueryAnyMap(content)
	}

	result := cloneQueryAnyMap(merged)
	for key, value := range content {
		if _, ok := result[key]; ok {
			continue
		}
		result[key] = value
	}
	return result
}

func cloneQueryAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func deadCodeInvestigationNextCalls(scan deadcode.DeadCodeInvestigationScan) []map[string]any {
	candidates := append([]map[string]any{}, scan.CleanupReady...)
	candidates = append(candidates, scan.Ambiguous...)
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	next := make([]map[string]any, 0, len(candidates)*4)
	for _, candidate := range candidates {
		entityID := StringVal(candidate, "entity_id")
		if entityID == "" {
			continue
		}
		next = append(next, map[string]any{
			"tool":      "get_entity_content",
			"arguments": map[string]any{"entity_id": entityID},
			"reason":    "read the exact source before changing or deleting the candidate",
		})
		for _, relationshipType := range deadCodeInvestigationRelationshipTypes(candidate) {
			next = append(next, deadCodeInvestigationRelationshipCall(entityID, relationshipType))
		}
		if querycontract.PrimaryEntityLabel(candidate) == "SqlFunction" {
			next = append(next, deadCodeInvestigationSQLExecuteCall(candidate))
		}
	}
	return next
}

func deadCodeInvestigationRelationshipTypes(candidate map[string]any) []string {
	switch querycontract.PrimaryEntityLabel(candidate) {
	case "Function":
		return []string{"CALLS", "REFERENCES", "IMPORTS"}
	case "Class", "Struct":
		return []string{"REFERENCES", "INHERITS"}
	case "Interface", "Trait":
		return []string{"REFERENCES", "INHERITS", "OVERRIDES"}
	case "SqlFunction":
		return nil
	default:
		return []string{"REFERENCES"}
	}
}

func deadCodeInvestigationRelationshipCall(entityID string, relationshipType string) map[string]any {
	return map[string]any{
		"tool": "get_code_relationship_story",
		"arguments": map[string]any{
			"entity_id":          entityID,
			"direction":          "incoming",
			"relationship_type":  relationshipType,
			"include_transitive": false,
			"limit":              25,
			"offset":             0,
		},
		"reason": "check incoming " + relationshipType + " evidence before treating the candidate as cleanup-ready",
	}
}

func deadCodeInvestigationSQLExecuteCall(candidate map[string]any) map[string]any {
	entityID := StringVal(candidate, "entity_id")
	return map[string]any{
		"tool": "execute_cypher_query",
		"arguments": map[string]any{
			"cypher_query": "MATCH (e:SqlFunction {uid: " + deadCodeCypherStringLiteral(entityID) + "})<-[:EXECUTES]-(source) RETURN coalesce(source.uid, source.id) as source_id, labels(source) as source_labels LIMIT 25",
			"limit":        25,
		},
		"reason": "SQL routine reachability uses EXECUTES edges, which relationship-story does not expose",
	}
}

func deadCodeCypherStringLiteral(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}
