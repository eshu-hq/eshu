package query

import (
	"context"
	"fmt"
	"strings"
)

func relationshipCapability(direction, relationshipType string) string {
	switch relationshipType {
	case "CALLS":
		if direction == "incoming" {
			return "call_graph.direct_callers"
		}
		return "call_graph.direct_callees"
	case "IMPORTS":
		return "symbol_graph.imports"
	case "INHERITS", "OVERRIDES", "IMPLEMENTS":
		return "symbol_graph.inheritance"
	case "INSTANTIATES":
		if direction == "incoming" {
			return "call_graph.direct_callers"
		}
		return "call_graph.direct_callees"
	default:
		return "call_graph.direct_callees"
	}
}

func transitiveRelationshipCapability(direction string) string {
	if direction == "incoming" {
		return "call_graph.transitive_callers"
	}
	return "call_graph.transitive_callees"
}

func transitiveRelationshipUnsupportedMessage(direction string) string {
	if direction == "incoming" {
		return "transitive callers require authoritative graph mode"
	}
	return "transitive callees require authoritative graph mode"
}

func (h *CodeHandler) relationshipsGraphRow(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
	direction string,
	relationshipType string,
) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipsGraphRow(ctx, entityID, name, repoID, direction, relationshipType)
	}

	if strings.TrimSpace(entityID) != "" {
		return h.Neo4j.RunSingle(ctx, relationshipGraphRowCypher(graphEntityIDPredicate("e", "$entity_id")), map[string]any{
			"entity_id": entityID,
		})
	}
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	if strings.TrimSpace(repoID) != "" {
		return h.Neo4j.RunSingle(ctx, relationshipGraphRowCypher(
			"e.name = $name AND EXISTS { MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository) WHERE repo.id = $repo_id }",
		), map[string]any{
			"name":    name,
			"repo_id": repoID,
		})
	}

	rows, err := h.Neo4j.Run(ctx, relationshipGraphRowCypher("e.name = $name"), map[string]any{
		"name": name,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, nil
	}
	return rows[0], nil
}

func (h *CodeHandler) transitiveRelationshipsGraphRow(
	ctx context.Context,
	req relationshipsRequest,
) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		metadataRow, err := h.nornicDBRelationshipMetadataRow(ctx, req.EntityID, req.Name, req.RepoID)
		if err != nil || metadataRow == nil {
			return metadataRow, err
		}
		rows, err := h.nornicDBTransitiveRelationshipRows(
			ctx,
			StringVal(metadataRow, "id"),
			req.Direction,
			req.MaxDepth,
		)
		if err != nil {
			return nil, err
		}
		return buildTransitiveRelationshipGraphResponse(metadataRow, rows, req.Direction), nil
	}

	metadataRow, err := h.relationshipsGraphRow(ctx, req.EntityID, req.Name, req.RepoID, "", "")
	if err != nil || metadataRow == nil {
		return metadataRow, err
	}

	cypher, params := buildTransitiveRelationshipRowsCypher(
		StringVal(metadataRow, "id"),
		req.Direction,
		req.MaxDepth,
		h.graphBackend(),
	)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return buildTransitiveRelationshipGraphResponse(metadataRow, rows, req.Direction), nil
}

func relationshipGraphRowCypher(predicate string) string {
	return `
		MATCH (e) WHERE ` + predicate + `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		OPTIONAL MATCH (e)-[outgoingRel]->(target)
		OPTIONAL MATCH (source)-[incomingRel]->(e)
		RETURN coalesce(e.id, e.uid) as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		       ,collect(DISTINCT {direction: 'outgoing', type: type(outgoingRel), call_kind: outgoingRel.call_kind, reason: outgoingRel.reason, target_name: target.name, target_id: coalesce(target.id, target.uid)}) as outgoing,
		       collect(DISTINCT {direction: 'incoming', type: type(incomingRel), call_kind: incomingRel.call_kind, reason: incomingRel.reason, source_name: source.name, source_id: coalesce(source.id, source.uid)}) as incoming
		LIMIT 2
	`
}

func buildTransitiveRelationshipRowsCypher(
	entityID string,
	direction string,
	maxDepth int,
	backend GraphBackend,
) (string, map[string]any) {
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
	}
	var cypher strings.Builder
	if backend == GraphBackendNornicDB {
		if direction == "incoming" {
			cypher.WriteString("\n\t\tMATCH (e)\n")
			cypher.WriteString("\t\tWHERE ")
			cypher.WriteString(graphEntityIDPredicate("e", "$entity_id"))
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
		cypher.WriteString(graphEntityIDPredicate("e", "$entity_id"))
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
	cypher.WriteString(graphEntityIDPredicate("e", "$entity_id"))
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

func graphEntityIDPredicate(alias string, param string) string {
	return fmt.Sprintf("(%s.id = %s OR %s.uid = %s)", alias, param, alias, param)
}

func buildTransitiveRelationshipGraphResponse(metadataRow map[string]any, rows []map[string]any, direction string) map[string]any {
	response := cloneQueryAnyMap(metadataRow)
	response["outgoing"] = []map[string]any{}
	response["incoming"] = []map[string]any{}

	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		depth := IntVal(row, "depth")
		if depth <= 0 {
			continue
		}
		if direction == "incoming" {
			sourceID := StringVal(row, "source_id")
			sourceName := StringVal(row, "source_name")
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
		targetID := StringVal(row, "target_id")
		targetName := StringVal(row, "target_name")
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

func normalizeGraphRelationships(response map[string]any) {
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
		if StringVal(item, "type") == "CALLS" && StringVal(item, "call_kind") == "jsx_component" {
			item["type"] = "REFERENCES"
			if StringVal(item, "reason") == "" {
				item["reason"] = "jsx_component_call_kind"
			}
		}
		normalized = append(normalized, item)
	}
	return normalized
}
