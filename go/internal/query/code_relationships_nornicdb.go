// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

func (h *CodeHandler) nornicDBRelationshipsGraphRow(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
	direction string,
	relationshipType string,
) (map[string]any, error) {
	row, err := h.nornicDBRelationshipMetadataRow(ctx, entityID, name, repoID)
	if err != nil || row == nil {
		return row, err
	}

	rowEntityID := StringVal(row, "id")
	entityLabel := nornicDBPrimaryEntityLabel(row)
	outgoing := []map[string]any{}
	outgoingTruncated := false
	if direction != "incoming" {
		var err error
		// NornicDB currently needs metadata and each requested direction as
		// separate row queries; this avoids Neo4j-style map collection shapes
		// that are not dialect-safe while preserving direct relationship truth.
		outgoing, outgoingTruncated, err = h.nornicDBOneHopRelationships(ctx, rowEntityID, "outgoing", relationshipType, entityLabel)
		if err != nil {
			return nil, err
		}
	}
	incoming := []map[string]any{}
	incomingTruncated := false
	if direction != "outgoing" {
		var err error
		incoming, incomingTruncated, err = h.nornicDBOneHopRelationships(ctx, rowEntityID, "incoming", relationshipType, entityLabel)
		if err != nil {
			return nil, err
		}
	}
	response := cloneQueryAnyMap(row)
	response["outgoing"] = outgoing
	response["incoming"] = incoming
	response["outgoing_truncated"] = outgoingTruncated
	response["incoming_truncated"] = incomingTruncated
	return response, nil
}

func nornicDBPrimaryEntityLabel(row map[string]any) string {
	for _, label := range StringSliceVal(row, "labels") {
		if graphLabelToContentEntityType(label) != "" {
			return label
		}
	}
	return ""
}

func (h *CodeHandler) nornicDBRelationshipMetadataRow(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}

	entityLabel, err := h.nornicDBRelationshipEntityLabel(ctx, entityID, repoID)
	if err != nil {
		return nil, err
	}
	predicate, params := nornicDBRelationshipMetadataPredicate(name, repoID)
	entityID = strings.TrimSpace(entityID)
	if predicate == "" && entityID == "" {
		return nil, nil
	}
	if entityID != "" {
		if entityLabel == "" {
			return nil, nil
		}
		params["entity_id"] = entityID
		for _, property := range []string{"uid", "id"} {
			rows, err := h.Neo4j.Run(ctx, nornicDBRelationshipMetadataCypher(predicate, entityLabel, property), params)
			if err != nil {
				return nil, err
			}
			if len(rows) == 1 {
				return rows[0], nil
			}
			if len(rows) > 1 {
				return nil, nil
			}
		}
		return nil, nil
	}
	rows, err := h.Neo4j.Run(ctx, nornicDBRelationshipMetadataCypher(predicate, entityLabel, ""), params)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, nil
	}
	return rows[0], nil
}

func nornicDBGraphLabelForContentEntityType(entityType string) string {
	label := strings.TrimSpace(entityType)
	if graphLabelToContentEntityType(label) == "" {
		return ""
	}
	return label
}

func nornicDBRelationshipMetadataPredicate(
	name string,
	repoID string,
) (string, map[string]any) {
	params := make(map[string]any)
	var predicates []string
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		predicates = append(predicates, "e.name = $name")
		params["name"] = trimmed
	}
	if trimmed := strings.TrimSpace(repoID); trimmed != "" {
		predicates = append(predicates, "repo.id = $repo_id")
		params["repo_id"] = trimmed
	}
	return strings.Join(predicates, " AND "), params
}

