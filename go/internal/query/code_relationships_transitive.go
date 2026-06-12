package query

import (
	"context"
	"fmt"
	"strings"
)

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
