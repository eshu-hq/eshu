package query

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func (h *ImpactHandler) entityMapNeighborhoodRows(
	ctx context.Context,
	req entityMapRequest,
	selected entityMapCandidate,
) ([]map[string]any, bool, error) {
	if selected.AnchorLabel == "" || selected.AnchorProperty == "" || selected.AnchorValue == "" {
		return nil, false, fmt.Errorf("resolved entity is missing a typed traversal anchor")
	}
	rows := make([]map[string]any, 0, req.Limit)
	truncated := false
	for _, direction := range []string{"outgoing", "incoming"} {
		cypher := entityMapTraversalCypher(selected, direction, req.Relationship, req.Depth)
		nextRows, err := h.Neo4j.Run(ctx, cypher, map[string]any{
			"from_id":     selected.AnchorValue,
			"environment": req.Environment,
			"repo_id":     req.RepoID,
			"limit":       req.Limit + 1,
		})
		if err != nil {
			return nil, false, fmt.Errorf("load %s entity map neighborhood: %w", direction, err)
		}
		if len(nextRows) > req.Limit {
			truncated = true
			nextRows = nextRows[:req.Limit]
		}
		rows = append(rows, nextRows...)
	}
	sortEntityMapRows(rows)
	if len(rows) > req.Limit {
		truncated = true
		rows = rows[:req.Limit]
	}
	return entityMapRelationshipMaps(rows), truncated, nil
}

func entityMapTraversalCypher(selected entityMapCandidate, direction string, relationship string, depth int) string {
	relationshipPattern := "rels"
	if relationship != "" {
		relationshipPattern = "rels:" + relationship
	}
	edge := fmt.Sprintf("(start)-[%s*1..%d]->(entity)", relationshipPattern, depth)
	if direction == "incoming" {
		edge = fmt.Sprintf("(start)<-[%s*1..%d]-(entity)", relationshipPattern, depth)
	}
	return fmt.Sprintf(`MATCH (start:%s {%s: $from_id})
MATCH path = %s
WHERE ($environment = '' OR coalesce(entity.environment, start.environment, '') = '' OR entity.environment = $environment)
  AND ($repo_id = '' OR coalesce(entity.repo_id, entity.id, '') = $repo_id OR coalesce(start.repo_id, start.id, '') = $repo_id)
RETURN DISTINCT coalesce(entity.id, entity.uid, entity.resource_id, entity.path, entity.name) AS entity_id,
       coalesce(entity.name, entity.address, entity.qualified_name, entity.path, entity.id, entity.uid) AS entity_name,
       labels(entity) AS entity_labels,
       %q AS direction,
       length(path) AS depth,
       [rel IN relationships(path) | type(rel)] AS relationship_types,
       coalesce(entity.repo_id, entity.id, '') AS repo_id,
       coalesce(entity.environment, '') AS environment
ORDER BY depth, entity.name, entity_id
LIMIT $limit`, selected.AnchorLabel, selected.AnchorProperty, edge, direction)
}

func entityMapRelationshipMaps(rows []map[string]any) []map[string]any {
	relationships := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		types := StringSliceVal(row, "relationship_types")
		relationshipType := ""
		if len(types) > 0 {
			relationshipType = types[len(types)-1]
		}
		relationship := compactStringMap(map[string]any{
			"entity_id":           StringVal(row, "entity_id"),
			"entity_name":         StringVal(row, "entity_name"),
			"direction":           StringVal(row, "direction"),
			"relationship_type":   relationshipType,
			"repo_id":             StringVal(row, "repo_id"),
			"environment":         StringVal(row, "environment"),
			"evidence_label":      entityMapEvidenceLabel(row),
			"relationship_source": "graph",
		})
		relationship["entity_labels"] = StringSliceVal(row, "entity_labels")
		relationship["relationship_types"] = types
		relationship["depth"] = IntVal(row, "depth")
		relationships = append(relationships, relationship)
	}
	return relationships
}

func sortEntityMapRows(rows []map[string]any) {
	slices.SortFunc(rows, func(a, b map[string]any) int {
		for _, compare := range []int{
			strings.Compare(StringVal(a, "direction"), StringVal(b, "direction")),
			IntVal(a, "depth") - IntVal(b, "depth"),
			strings.Compare(StringVal(a, "entity_name"), StringVal(b, "entity_name")),
			strings.Compare(StringVal(a, "entity_id"), StringVal(b, "entity_id")),
		} {
			if compare < 0 {
				return -1
			}
			if compare > 0 {
				return 1
			}
		}
		return 0
	})
}

func entityMapEvidenceLabel(row map[string]any) string {
	labels := StringSliceVal(row, "entity_labels")
	if hasEntityMapLabel(labels, "CloudResource") {
		return "cloud_or_runtime_graph"
	}
	if hasEntityMapLabel(labels, "TerraformResource") || hasEntityMapLabel(labels, "TerraformDataSource") {
		return "iac_graph"
	}
	if hasEntityMapLabel(labels, "K8sResource") {
		return "kubernetes_graph"
	}
	return "graph_relationship"
}