func nornicDBRelationshipMetadataCypher(predicate string, entityLabel string, entityIDProperty string) string {
	entityPattern := "(e" + nornicDBLabelPattern(entityLabel) + ")"
	if strings.TrimSpace(entityIDProperty) != "" {
		entityPattern = nornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	}
	var predicates []string
	if trimmed := strings.TrimSpace(predicate); trimmed != "" {
		predicates = append(predicates, trimmed)
	}
	whereClause := ""
	if len(predicates) > 0 {
		whereClause = `
		WHERE ` + strings.Join(predicates, " AND ")
	}
	return `
		MATCH ` + entityPattern + `<-[:CONTAINS]-(f:File)
		MATCH (repo:Repository)-[:REPO_CONTAINS]->(f)
		` + whereClause + `
		RETURN coalesce(e.id, e.uid) as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		LIMIT 2
	`
}

// nornicDBOneHopRelationships returns a single symbol's direct relationships for
// one direction. The bool reports whether the row ceiling clipped the result so
// the caller can disclose truncation instead of presenting a clipped set as an
// exact-truth response.
func (h *CodeHandler) nornicDBOneHopRelationships(
	ctx context.Context,
	entityID string,
	direction string,
	relationshipType string,
	entityLabel string,
) ([]map[string]any, bool, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return []map[string]any{}, false, nil
	}
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBOneHopRelationshipsCypher(entityID, direction, relationshipType, entityLabel, property)
		rows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, false, err
		}
		if len(rows) > 0 {
			truncated := len(rows) > nornicDBRelationshipRowLimit
			if truncated {
				rows = rows[:nornicDBRelationshipRowLimit]
			}
			enriched, err := h.enrichNornicDBRelationshipRows(ctx, rows, entityID, direction, relationshipType, entityLabel, property)
			if err != nil {
				return nil, false, err
			}
			return normalizeNornicDBRelationshipRows(enriched), truncated, nil
		}
	}
	return []map[string]any{}, false, nil
}

// nornicDBOneHopRelationshipsCypher is the relationship core read. It carries no
// trailing OPTIONAL MATCH: on the pinned NornicDB build, a relationship-bound
// MATCH followed by any OPTIONAL MATCH routes to an executor branch that emits
// every function-call projection (type(rel), coalesce(...), head(labels(...)))
// as its literal source text instead of the evaluated value, silently
// corrupting the relationship type and identity columns. See
// docs/public/reference/nornicdb-pitfalls.md ("Trailing OPTIONAL MATCH Corrupts
// Every Function-Call Projection"). File and repository metadata is fetched by the
// separate, OPTIONAL-MATCH-free enrichment reads in
// code_relationships_nornicdb_enrich.go and merged in Go. The extra
// source_entity_uid/target_entity_uid columns key that merge and are stripped
// before the response.
// nornicDBRelationshipRowLimit bounds a single symbol's direct-relationship read
// so the hot API path cannot issue an unbounded graph read for a pathological
// high-degree node (for example an incoming-CALLS hub). Direct relationships of
// one symbol and one type are far below this ceiling in practice; the
// deterministic ORDER BY makes the bound stable rather than arbitrary. The reads
// over-fetch by one row so a caller can be told, via the response's
// outgoing_truncated/incoming_truncated flags, when the ceiling clipped the
// result — the exact-truth envelope must never silently drop edges.
const nornicDBRelationshipRowLimit = 500

// nornicDBRelationshipFetchLimit over-fetches one row past the ceiling so
// truncation is detectable without a second count query.
const nornicDBRelationshipFetchLimit = nornicDBRelationshipRowLimit + 1

