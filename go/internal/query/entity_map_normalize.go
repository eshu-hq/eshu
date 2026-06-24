// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// normalizeEntityMapRows converts graph traversal rows into the canonical
// relationship row shape consumed by response building. Traversal Cypher keeps
// projections to plain node properties so backend-specific expression quirks do
// not decide the API shape.
func normalizeEntityMapRows(
	rawRows []map[string]any,
	spec entityMapTraversalSpec,
	selected entityMapCandidate,
) []map[string]any {
	rows := make([]map[string]any, 0, len(rawRows))
	for _, raw := range rawRows {
		entityID := entityMapResolveEntityID(raw, spec, selected)
		row := map[string]any{
			"entity_id":           entityID,
			"entity_name":         entityMapResolveEntityName(raw),
			"entity_labels":       StringSliceVal(raw, "entity_labels"),
			"direction":           spec.direction,
			"depth":               entityMapResolveDepth(raw, spec),
			"relationship_type":   StringVal(raw, "relationship_type"),
			"relationship_types":  StringSliceVal(raw, "relationship_types"),
			"repo_id":             entityMapResolveRepoID(raw, entityID),
			"environment":         entityMapResolveEnvironment(raw),
			"relationship_source": "graph",
		}
		rows = append(rows, row)
	}
	return rows
}

func entityMapResolveEntityID(
	raw map[string]any,
	spec entityMapTraversalSpec,
	selected entityMapCandidate,
) string {
	for _, key := range []string{"entity_id", "id", "uid", "resource_id", "path", "name"} {
		if value := StringVal(raw, key); value != "" {
			return value
		}
	}
	if spec.direction == "incoming" &&
		hasEntityMapLabel(StringSliceVal(raw, "entity_labels"), "Repository") &&
		selected.RepoID != "" {
		return selected.RepoID
	}
	return ""
}

func entityMapResolveEntityName(raw map[string]any) string {
	for _, key := range []string{"entity_name", "name", "address", "qualified_name", "path", "id", "uid"} {
		if value := StringVal(raw, key); value != "" {
			return value
		}
	}
	return ""
}

func entityMapResolveRepoID(raw map[string]any, entityID string) string {
	if repoID := StringVal(raw, "repo_id"); repoID != "" {
		return repoID
	}
	return entityID
}

func entityMapResolveEnvironment(raw map[string]any) string {
	return StringVal(raw, "environment")
}

func entityMapResolveDepth(raw map[string]any, spec entityMapTraversalSpec) int {
	if depth := IntVal(raw, "depth"); depth >= 1 {
		return depth
	}
	if spec.maxHops <= 1 {
		return 1
	}
	if depth := IntVal(raw, "path_length"); depth >= spec.minHops {
		return depth
	}
	if spec.minHops >= 1 {
		return spec.minHops
	}
	return 1
}
