// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	specs := entityMapTraversalSpecs(selected, req)
	rows := make([]map[string]any, 0, req.Limit)
	truncated := false
	for _, traversal := range specs {
		cypher := entityMapTraversalCypher(selected, traversal)
		nextRows, err := h.Neo4j.Run(ctx, cypher, map[string]any{
			"from_id":     selected.AnchorValue,
			"environment": req.Environment,
			"repo_id":     req.RepoID,
			"limit":       req.Limit + 1,
		})
		if err != nil {
			return nil, false, fmt.Errorf("load %s entity map neighborhood: %w", traversal.direction, err)
		}
		nextRows = normalizeEntityMapRows(nextRows, traversal, selected)
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
// bounded traversal specs. The traversal Cypher no longer uses RETURN
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
	direction     string
	relationships []string
	minHops       int
	maxHops       int
}

func entityMapTraversalSpecs(selected entityMapCandidate, req entityMapRequest) []entityMapTraversalSpec {
	directions := []struct {
		name          string
		relationships []string
	}{
		{name: "outgoing", relationships: entityMapTraversalRelationships(selected, req, "outgoing")},
		{name: "incoming", relationships: entityMapTraversalRelationships(selected, req, "incoming")},
	}
	specs := make([]entityMapTraversalSpec, 0, 2*len(directions))
	for _, direction := range directions {
		if len(direction.relationships) == 0 {
			continue
		}
		specs = append(specs, entityMapTraversalSpec{
			direction:     direction.name,
			relationships: direction.relationships,
			minHops:       1,
			maxHops:       1,
		})
		if req.Depth > 1 {
			specs = append(specs, entityMapTraversalSpec{
				direction:     direction.name,
				relationships: direction.relationships,
				minHops:       2,
				maxHops:       req.Depth,
			})
		}
	}
	return specs
}

func entityMapTraversalRelationships(selected entityMapCandidate, req entityMapRequest, direction string) []string {
	if req.Relationship != "" {
		return []string{req.Relationship}
	}
	if direction == "incoming" {
		return entityMapDefaultIncomingRelationshipTypes(selected)
	}
	return entityMapDefaultOutgoingRelationshipTypes(selected)
}

func entityMapDefaultOutgoingRelationshipTypes(selected entityMapCandidate) []string {
	if selected.AnchorLabel == "Repository" {
		return entityMapRepositoryOutgoingRelationships
	}
	return entityMapDefaultOutgoingRelationships
}

// entityMapTraversalCypher builds the neighborhood traversal for one bounded
// direction spec. Direct specs bind rel and report type(rel), preserving exact
// edge verbs for first-hop atlas edges. Deeper specs use one variable-length
// read per direction with the same relationship family set; backends that
// populate relationships(path) return exact hop verbs, while backends that omit
// path relationship metadata still return an honestly bounded neighbor row.
//
// The projection intentionally avoids RETURN DISTINCT. On NornicDB, DISTINCT
// over a coalesce()-projected entity binding nulls the first projected column
// (entity_id) and can drop the relationship-family constraint. Deduplication is
// performed in Go (see dedupeEntityMapRows).
//
// The anchor and the expansion live in one connected MATCH pattern
// (MATCH (start:Label {prop: $from_id})-[rel:...]->(entity)). Splitting them
// across two MATCH clauses (a bare MATCH (start:Label {...}) followed by a
// separate MATCH (start)-[rel]->(entity)) makes NornicDB re-plan the second
// clause independently of the resolved start node, scanning the relationship
// family population instead of expanding from the indexed anchor; that re-anchor
// fanout (issue #3549, same class as the issue #3172 double-MATCH cold plan)
// timed every service-node entity map out past the console's 15s budget.
func entityMapTraversalCypher(selected entityMapCandidate, spec entityMapTraversalSpec) string {
	if spec.maxHops <= 1 {
		return entityMapDirectTraversalCypher(selected, spec)
	}
	return entityMapVariableTraversalCypher(selected, spec)
}

const entityMapRawProjection = `entity.id AS id,
       entity.uid AS uid,
       entity.resource_id AS resource_id,
       entity.path AS path,
       entity.name AS name,
       entity.address AS address,
       entity.qualified_name AS qualified_name,
       entity.repo_id AS repo_id,
       entity.environment AS environment,
       labels(entity) AS entity_labels`

func entityMapDirectTraversalCypher(selected entityMapCandidate, spec entityMapTraversalSpec) string {
	edge := fmt.Sprintf("(start:%s {%s: $from_id})-[rel:%s]->(entity)", selected.AnchorLabel, selected.AnchorProperty, strings.Join(spec.relationships, "|"))
	if spec.direction == "incoming" {
		edge = fmt.Sprintf("(start:%s {%s: $from_id})<-[rel:%s]-(entity)", selected.AnchorLabel, selected.AnchorProperty, strings.Join(spec.relationships, "|"))
	}
	return fmt.Sprintf(
		`MATCH %s
WHERE ($environment = '' OR coalesce(entity.environment, '') = '' OR entity.environment = $environment)
  AND ($repo_id = '' OR coalesce(entity.repo_id, '') = '' OR coalesce(entity.repo_id, entity.id, '') = $repo_id)
RETURN %s,
       type(rel) AS relationship_type
ORDER BY name, id
LIMIT $limit`,
		edge,
		entityMapRawProjection,
	)
}

func entityMapVariableTraversalCypher(selected entityMapCandidate, spec entityMapTraversalSpec) string {
	relationshipPattern := fmt.Sprintf("rels:%s*%d..%d", strings.Join(spec.relationships, "|"), spec.minHops, spec.maxHops)
	edge := fmt.Sprintf("(start:%s {%s: $from_id})-[%s]->(entity)", selected.AnchorLabel, selected.AnchorProperty, relationshipPattern)
	if spec.direction == "incoming" {
		edge = fmt.Sprintf("(start:%s {%s: $from_id})<-[%s]-(entity)", selected.AnchorLabel, selected.AnchorProperty, relationshipPattern)
	}
	return fmt.Sprintf(
		`MATCH path = %s
WHERE ($environment = '' OR coalesce(entity.environment, '') = '' OR entity.environment = $environment)
  AND ($repo_id = '' OR coalesce(entity.repo_id, '') = '' OR coalesce(entity.repo_id, entity.id, '') = $repo_id)
RETURN %s,
       length(path) AS path_length,
       [rel IN relationships(path) | type(rel)] AS relationship_types
ORDER BY name, id
LIMIT $limit`,
		edge,
		entityMapRawProjection,
	)
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
	if hasEntityMapLabel(labels, "TerraformResource") ||
		hasEntityMapLabel(labels, "TerraformStateResource") ||
		hasEntityMapLabel(labels, "TerraformDataSource") {
		return "iac_graph"
	}
	if hasEntityMapLabel(labels, "K8sResource") {
		return "kubernetes_graph"
	}
	return "graph_relationship"
}