func nornicDBOneHopRelationshipsCypher(entityID string, direction string, relationshipType string, entityLabel string, entityIDProperty string) (string, map[string]any) {
	params := map[string]any{"entity_id": entityID, "row_limit": nornicDBRelationshipFetchLimit}
	relPattern := nornicDBRelationshipPattern(relationshipType)
	entityPattern := nornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	if direction == "incoming" {
		return `
		MATCH ` + entityPattern + `<-[rel` + relPattern + `]-(source)
		RETURN 'incoming' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       source.name as source_name,
		       coalesce(source.id, source.uid) as source_id,
		       coalesce(source.id, source.uid) as source_entity_uid,
		       source.language as source_language,
		       head(labels(source)) as source_type,
		       source.start_line as source_start_line,
		       source.end_line as source_end_line,
		       e.name as target_name,
		       coalesce(e.id, e.uid) as target_id,
		       coalesce(e.id, e.uid) as target_entity_uid,
		       e.language as target_language,
		       head(labels(e)) as target_type,
		       e.start_line as target_start_line,
		       e.end_line as target_end_line
		ORDER BY source.uid
		LIMIT $row_limit
	`, params
	}
	return `
		MATCH ` + entityPattern + `-[rel` + relPattern + `]->(target)
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       rel.confidence as confidence,
		       rel.resolution_method as resolution_method,
		       e.name as source_name,
		       coalesce(e.id, e.uid) as source_id,
		       coalesce(e.id, e.uid) as source_entity_uid,
		       e.language as source_language,
		       head(labels(e)) as source_type,
		       e.start_line as source_start_line,
		       e.end_line as source_end_line,
		       target.name as target_name,
		       coalesce(target.id, target.uid) as target_id,
		       coalesce(target.id, target.uid) as target_entity_uid,
		       target.language as target_language,
		       head(labels(target)) as target_type,
		       target.start_line as target_start_line,
		       target.end_line as target_end_line
		ORDER BY target.uid
		LIMIT $row_limit
	`, params
}

func nornicDBLabelPattern(label string) string {
	label = nornicDBGraphLabelForContentEntityType(label)
	if label == "" {
		return ""
	}
	return ":" + label
}

func nornicDBNodePattern(alias string, label string, param string) string {
	return nornicDBNodePatternWithProperty(alias, label, "uid", param)
}

// nornicDBNodePatternWithProperty keeps NornicDB entity-id lookups anchored in
// the node pattern. Live dogfood showed MATCH-plus-WHERE id/uid predicates can
// scan or hang, while relationship-pattern MATCH keeps type(rel) populated.
func nornicDBNodePatternWithProperty(alias string, label string, property string, param string) string {
	property = strings.TrimSpace(property)
	if property == "" {
		property = "uid"
	}
	return "(" + alias + nornicDBLabelPattern(label) + " {" + property + ": " + param + "})"
}

func nornicDBRelationshipPattern(relationshipType string) string {
	switch strings.ToUpper(strings.TrimSpace(relationshipType)) {
	case "CALLS", "REFERENCES", "IMPORTS", "INHERITS", "OVERRIDES", "USES_METACLASS":
		return ":" + strings.ToUpper(strings.TrimSpace(relationshipType))
	default:
		return ""
	}
}

func normalizeNornicDBRelationshipRows(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	normalized := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := cloneQueryAnyMap(row)
		removeNornicDBRelationshipPlaceholderProperties(item)
		normalized = append(normalized, item)
	}
	return normalized
}

func removeNornicDBRelationshipPlaceholderProperties(row map[string]any) {
	for _, key := range []string{
		"call_kind",
		"confidence",
		"reason",
		"resolution_method",
		"source_id",
		"source_name",
		"source_repo_id",
		"source_repo_name",
		"source_file_path",
		"source_language",
		"source_type",
		"source_start_line",
		"source_end_line",
		"target_id",
		"target_name",
		"target_repo_id",
		"target_repo_name",
		"target_file_path",
		"target_language",
		"target_type",
		"target_start_line",
		"target_end_line",
	} {
		removeNornicDBPlaceholderProperty(row, key)
	}
	removeNornicDBPlaceholderValue(row, "source_repo_id", "sourceRepo.id")
	removeNornicDBPlaceholderValue(row, "source_repo_name", "sourceRepo.name")
	removeNornicDBPlaceholderValue(row, "source_file_path", "sourceFile.relative_path")
	removeNornicDBPlaceholderValue(row, "source_language", "source.language", "sourceFile.language")
	removeNornicDBPlaceholderValue(row, "source_type", "head(labels(source))")
	removeNornicDBPlaceholderValue(row, "source_start_line", "source.start_line")
	removeNornicDBPlaceholderValue(row, "source_end_line", "source.end_line")
	removeNornicDBPlaceholderValue(row, "target_repo_id", "targetRepo.id")
	removeNornicDBPlaceholderValue(row, "target_repo_name", "targetRepo.name")
	removeNornicDBPlaceholderValue(row, "target_file_path", "targetFile.relative_path")
	removeNornicDBPlaceholderValue(row, "target_language", "target.language", "targetFile.language")
	removeNornicDBPlaceholderValue(row, "target_type", "head(labels(target))")
	removeNornicDBPlaceholderValue(row, "target_start_line", "target.start_line")
	removeNornicDBPlaceholderValue(row, "target_end_line", "target.end_line")
}

