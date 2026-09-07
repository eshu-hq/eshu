// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// normalizeEntityMapRows converts graph traversal rows into the canonical
// relationship row shape consumed by response building. Traversal Cypher keeps
// projections to plain node properties so backend-specific expression quirks do
// not decide the API shape.
func NormalizeEntityMapRows(
	rawRows []map[string]any,
	spec EntityMapTraversalSpec,
	selected EntityMapCandidate,
) []map[string]any {
	rows := make([]map[string]any, 0, len(rawRows))
	for _, raw := range rawRows {
		entityID := entityMapResolveEntityID(raw, spec, selected)
		row := map[string]any{
			"entity_id":           entityID,
			"entity_name":         entityMapResolveEntityName(raw),
			"entity_labels":       querycontract.StringSliceVal(raw, "entity_labels"),
			"direction":           spec.Direction,
			"depth":               EntityMapResolveDepth(raw, spec),
			"relationship_type":   querycontract.StringVal(raw, "relationship_type"),
			"relationship_types":  querycontract.StringSliceVal(raw, "relationship_types"),
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
	spec EntityMapTraversalSpec,
	selected EntityMapCandidate,
) string {
	for _, key := range []string{"entity_id", "id", "uid", "resource_id", "path", "name"} {
		if value := querycontract.StringVal(raw, key); value != "" {
			return value
		}
	}
	if spec.Direction == "incoming" &&
		hasEntityMapLabel(querycontract.StringSliceVal(raw, "entity_labels"), "Repository") &&
		selected.RepoID != "" {
		return selected.RepoID
	}
	return ""
}

func entityMapResolveEntityName(raw map[string]any) string {
	for _, key := range []string{"entity_name", "name", "address", "qualified_name", "path", "id", "uid"} {
		if value := querycontract.StringVal(raw, key); value != "" {
			return value
		}
	}
	return ""
}

func entityMapResolveRepoID(raw map[string]any, entityID string) string {
	if repoID := querycontract.StringVal(raw, "repo_id"); repoID != "" {
		return repoID
	}
	return entityID
}

func entityMapResolveEnvironment(raw map[string]any) string {
	return querycontract.StringVal(raw, "environment")
}

func EntityMapResolveDepth(raw map[string]any, spec EntityMapTraversalSpec) int {
	if depth := querycontract.IntVal(raw, "depth"); depth >= 1 {
		return depth
	}
	if spec.MaxHops <= 1 {
		return 1
	}
	if depth := querycontract.IntVal(raw, "path_length"); depth >= spec.MinHops {
		return depth
	}
	if spec.MinHops >= 1 {
		return spec.MinHops
	}
	return 1
}
