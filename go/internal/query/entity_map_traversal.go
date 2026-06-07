package query

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

var entityMapDefaultOutgoingRelationships = []string{
	"DEPENDS_ON",
	"USES",
	"USES_MODULE",
	"PROVISIONS_DEPENDENCY_FOR",
	"READS_CONFIG_FROM",
	"CALLS",
	"IMPORTS",
	"RUNS_ON",
}

var entityMapRepositoryOutgoingRelationships []string

var entityMapDefaultIncomingRelationships = []string{
	"DEFINES",
	"CONTAINS",
	"REPO_CONTAINS",
	"DEPLOYS_FROM",
	"HAS_DEPLOYMENT_EVIDENCE",
	"DEPENDS_ON",
	"USES",
	"USES_MODULE",
	"PROVISIONS_DEPENDENCY_FOR",
	"READS_CONFIG_FROM",
	"CALLS",
	"IMPORTS",
	"RUNS_ON",
}

var entityMapRepositoryIncomingRelationships = []string{
	"DEPLOYS_FROM",
	"HAS_DEPLOYMENT_EVIDENCE",
	"PROVISIONS_DEPENDENCY_FOR",
	"READS_CONFIG_FROM",
}

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
	for _, traversal := range entityMapTraversalSpecs(selected, req) {
		cypher := entityMapTraversalCypher(selected, traversal.direction, traversal.relationship, req.Depth)
		nextRows, err := h.Neo4j.Run(ctx, cypher, map[string]any{
			"from_id":     selected.AnchorValue,
			"environment": req.Environment,
			"repo_id":     req.RepoID,
			"limit":       req.Limit + 1,
		})
		if err != nil {
			return nil, false, fmt.Errorf("load %s entity map neighborhood: %w", traversal.direction, err)
		}
		if len(nextRows) > req.Limit {
			truncated = true
			nextRows = nextRows[:req.Limit]
		}
		rows = append(rows, nextRows...)
	}
	rows = dedupeEntityMapRows(rows)
	sortEntityMapRows(rows)
	if len(rows) > req.Limit {
		truncated = true
		rows = rows[:req.Limit]
	}
	return entityMapRelationshipMaps(rows, req.Relationship), truncated, nil
}

// dedupeEntityMapRows removes duplicate neighborhood rows produced across
// relationship-family traversals. The traversal Cypher no longer uses RETURN
// DISTINCT (it nulls the first coalesce column on NornicDB and can widen the
// match), so equivalent (direction, entity, relationship) rows are collapsed
// here. The first row for a key wins, preserving the per-spec ordering.
func dedupeEntityMapRows(rows []map[string]any) []map[string]any {
	if len(rows) <= 1 {
		return rows
	}
	seen := make(map[string]struct{}, len(rows))
	deduped := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		key := strings.Join([]string{
			StringVal(row, "direction"),
			StringVal(row, "entity_id"),
			StringVal(row, "entity_name"),
			StringVal(row, "relationship_type"),
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, row)
	}
	return deduped
}

type entityMapTraversalSpec struct {
	direction    string
	relationship string
}

func entityMapTraversalSpecs(selected entityMapCandidate, req entityMapRequest) []entityMapTraversalSpec {
	if req.Relationship != "" {
		return []entityMapTraversalSpec{
			{direction: "outgoing", relationship: req.Relationship},
			{direction: "incoming", relationship: req.Relationship},
		}
	}
	specs := make([]entityMapTraversalSpec, 0,
		len(entityMapDefaultOutgoingRelationshipTypes(selected))+len(entityMapDefaultIncomingRelationshipTypes(selected)))
	for _, relationship := range entityMapDefaultOutgoingRelationshipTypes(selected) {
		specs = append(specs, entityMapTraversalSpec{direction: "outgoing", relationship: relationship})
	}
	for _, relationship := range entityMapDefaultIncomingRelationshipTypes(selected) {
		specs = append(specs, entityMapTraversalSpec{direction: "incoming", relationship: relationship})
	}
	return specs
}

func entityMapDefaultOutgoingRelationshipTypes(selected entityMapCandidate) []string {
	if selected.AnchorLabel == "Repository" {
		return entityMapRepositoryOutgoingRelationships
	}
	return entityMapDefaultOutgoingRelationships
}