func removeNornicDBPlaceholderValue(row map[string]any, key string, placeholders ...string) {
	value := strings.TrimSpace(StringVal(row, key))
	if value == "" {
		delete(row, key)
		return
	}
	for _, placeholder := range placeholders {
		if value == placeholder {
			delete(row, key)
			return
		}
	}
}

func removeNornicDBPlaceholderProperty(row map[string]any, key string) {
	value := strings.TrimSpace(StringVal(row, key))
	if value == "" {
		delete(row, key)
		return
	}
	if value == key || strings.HasSuffix(value, "."+key) {
		delete(row, key)
	}
}

func (h *CodeHandler) nornicDBTransitiveRelationshipRows(
	ctx context.Context,
	entityID string,
	direction string,
	maxDepth int,
) ([]map[string]any, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" || maxDepth <= 0 {
		return []map[string]any{}, nil
	}

	frontier := []string{entityID}
	seen := map[string]struct{}{entityID: {}}
	rows := make([]map[string]any, 0)
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		next := make([]string, 0)
		for _, currentID := range frontier {
			hopRows, err := h.nornicDBTransitiveOneHopRows(ctx, currentID, direction)
			if err != nil {
				return nil, err
			}
			for _, hop := range hopRows {
				relationship := nornicDBTransitiveRelationshipRow(hop, direction, depth)
				nextID := nornicDBTransitiveNextID(relationship, direction)
				if nextID == "" {
					continue
				}
				if _, ok := seen[nextID]; ok {
					continue
				}
				seen[nextID] = struct{}{}
				next = append(next, nextID)
				rows = append(rows, relationship)
			}
		}
		frontier = next
	}
	return rows, nil
}

func (h *CodeHandler) nornicDBTransitiveOneHopRows(
	ctx context.Context,
	entityID string,
	direction string,
) ([]map[string]any, error) {
	params := map[string]any{"entity_id": entityID}
	if direction == "incoming" {
		return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+nornicDBEntityUIDPredicate("target", "$entity_id")+`
		RETURN coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
	`, params)
	}
	return h.Neo4j.Run(ctx, `
		MATCH (source)-[:CALLS]->(target)
		WHERE `+nornicDBEntityUIDPredicate("source", "$entity_id")+`
		RETURN coalesce(source.id, source.uid) as source_id,
		       source.name as source_name,
		       coalesce(target.id, target.uid) as target_id,
		       target.name as target_name
	`, params)
}

func nornicDBEntityUIDPredicate(alias string, param string) string {
	return alias + ".uid = " + param
}

func nornicDBTransitiveRelationshipRow(row map[string]any, direction string, depth int) map[string]any {
	out := cloneQueryAnyMap(row)
	out["depth"] = depth
	if direction == "incoming" {
		out["target_id"] = ""
		out["target_name"] = ""
	} else {
		out["source_id"] = ""
		out["source_name"] = ""
	}
	return out
}

func nornicDBTransitiveNextID(row map[string]any, direction string) string {
	if direction == "incoming" {
		return StringVal(row, "source_id")
	}
	return StringVal(row, "target_id")
}
