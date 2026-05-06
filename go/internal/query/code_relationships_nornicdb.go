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
	if direction != "incoming" {
		var err error
		// NornicDB currently needs metadata and each requested direction as
		// separate row queries; this avoids Neo4j-style map collection shapes
		// that are not dialect-safe while preserving direct relationship truth.
		outgoing, err = h.nornicDBOneHopRelationships(ctx, rowEntityID, "outgoing", relationshipType, entityLabel)
		if err != nil {
			return nil, err
		}
	}
	incoming := []map[string]any{}
	if direction != "outgoing" {
		var err error
		incoming, err = h.nornicDBOneHopRelationships(ctx, rowEntityID, "incoming", relationshipType, entityLabel)
		if err != nil {
			return nil, err
		}
	}
	response := cloneQueryAnyMap(row)
	response["outgoing"] = outgoing
	response["incoming"] = incoming
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

	entityLabel := h.nornicDBRelationshipEntityLabel(ctx, entityID)
	predicate, params := nornicDBRelationshipMetadataPredicate(name, repoID)
	entityID = strings.TrimSpace(entityID)
	if predicate == "" && entityID == "" {
		return nil, nil
	}
	if entityID != "" {
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

func (h *CodeHandler) nornicDBRelationshipEntityLabel(ctx context.Context, entityID string) string {
	if h == nil || h.Content == nil || strings.TrimSpace(entityID) == "" {
		return ""
	}
	entity, err := h.Content.GetEntityContent(ctx, strings.TrimSpace(entityID))
	if err != nil || entity == nil {
		return ""
	}
	return nornicDBGraphLabelForContentEntityType(entity.EntityType)
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

func (h *CodeHandler) nornicDBOneHopRelationships(
	ctx context.Context,
	entityID string,
	direction string,
	relationshipType string,
	entityLabel string,
) ([]map[string]any, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return []map[string]any{}, nil
	}
	for _, property := range []string{"uid", "id"} {
		cypher, params := nornicDBOneHopRelationshipsCypher(entityID, direction, relationshipType, entityLabel, property)
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

func nornicDBOneHopRelationshipsCypher(entityID string, direction string, relationshipType string, entityLabel string, entityIDProperty string) (string, map[string]any) {
	params := map[string]any{"entity_id": entityID}
	relPattern := nornicDBRelationshipPattern(relationshipType)
	entityPattern := nornicDBNodePatternWithProperty("e", entityLabel, entityIDProperty, "$entity_id")
	if direction == "incoming" {
		return `
		MATCH (source)-[rel` + relPattern + `]->` + entityPattern + `
		RETURN 'incoming' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       source.name as source_name,
		       coalesce(source.id, source.uid) as source_id
	`, params
	}
	return `
		MATCH ` + entityPattern + `-[rel` + relPattern + `]->(target)
		RETURN 'outgoing' as direction,
		       type(rel) as type,
		       rel.call_kind as call_kind,
		       rel.reason as reason,
		       target.name as target_name,
		       coalesce(target.id, target.uid) as target_id
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
		removeNornicDBPlaceholderProperty(item, "call_kind")
		removeNornicDBPlaceholderProperty(item, "reason")
		normalized = append(normalized, item)
	}
	return normalized
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