// entityMapTraversalCypher builds the neighborhood traversal for one
// direction/relationship-family spec. Each spec carries a single relationship
// family, so the relationship verb is emitted as a string literal rather than
// derived from relationships(path). This keeps the typed verb stable across
// graph backends: NornicDB does not populate relationships(path)/length(path)
// for variable-length patterns, so a path-derived type/depth would be
// null/zero there.
//
// The projection intentionally avoids RETURN DISTINCT. On NornicDB, DISTINCT
// over a coalesce()-projected entity binding nulls the first projected column
// (entity_id) and can drop the relationship-family constraint. Deduplication is
// performed in Go (see dedupeEntityMapRows).
func entityMapTraversalCypher(selected entityMapCandidate, direction string, relationship string, depth int) string {
	if depth <= 1 {
		return entityMapDirectTraversalCypher(selected, direction, relationship)
	}
	relationshipPattern := entityMapVariableRelationshipPattern(selected, direction, relationship)
	edge := fmt.Sprintf("(start)-[%s*1..%d]->(entity)", relationshipPattern, depth)
	if direction == "incoming" {
		edge = fmt.Sprintf("(start)<-[%s*1..%d]-(entity)", relationshipPattern, depth)
	}
	return fmt.Sprintf(`MATCH (start:%s {%s: $from_id})
MATCH path = %s
WHERE ($environment = '' OR coalesce(entity.environment, '') = '' OR entity.environment = $environment)
  AND ($repo_id = '' OR coalesce(entity.repo_id, '') = '' OR entity.repo_id = $repo_id OR (entity:Repository AND entity.id = $repo_id))
RETURN coalesce(entity.id, entity.uid, entity.resource_id, entity.path, entity.name) AS entity_id,
       coalesce(entity.name, entity.address, entity.qualified_name, entity.path, entity.id, entity.uid) AS entity_name,
       labels(entity) AS entity_labels,
       %q AS direction,
       length(path) AS depth,
       %q AS relationship_type,
       [rel IN relationships(path) | type(rel)] AS relationship_types,
       coalesce(entity.repo_id, entity.id, '') AS repo_id,
       coalesce(entity.environment, '') AS environment
ORDER BY depth, entity_name, entity_id
LIMIT $limit`, selected.AnchorLabel, selected.AnchorProperty, edge, direction, relationship)
}

func entityMapDirectTraversalCypher(selected entityMapCandidate, direction string, relationship string) string {
	edge := fmt.Sprintf("(start)-[%s]->(entity)", entityMapDirectRelationshipPattern(selected, direction, relationship))
	if direction == "incoming" {
		edge = fmt.Sprintf("(start)<-[%s]-(entity)", entityMapDirectRelationshipPattern(selected, direction, relationship))
	}
	return fmt.Sprintf(`MATCH (start:%s {%s: $from_id})
MATCH %s
WHERE ($environment = '' OR coalesce(entity.environment, '') = '' OR entity.environment = $environment)
  AND ($repo_id = '' OR coalesce(entity.repo_id, '') = '' OR entity.repo_id = $repo_id OR (entity:Repository AND entity.id = $repo_id))
RETURN coalesce(entity.id, entity.uid, entity.resource_id, entity.path, entity.name) AS entity_id,
       coalesce(entity.name, entity.address, entity.qualified_name, entity.path, entity.id, entity.uid) AS entity_name,
       labels(entity) AS entity_labels,
       %q AS direction,
       1 AS depth,
       %q AS relationship_type,
       coalesce(entity.repo_id, entity.id, '') AS repo_id,
       coalesce(entity.environment, '') AS environment
ORDER BY depth, entity_name, entity_id
LIMIT $limit`, selected.AnchorLabel, selected.AnchorProperty, edge, direction, relationship)
}

func entityMapDirectRelationshipPattern(selected entityMapCandidate, direction string, relationship string) string {
	return "rel:" + entityMapRelationshipTypes(selected, direction, relationship)
}

func entityMapVariableRelationshipPattern(selected entityMapCandidate, direction string, relationship string) string {
	return "rels:" + entityMapRelationshipTypes(selected, direction, relationship)
}

func entityMapRelationshipTypes(selected entityMapCandidate, direction string, relationship string) string {
	if relationship != "" {
		return relationship
	}
	if direction == "incoming" {
		return strings.Join(entityMapDefaultIncomingRelationshipTypes(selected), "|")
	}
	return strings.Join(entityMapDefaultOutgoingRelationshipTypes(selected), "|")
}

func entityMapDefaultIncomingRelationshipTypes(selected entityMapCandidate) []string {
	if selected.AnchorLabel == "Repository" {
		return entityMapRepositoryIncomingRelationships
	}
	return entityMapDefaultIncomingRelationships
}

func entityMapRelationshipMaps(rows []map[string]any, fallbackRelationship string) []map[string]any {
	relationships := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		types := StringSliceVal(row, "relationship_types")
		if len(types) == 0 {
			if relationshipType := StringVal(row, "relationship_type"); relationshipType != "" {
				types = []string{relationshipType}
			}
		}
		if len(types) == 0 && fallbackRelationship != "" {
			types = []string{fallbackRelationship}
		}
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
		relationship["depth"] = entityMapRowDepth(row)
		relationships = append(relationships, relationship)
	}
	return relationships
}

// entityMapRowDepth returns the traversal hop distance for a neighborhood row,
// clamped to a minimum of one hop. NornicDB returns length(path)=0 for
// variable-length patterns, but any returned graph edge is at least one hop
// from the anchor, so reporting depth 0 would mislabel the node as the anchor
// itself in the console Graph Explorer.
func entityMapRowDepth(row map[string]any) int {
	if depth := IntVal(row, "depth"); depth >= 1 {
		return depth
	}
	return 1
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
