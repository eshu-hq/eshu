// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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
	selected EntityMapCandidate,
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
			return nil, false, fmt.Errorf("load %s entity map neighborhood: %w", traversal.Direction, err)
		}
		nextRows = NormalizeEntityMapRows(nextRows, traversal, selected)
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
			querycontract.StringVal(row, "direction"),
			querycontract.StringVal(row, "entity_id"),
			querycontract.StringVal(row, "entity_name"),
			querycontract.StringVal(row, "relationship_type"),
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, row)
	}
	return deduped
}

// EntityMapTraversalSpec bounds one entity-map traversal direction.
// Exported because staying query tests (queryplan binding) construct it to
// pin NormalizeEntityMapRows; the home stays this package. See #6060.
type EntityMapTraversalSpec struct {
	Direction     string
	Relationships []string
	MinHops       int
	MaxHops       int
}

func entityMapTraversalSpecs(selected EntityMapCandidate, req entityMapRequest) []EntityMapTraversalSpec {
	directions := []struct {
		name          string
		Relationships []string
	}{
		{name: "outgoing", Relationships: entityMapTraversalRelationships(selected, req, "outgoing")},
		{name: "incoming", Relationships: entityMapTraversalRelationships(selected, req, "incoming")},
	}
	specs := make([]EntityMapTraversalSpec, 0, 2*len(directions))
	for _, direction := range directions {
		if len(direction.Relationships) == 0 {
			continue
		}
		specs = append(specs, EntityMapTraversalSpec{
			Direction:     direction.name,
			Relationships: direction.Relationships,
			MinHops:       1,
			MaxHops:       1,
		})
		if req.Depth > 1 {
			specs = append(specs, EntityMapTraversalSpec{
				Direction:     direction.name,
				Relationships: direction.Relationships,
				MinHops:       2,
				MaxHops:       req.Depth,
			})
		}
	}
	return specs
}

func entityMapTraversalRelationships(selected EntityMapCandidate, req entityMapRequest, direction string) []string {
	if req.Relationship != "" {
		return []string{req.Relationship}
	}
	if direction == "incoming" {
		return entityMapDefaultIncomingRelationshipTypes(selected)
	}
	return entityMapDefaultOutgoingRelationshipTypes(selected)
}

func entityMapDefaultOutgoingRelationshipTypes(selected EntityMapCandidate) []string {
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
func entityMapTraversalCypher(selected EntityMapCandidate, spec EntityMapTraversalSpec) string {
	if spec.MaxHops <= 1 {
		return EntityMapDirectTraversalCypher(selected, spec)
	}
	return EntityMapVariableTraversalCypher(selected, spec)
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

func EntityMapDirectTraversalCypher(selected EntityMapCandidate, spec EntityMapTraversalSpec) string {
	edge := fmt.Sprintf("(start:%s {%s: $from_id})-[rel:%s]->(entity)", selected.AnchorLabel, selected.AnchorProperty, strings.Join(spec.Relationships, "|"))
	if spec.Direction == "incoming" {
		edge = fmt.Sprintf("(start:%s {%s: $from_id})<-[rel:%s]-(entity)", selected.AnchorLabel, selected.AnchorProperty, strings.Join(spec.Relationships, "|"))
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

func EntityMapVariableTraversalCypher(selected EntityMapCandidate, spec EntityMapTraversalSpec) string {
	relationshipPattern := fmt.Sprintf("rels:%s*%d..%d", strings.Join(spec.Relationships, "|"), spec.MinHops, spec.MaxHops)
	edge := fmt.Sprintf("(start:%s {%s: $from_id})-[%s]->(entity)", selected.AnchorLabel, selected.AnchorProperty, relationshipPattern)
	if spec.Direction == "incoming" {
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

func entityMapDefaultIncomingRelationshipTypes(selected EntityMapCandidate) []string {
	if selected.AnchorLabel == "Repository" {
		return entityMapRepositoryIncomingRelationships
	}
	return entityMapDefaultIncomingRelationships
}

func entityMapRelationshipMaps(rows []map[string]any, fallbackRelationship string) []map[string]any {
	relationships := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		types := querycontract.StringSliceVal(row, "relationship_types")
		if len(types) == 0 {
			if relationshipType := querycontract.StringVal(row, "relationship_type"); relationshipType != "" {
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
		relationship := querycontract.CompactStringMap(map[string]any{
			"entity_id":           querycontract.StringVal(row, "entity_id"),
			"entity_name":         querycontract.StringVal(row, "entity_name"),
			"direction":           querycontract.StringVal(row, "direction"),
			"relationship_type":   relationshipType,
			"repo_id":             querycontract.StringVal(row, "repo_id"),
			"environment":         querycontract.StringVal(row, "environment"),
			"evidence_label":      entityMapEvidenceLabel(row),
			"relationship_source": "graph",
		})
		relationship["entity_labels"] = querycontract.StringSliceVal(row, "entity_labels")
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
	if depth := querycontract.IntVal(row, "depth"); depth >= 1 {
		return depth
	}
	return 1
}

func sortEntityMapRows(rows []map[string]any) {
	slices.SortFunc(rows, func(a, b map[string]any) int {
		for _, compare := range []int{
			strings.Compare(querycontract.StringVal(a, "direction"), querycontract.StringVal(b, "direction")),
			querycontract.IntVal(a, "depth") - querycontract.IntVal(b, "depth"),
			strings.Compare(querycontract.StringVal(a, "entity_name"), querycontract.StringVal(b, "entity_name")),
			strings.Compare(querycontract.StringVal(a, "entity_id"), querycontract.StringVal(b, "entity_id")),
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
	labels := querycontract.StringSliceVal(row, "entity_labels")
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
